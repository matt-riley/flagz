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
	"time"

	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/metrics"
	"github.com/matt-riley/flagz/internal/middleware"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/service"
)

const (
	defaultStreamPollInterval = time.Second
	maxJSONBodyBytes          = 1 << 20
)

var errJSONBodyTooLarge = errors.New("json request body too large")

// HTTPServer handles HTTP requests for the flagz API, including flag CRUD,
// evaluation, SSE streaming, health checks, and metrics.
type HTTPServer struct {
	service            Service
	metrics            *metrics.Metrics
	metricsHandler     http.Handler
	streamPollInterval time.Duration
	maxJSONBodyBytes   int64
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

// NewHTTPHandler returns an [http.Handler] wired with all flagz routes and a
// default stream poll interval of 1 second.
func NewHTTPHandler(svc Service) http.Handler {
	return NewHTTPHandlerWithOptions(svc, defaultStreamPollInterval, nil)
}

// HTTPOption configures optional HTTPServer parameters.
type HTTPOption func(*HTTPServer)

// WithMaxJSONBodySize sets the maximum allowed JSON request body size in bytes.
// Defaults to 1MB if not set or if size <= 0.
func WithMaxJSONBodySize(size int64) HTTPOption {
	return func(s *HTTPServer) {
		if size > 0 {
			s.maxJSONBodyBytes = size
		}
	}
}

// NewHTTPHandlerWithStreamPollInterval returns an [http.Handler] wired with all
// flagz routes using the specified stream poll interval for SSE.
//
// Note: This constructor creates a private [metrics.Metrics] instance. To share
// a single registry across HTTP and gRPC servers, use [NewHTTPHandlerWithOptions].
func NewHTTPHandlerWithStreamPollInterval(svc Service, streamPollInterval time.Duration, opts ...HTTPOption) http.Handler {
	return NewHTTPHandlerWithOptions(svc, streamPollInterval, nil, opts...)
}

// NewHTTPHandlerWithOptions returns an [http.Handler] wired with all flagz
// routes using the specified stream poll interval and metrics. If m is nil, a
// default [metrics.Metrics] instance is created.
func NewHTTPHandlerWithOptions(svc Service, streamPollInterval time.Duration, m *metrics.Metrics, opts ...HTTPOption) http.Handler {
	if svc == nil {
		panic("service is nil")
	}

	if streamPollInterval <= 0 {
		streamPollInterval = defaultStreamPollInterval
	}

	if m == nil {
		m = metrics.New()
	}

	server := &HTTPServer{
		service:            svc,
		metrics:            m,
		metricsHandler:     m.Handler(),
		streamPollInterval: streamPollInterval,
		maxJSONBodyBytes:   maxJSONBodyBytes,
	}

	for _, opt := range opts {
		opt(server)
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
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		route := routePattern(r)
		status := strconv.Itoa(rw.statusCode)
		s.metrics.HTTPRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		s.metrics.HTTPRequestDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
	})
}

// statusRecorder wraps [http.ResponseWriter] to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.statusCode = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter so http.ResponseController
// can detect real interface support (e.g. http.Flusher) instead of relying
// on the wrapper's own type assertions.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// routePattern returns the matched route pattern for metrics labels,
// falling back to "unknown" if no pattern is available.
func routePattern(r *http.Request) string {
	if pat := r.Pattern; pat != "" {
		return pat
	}
	return "unknown"
}

func (s *HTTPServer) handleCreateFlag(w http.ResponseWriter, r *http.Request) {
	projectID, ok := middleware.ProjectIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var flag repository.Flag
	if err := s.decodeJSONBody(w, r, &flag); err != nil {
		writeJSONDecodeError(w, err)
		return
	}

	if strings.TrimSpace(flag.Key) == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}
	
	// Force project ID from context
	flag.ProjectID = projectID

	created, err := s.service.CreateFlag(r.Context(), flag)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (s *HTTPServer) handleGetFlag(w http.ResponseWriter, r *http.Request) {
	projectID, ok := middleware.ProjectIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}

	flag, err := s.service.GetFlag(r.Context(), projectID, key)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, flag)
}

func (s *HTTPServer) handleListFlags(w http.ResponseWriter, r *http.Request) {
	projectID, ok := middleware.ProjectIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	flags, err := s.service.ListFlags(r.Context(), projectID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, flags)
}

func (s *HTTPServer) handleUpdateFlag(w http.ResponseWriter, r *http.Request) {
	projectID, ok := middleware.ProjectIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}

	var flag repository.Flag
	if err := s.decodeJSONBody(w, r, &flag); err != nil {
		writeJSONDecodeError(w, err)
		return
	}

	if strings.TrimSpace(flag.Key) != "" && flag.Key != key {
		writeJSONError(w, http.StatusBadRequest, "path key and body key must match")
		return
	}
	flag.Key = key
	flag.ProjectID = projectID

	updated, err := s.service.UpdateFlag(r.Context(), flag)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

func (s *HTTPServer) handleDeleteFlag(w http.ResponseWriter, r *http.Request) {
	projectID, ok := middleware.ProjectIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeJSONError(w, http.StatusBadRequest, "key is required")
		return
	}

	if err := s.service.DeleteFlag(r.Context(), projectID, key); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	projectID, ok := middleware.ProjectIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var request evaluateJSONRequest
	if err := s.decodeJSONBody(w, r, &request); err != nil {
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
				ProjectID:    projectID,
				Key:          item.Key,
				Context:      item.Context,
				DefaultValue: item.DefaultValue,
			})
		}
	case strings.TrimSpace(request.Key) != "":
		requests = append(requests, service.ResolveRequest{
			ProjectID:    projectID,
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

	for _, result := range results {
		s.metrics.RecordEvaluation(result.Value)
	}

	writeJSON(w, http.StatusOK, evaluateJSONResponse{Results: results})
}

func (s *HTTPServer) handleStream(w http.ResponseWriter, r *http.Request) {
	projectID, ok := middleware.ProjectIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	lastEventID, err := parseLastEventID(r.Header.Get("Last-Event-ID"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid Last-Event-ID")
		return
	}

	rc := http.NewResponseController(w)

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
			_ = rc.Flush()
		}

		return nil
	}

	initialEvents, err := s.service.ListEventsSince(r.Context(), projectID, currentEventID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	headers := w.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	s.metrics.ActiveStreams.WithLabelValues("sse").Inc()
	defer s.metrics.ActiveStreams.WithLabelValues("sse").Dec()

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
			events, err := s.service.ListEventsSince(r.Context(), projectID, currentEventID)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				writeSSEError(w, rc, serviceErrorMessage(err))
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

func (s *HTTPServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.metricsHandler.ServeHTTP(w, r)
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
	case errors.Is(err, service.ErrFlagKeyRequired), errors.Is(err, service.ErrProjectIDRequired):
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
	case errors.Is(err, service.ErrFlagKeyRequired):
		return "flag key is required"
	case errors.Is(err, service.ErrProjectIDRequired):
		return "project ID is required"
	case errors.Is(err, service.ErrFlagNotFound):
		return "flag not found"
	case errors.Is(err, context.Canceled):
		return "request canceled"
	default:
		return "internal server error"
	}
}

func writeSSEError(w http.ResponseWriter, rc *http.ResponseController, message string) {
	payload, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		payload = []byte(`{"error":"internal server error"}`)
	}
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
	_ = rc.Flush()
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

func (s *HTTPServer) decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return io.EOF
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.maxJSONBodyBytes))
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
