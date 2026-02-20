package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mattriley/flagz/internal/core"
	"github.com/mattriley/flagz/internal/repository"
	"github.com/mattriley/flagz/internal/service"
)

const (
	defaultStreamPollInterval = time.Second
	maxJSONBodyBytes          = 1 << 20
)

var errJSONBodyTooLarge = errors.New("json request body too large")

type HTTPServer struct {
	service            Service
	streamPollInterval time.Duration
	requestsTotal      atomic.Uint64
}

type evaluateJSONRequest struct {
	Key          string                  `json:"key,omitempty"`
	Context      core.EvaluationContext  `json:"context,omitempty"`
	DefaultValue bool                    `json:"default_value,omitempty"`
	Requests     []evaluateJSONBatchItem `json:"requests,omitempty"`
}

type evaluateJSONBatchItem struct {
	Key          string                 `json:"key"`
	Context      core.EvaluationContext `json:"context"`
	DefaultValue bool                   `json:"default_value"`
}

type evaluateJSONResponse struct {
	Results []service.ResolveResult `json:"results"`
}

func NewHTTPHandler(svc Service) http.Handler {
	return NewHTTPHandlerWithStreamPollInterval(svc, defaultStreamPollInterval)
}

func NewHTTPHandlerWithStreamPollInterval(svc Service, streamPollInterval time.Duration) http.Handler {
	if svc == nil {
		panic("service is nil")
	}

	if streamPollInterval <= 0 {
		streamPollInterval = defaultStreamPollInterval
	}

	server := &HTTPServer{
		service:            svc,
		streamPollInterval: streamPollInterval,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/flags", server.handleCreateFlag)
	mux.HandleFunc("GET /v1/flags", server.handleListFlags)
	mux.HandleFunc("GET /v1/flags/{key}", server.handleGetFlag)
	mux.HandleFunc("PUT /v1/flags/{key}", server.handleUpdateFlag)
	mux.HandleFunc("DELETE /v1/flags/{key}", server.handleDeleteFlag)
	mux.HandleFunc("POST /v1/evaluate", server.handleEvaluate)
	mux.HandleFunc("GET /v1/stream", server.handleStream)
	mux.HandleFunc("GET /healthz", server.handleHealthz)
	mux.HandleFunc("GET /metrics", server.handleMetrics)

	return server.withMetrics(mux)
}

func (s *HTTPServer) withMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requestsTotal.Add(1)
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) handleCreateFlag(w http.ResponseWriter, r *http.Request) {
	var flag repository.Flag
	if err := decodeJSONBody(w, r, &flag); err != nil {
		writeJSONDecodeError(w, err)
		return
	}

	if strings.TrimSpace(flag.Key) == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}

	created, err := s.service.CreateFlag(r.Context(), flag)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (s *HTTPServer) handleGetFlag(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}

	flag, err := s.service.GetFlag(r.Context(), key)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, flag)
}

func (s *HTTPServer) handleListFlags(w http.ResponseWriter, r *http.Request) {
	flags, err := s.service.ListFlags(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, flags)
}

func (s *HTTPServer) handleUpdateFlag(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}

	var flag repository.Flag
	if err := decodeJSONBody(w, r, &flag); err != nil {
		writeJSONDecodeError(w, err)
		return
	}

	if strings.TrimSpace(flag.Key) != "" && flag.Key != key {
		writeJSONError(w, http.StatusBadRequest, "path key and body key must match")
		return
	}
	flag.Key = key

	updated, err := s.service.UpdateFlag(r.Context(), flag)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (s *HTTPServer) handleDeleteFlag(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}

	if err := s.service.DeleteFlag(r.Context(), key); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	var request evaluateJSONRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeJSONDecodeError(w, err)
		return
	}

	requests := make([]service.ResolveRequest, 0)
	switch {
	case len(request.Requests) > 0 && strings.TrimSpace(request.Key) != "":
		writeJSONError(w, http.StatusBadRequest, "use either key or requests")
		return
	case len(request.Requests) > 0:
		requests = make([]service.ResolveRequest, 0, len(request.Requests))
		for idx, item := range request.Requests {
			if strings.TrimSpace(item.Key) == "" {
				writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("requests[%d].key is required", idx))
				return
			}
			requests = append(requests, service.ResolveRequest{
				Key:          item.Key,
				Context:      item.Context,
				DefaultValue: item.DefaultValue,
			})
		}
	case strings.TrimSpace(request.Key) != "":
		requests = append(requests, service.ResolveRequest{
			Key:          request.Key,
			Context:      request.Context,
			DefaultValue: request.DefaultValue,
		})
	default:
		writeJSONError(w, http.StatusBadRequest, "key or requests is required")
		return
	}

	results, err := s.service.ResolveBatch(r.Context(), requests)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, evaluateJSONResponse{Results: results})
}

