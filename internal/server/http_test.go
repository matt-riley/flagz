package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/middleware"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/service"
)

func reqWithProject(req *http.Request) *http.Request {
	ctx := middleware.NewContextWithProjectID(req.Context(), "default")
	return req.WithContext(ctx)
}

func TestHTTPHandlerGetFlag(t *testing.T) {
	svc := &fakeService{
		getFlagFunc: func(_ context.Context, _, key string) (repository.Flag, error) {
			if key != "new-ui" {
				t.Fatalf("GetFlag key = %q, want %q", key, "new-ui")
			}
			return repository.Flag{
				Key:         "new-ui",
				Description: "new UI rollout",
				Enabled:     true,
				Variants:    json.RawMessage(`{}`),
				Rules:       json.RawMessage(`[]`),
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags/new-ui", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var got repository.Flag
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Key != "new-ui" {
		t.Fatalf("response key = %q, want %q", got.Key, "new-ui")
	}
}

func TestHTTPHandlerListFlags(t *testing.T) {
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return []repository.Flag{
				{
					Key:         "new-ui",
					Description: "new UI rollout",
					Enabled:     true,
				},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got []repository.Flag
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got) != 1 || got[0].Key != "new-ui" {
		t.Fatalf("response = %#v, want single new-ui flag", got)
	}
}

func TestHTTPHandlerCreateFlagOversizedBody(t *testing.T) {
	svc := &fakeService{
		createFlagFunc: func(_ context.Context, _ repository.Flag) (repository.Flag, error) {
			t.Fatal("CreateFlag should not be called for oversized request bodies")
			return repository.Flag{}, nil
		},
	}

	oversizedDescription := strings.Repeat("a", int(maxJSONBodyBytes)+1)
	body := `{"key":"new-ui","description":"` + oversizedDescription + `"}`

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodPost, "/v1/flags", strings.NewReader(body)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if !strings.Contains(rec.Body.String(), `"error":"request body too large"`) {
		t.Fatalf("body = %q, want request body too large error", rec.Body.String())
	}
}

func TestHTTPHandlerCreateFlagInvalidRulesReturnsBadRequest(t *testing.T) {
	svc := &fakeService{
		createFlagFunc: func(_ context.Context, _ repository.Flag) (repository.Flag, error) {
			return repository.Flag{}, service.ErrInvalidRules
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodPost, "/v1/flags", strings.NewReader(`{"key":"new-ui","rules":"invalid"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), `"error":"invalid rules"`) {
		t.Fatalf("body = %q, want invalid rules error", rec.Body.String())
	}
}

func TestHTTPHandlerCreateFlagInvalidVariantsReturnsBadRequest(t *testing.T) {
	svc := &fakeService{
		createFlagFunc: func(_ context.Context, _ repository.Flag) (repository.Flag, error) {
			return repository.Flag{}, service.ErrInvalidVariants
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodPost, "/v1/flags", strings.NewReader(`{"key":"new-ui"}`)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), `"error":"invalid variants"`) {
		t.Fatalf("body = %q, want invalid variants error", rec.Body.String())
	}
}

func TestHTTPHandlerStreamReplaysFromLastEventID(t *testing.T) {
	sinceCalls := make([]int64, 0)
	svc := &fakeService{
		listEventsSinceFunc: func(_ context.Context, _ string, since int64) ([]repository.FlagEvent, error) {
			sinceCalls = append(sinceCalls, since)
			if since != 1 {
				return nil, nil
			}
			return []repository.FlagEvent{
				{
					EventID:   2,
					FlagKey:   "new-ui",
					EventType: service.EventTypeUpdated,
					Payload:   json.RawMessage(`{"key":"new-ui","enabled":true}`),
				},
				{
					EventID:   3,
					FlagKey:   "old-ui",
					EventType: service.EventTypeDeleted,
					Payload:   json.RawMessage(`{"key":"old-ui"}`),
				},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/stream", nil).WithContext(ctx))
	req.Header.Set("Last-Event-ID", "1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if len(sinceCalls) == 0 || sinceCalls[0] != 1 {
		t.Fatalf("first ListEventsSince call = %#v, want first value %d", sinceCalls, 1)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "id: 2") || !strings.Contains(body, "event: update") {
		t.Fatalf("stream body missing update event: %q", body)
	}
	if !strings.Contains(body, "id: 3") || !strings.Contains(body, "event: delete") {
		t.Fatalf("stream body missing delete event: %q", body)
	}
}

func TestHTTPHandlerStreamCompactsPayloadToSingleDataLine(t *testing.T) {
	svc := &fakeService{
		listEventsSinceFunc: func(_ context.Context, _ string, since int64) ([]repository.FlagEvent, error) {
			if since != 0 {
				return nil, nil
			}

			return []repository.FlagEvent{
				{
					EventID:   1,
					FlagKey:   "new-ui",
					EventType: service.EventTypeUpdated,
					Payload:   json.RawMessage("{\n  \"key\": \"new-ui\",\n  \"enabled\": true\n}"),
				},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/stream", nil).WithContext(ctx))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `data: {"key":"new-ui","enabled":true}`) {
		t.Fatalf("stream body missing compact payload: %q", body)
	}
	if strings.Contains(body, "data: {\n") {
		t.Fatalf("stream body should not contain multiline data payload: %q", body)
	}
}

func TestHTTPHandlerStreamInitialFetchErrorReturnsHTTPError(t *testing.T) {
	svc := &fakeService{
		listEventsSinceFunc: func(_ context.Context, _ string, _ int64) ([]repository.FlagEvent, error) {
			return nil, errors.New("backend failure")
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/stream", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if !strings.Contains(rec.Body.String(), `"error":"internal server error"`) {
		t.Fatalf("body = %q, want internal server error json", rec.Body.String())
	}
}

func TestHTTPHandlerStreamFlushesHeadersWithoutInitialEvents(t *testing.T) {
	svc := &fakeService{
		listEventsSinceFunc: func(_ context.Context, _ string, _ int64) ([]repository.FlagEvent, error) {
			return nil, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/stream", nil).WithContext(ctx))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want %q", got, "text/event-stream")
	}
	if !rec.Flushed {
		t.Fatal("stream should flush headers even without initial events")
	}
}

func TestHTTPHandlerStreamSendsSSEErrorAfterStartOnBackendFailure(t *testing.T) {
	callCount := 0
	svc := &fakeService{
		listEventsSinceFunc: func(_ context.Context, _ string, _ int64) ([]repository.FlagEvent, error) {
			callCount++
			switch callCount {
			case 1:
				return []repository.FlagEvent{
					{
						EventID:   1,
						FlagKey:   "new-ui",
						EventType: service.EventTypeUpdated,
						Payload:   json.RawMessage(`{"key":"new-ui","enabled":true}`),
					},
				}, nil
			case 2:
				return nil, errors.New("backend failure")
			default:
				return nil, nil
			}
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/stream", nil).WithContext(ctx))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: update") {
		t.Fatalf("stream body missing update event: %q", body)
	}
	if !strings.Contains(body, "event: error") {
		t.Fatalf("stream body missing error event: %q", body)
	}
	if !strings.Contains(body, `data: {"error":"internal server error"}`) {
		t.Fatalf("stream body missing error payload: %q", body)
	}
}

func TestHTTPHandlerListAuditLog(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	svc := &fakeService{
		listAuditLogFunc: func(_ context.Context, projectID string, limit, offset int) ([]repository.AuditLogEntry, error) {
			if projectID != "default" {
				t.Fatalf("ListAuditLog projectID = %q, want %q", projectID, "default")
			}
			return []repository.AuditLogEntry{
				{ID: 1, ProjectID: "default", Action: "create", FlagKey: "my-flag", CreatedAt: now},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/audit-log", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got []repository.AuditLogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got) != 1 || got[0].FlagKey != "my-flag" {
		t.Fatalf("response = %#v, want single entry for my-flag", got)
	}
}

func TestHTTPHandlerListAuditLogUnauthorized(t *testing.T) {
	svc := &fakeService{}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/v1/audit-log", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

type fakeService struct {
	createFlagFunc            func(ctx context.Context, flag repository.Flag) (repository.Flag, error)
	updateFlagFunc            func(ctx context.Context, flag repository.Flag) (repository.Flag, error)
	getFlagFunc               func(ctx context.Context, projectID, key string) (repository.Flag, error)
	listFlagsFunc             func(ctx context.Context, projectID string) ([]repository.Flag, error)
	deleteFlagFunc            func(ctx context.Context, projectID, key string) error
	resolveBooleanFunc        func(ctx context.Context, projectID, key string, evalContext core.EvaluationContext, defaultValue bool) (bool, error)
	resolveBatchFunc          func(ctx context.Context, requests []service.ResolveRequest) ([]service.ResolveResult, error)
	listEventsSinceFunc       func(ctx context.Context, projectID string, eventID int64) ([]repository.FlagEvent, error)
	listEventsSinceForKeyFunc func(ctx context.Context, projectID string, eventID int64, key string) ([]repository.FlagEvent, error)
	listAuditLogFunc          func(ctx context.Context, projectID string, limit, offset int) ([]repository.AuditLogEntry, error)
}

func (f *fakeService) CreateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error) {
	if f.createFlagFunc != nil {
		return f.createFlagFunc(ctx, flag)
	}
	return repository.Flag{}, errors.New("CreateFlag not implemented")
}

func (f *fakeService) UpdateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error) {
	if f.updateFlagFunc != nil {
		return f.updateFlagFunc(ctx, flag)
	}
	return repository.Flag{}, errors.New("UpdateFlag not implemented")
}

func (f *fakeService) GetFlag(ctx context.Context, projectID, key string) (repository.Flag, error) {
	if f.getFlagFunc != nil {
		return f.getFlagFunc(ctx, projectID, key)
	}
	return repository.Flag{}, errors.New("GetFlag not implemented")
}

func (f *fakeService) ListFlags(ctx context.Context, projectID string) ([]repository.Flag, error) {
	if f.listFlagsFunc != nil {
		return f.listFlagsFunc(ctx, projectID)
	}
	return nil, errors.New("ListFlags not implemented")
}

func (f *fakeService) DeleteFlag(ctx context.Context, projectID, key string) error {
	if f.deleteFlagFunc != nil {
		return f.deleteFlagFunc(ctx, projectID, key)
	}
	return errors.New("DeleteFlag not implemented")
}

func (f *fakeService) ResolveBoolean(ctx context.Context, projectID, key string, evalContext core.EvaluationContext, defaultValue bool) (bool, error) {
	if f.resolveBooleanFunc != nil {
		return f.resolveBooleanFunc(ctx, projectID, key, evalContext, defaultValue)
	}
	return false, errors.New("ResolveBoolean not implemented")
}

func (f *fakeService) ResolveBatch(ctx context.Context, requests []service.ResolveRequest) ([]service.ResolveResult, error) {
	if f.resolveBatchFunc != nil {
		return f.resolveBatchFunc(ctx, requests)
	}
	return nil, errors.New("ResolveBatch not implemented")
}

func (f *fakeService) ListEventsSince(ctx context.Context, projectID string, eventID int64) ([]repository.FlagEvent, error) {
	if f.listEventsSinceFunc != nil {
		return f.listEventsSinceFunc(ctx, projectID, eventID)
	}
	return nil, errors.New("ListEventsSince not implemented")
}

func (f *fakeService) ListEventsSinceForKey(ctx context.Context, projectID string, eventID int64, key string) ([]repository.FlagEvent, error) {
	if f.listEventsSinceForKeyFunc != nil {
		return f.listEventsSinceForKeyFunc(ctx, projectID, eventID, key)
	}
	return nil, errors.New("ListEventsSinceForKey not implemented")
}

func (f *fakeService) ListAuditLog(ctx context.Context, projectID string, limit, offset int) ([]repository.AuditLogEntry, error) {
	if f.listAuditLogFunc != nil {
		return f.listAuditLogFunc(ctx, projectID, limit, offset)
	}
	return nil, errors.New("ListAuditLog not implemented")
}