func (s *HTTPServer) handleStream(w http.ResponseWriter, r *http.Request) {
	lastEventID, err := parseLastEventID(r.Header.Get("Last-Event-ID"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid Last-Event-ID")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	currentEventID := lastEventID
	writeEvents := func(events []repository.FlagEvent) error {
		for _, event := range events {
			currentEventID = event.EventID
			eventName := toSSEEventName(event.EventType)
			if eventName == "" {
				continue
			}

			payload := event.Payload
			if len(payload) == 0 {
				payload = []byte(`{}`)
			}

			if err := writeSSEEvent(w, event.EventID, eventName, payload); err != nil {
				return err
			}
			flusher.Flush()
		}

		return nil
	}

	initialEvents, err := s.service.ListEventsSince(r.Context(), currentEventID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	headers := w.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	if err := writeEvents(initialEvents); err != nil {
		return
	}

	ticker := time.NewTicker(s.streamPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			events, err := s.service.ListEventsSince(r.Context(), currentEventID)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				writeSSEError(w, flusher, serviceErrorMessage(err))
				return
			}
			if err := writeEvents(events); err != nil {
				return
			}
		}
	}
}

func (s *HTTPServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *HTTPServer) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(w, "# HELP flagz_http_requests_total Total number of HTTP requests.\n")
	_, _ = fmt.Fprintf(w, "# TYPE flagz_http_requests_total counter\n")
	_, _ = fmt.Fprintf(w, "flagz_http_requests_total %d\n", s.requestsTotal.Load())
}

func parseLastEventID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}

	eventID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || eventID < 0 {
		return 0, errors.New("invalid event id")
	}

	return eventID, nil
}

func toSSEEventName(eventType string) string {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "update", "updated":
		return "update"
	case "delete", "deleted":
		return "delete"
	default:
		return ""
	}
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidRules), errors.Is(err, service.ErrInvalidVariants):
		writeJSONError(w, http.StatusBadRequest, serviceErrorMessage(err))
	case errors.Is(err, service.ErrFlagNotFound):
		writeJSONError(w, http.StatusNotFound, serviceErrorMessage(err))
	case errors.Is(err, context.Canceled):
		writeJSONError(w, http.StatusRequestTimeout, serviceErrorMessage(err))
	default:
		writeJSONError(w, http.StatusInternalServerError, serviceErrorMessage(err))
	}
}

func serviceErrorMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrInvalidRules):
		return "invalid rules"
	case errors.Is(err, service.ErrInvalidVariants):
		return "invalid variants"
	case errors.Is(err, service.ErrFlagNotFound):
		return "flag not found"
	case errors.Is(err, context.Canceled):
		return "request canceled"
	default:
		return "internal server error"
	}
}

func writeSSEError(w http.ResponseWriter, flusher http.Flusher, message string) {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		payload = []byte(`{"error":"internal server error"}`)
	}
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
	flusher.Flush()
}

func writeSSEEvent(w io.Writer, eventID int64, eventName string, payload []byte) error {
	dataLines := compactSSEPayload(payload)
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\n", eventID, eventName); err != nil {
		return err
	}

	for _, line := range dataLines {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}

	_, err := fmt.Fprint(w, "\n")
	return err
}

func compactSSEPayload(payload []byte) []string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err == nil {
		return []string{compact.String()}
	}

	lines := strings.Split(string(payload), "\n")
	if len(lines) == 0 {
		return []string{""}
	}

	return lines
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSONDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errJSONBodyTooLarge) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return io.EOF
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return normalizeJSONDecodeError(err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain a single JSON object")
		}
		return normalizeJSONDecodeError(err)
	}

	return nil
}

func normalizeJSONDecodeError(err error) error {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return errJSONBodyTooLarge
	}
	return err
}
