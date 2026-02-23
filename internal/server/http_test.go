package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestHTTPHandlerListFlagsPaginationDefault(t *testing.T) {
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return []repository.Flag{
				{Key: "alpha"},
				{Key: "beta"},
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
	if len(got) != 2 {
		t.Fatalf("got %d flags, want 2", len(got))
	}
}

func TestHTTPHandlerListFlagsPaginationWithCursor(t *testing.T) {
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return []repository.Flag{
				{Key: "alpha"},
				{Key: "beta"},
				{Key: "gamma"},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?cursor=alpha", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got paginatedFlagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Flags) != 2 {
		t.Fatalf("got %d flags, want 2", len(got.Flags))
	}
	if got.Flags[0].Key != "beta" {
		t.Fatalf("first flag key = %q, want %q", got.Flags[0].Key, "beta")
	}
}

func TestHTTPHandlerListFlagsPaginationWithEmptyCursorParam(t *testing.T) {
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return []repository.Flag{
				{Key: "alpha"},
				{Key: "beta"},
				{Key: "gamma"},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?cursor=", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got paginatedFlagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Flags) != 3 {
		t.Fatalf("got %d flags, want 3", len(got.Flags))
	}
	if got.NextCursor != "" {
		t.Fatalf("next_cursor = %q, want empty", got.NextCursor)
	}
}

func TestHTTPHandlerListFlagsPaginationWithLimit(t *testing.T) {
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return []repository.Flag{
				{Key: "alpha"},
				{Key: "beta"},
				{Key: "gamma"},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?limit=2", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got paginatedFlagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Flags) != 2 {
		t.Fatalf("got %d flags, want 2", len(got.Flags))
	}
	if got.NextCursor != "beta" {
		t.Fatalf("next_cursor = %q, want %q", got.NextCursor, "beta")
	}
}

func TestHTTPHandlerListFlagsPaginationWithInvalidLimit(t *testing.T) {
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return []repository.Flag{
				{Key: "alpha"},
				{Key: "beta"},
				{Key: "gamma"},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	tests := []struct {
		name  string
		query string
	}{
		{name: "zero", query: "0"},
		{name: "negative", query: "-1"},
		{name: "non-integer", query: "notanint"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?limit="+tc.query, nil))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			var got struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal error response: %v", err)
			}
			if got.Error != "limit must be a positive integer" {
				t.Fatalf("error = %q, want %q", got.Error, "limit must be a positive integer")
			}
		})
	}
}

func TestHTTPHandlerListFlagsPaginationMaxLimitClamped(t *testing.T) {
	flags := make([]repository.Flag, 1002)
	for i := range flags {
		flags[i] = repository.Flag{Key: fmt.Sprintf("flag-%04d", i)}
	}
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return flags, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?limit=9999", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got paginatedFlagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Flags) != 1000 {
		t.Fatalf("got %d flags, want 1000 (clamped)", len(got.Flags))
	}
	if got.NextCursor == "" {
		t.Fatal("expected next_cursor to be set when more flags remain")
	}
}

func TestHTTPHandlerListFlagsPaginationCursorAndLimit(t *testing.T) {
	flags := make([]repository.Flag, 10)
	for i := range flags {
		flags[i] = repository.Flag{Key: fmt.Sprintf("flag-%04d", i)}
	}
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return flags, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)
	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?cursor=flag-0003&limit=2", nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got paginatedFlagsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.Flags) != 2 {
		t.Fatalf("got %d flags, want 2", len(got.Flags))
	}
	if got.Flags[0].Key != "flag-0004" || got.Flags[1].Key != "flag-0005" {
		t.Fatalf("got keys %q, %q; want flag-0004, flag-0005", got.Flags[0].Key, got.Flags[1].Key)
	}
	if got.NextCursor != "flag-0005" {
		t.Fatalf("next_cursor = %q, want %q", got.NextCursor, "flag-0005")
	}
}

func TestHTTPHandlerListFlagsPaginationProgression(t *testing.T) {
	flags := make([]repository.Flag, 5)
	for i := range flags {
		flags[i] = repository.Flag{Key: fmt.Sprintf("flag-%04d", i)}
	}
	svc := &fakeService{
		listFlagsFunc: func(_ context.Context, _ string) ([]repository.Flag, error) {
			return flags, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, 5*time.Millisecond)

	// Page 1: limit=2, no cursor → flag-0000, flag-0001
	req1 := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?limit=2", nil))
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	var page1 paginatedFlagsResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &page1); err != nil {
		t.Fatalf("page 1 unmarshal: %v", err)
	}
	if len(page1.Flags) != 2 || page1.Flags[0].Key != "flag-0000" || page1.Flags[1].Key != "flag-0001" {
		t.Fatalf("page 1 unexpected: %v", page1.Flags)
	}
	if page1.NextCursor == "" {
		t.Fatal("page 1 next_cursor should be set")
	}

	// Page 2: cursor from page 1
	req2 := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?cursor="+page1.NextCursor+"&limit=2", nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	var page2 paginatedFlagsResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("page 2 unmarshal: %v", err)
	}
	if len(page2.Flags) != 2 || page2.Flags[0].Key != "flag-0002" || page2.Flags[1].Key != "flag-0003" {
		t.Fatalf("page 2 unexpected: %v", page2.Flags)
	}
	if page2.NextCursor == "" {
		t.Fatal("page 2 next_cursor should be set")
	}

	// Page 3: final page, 1 remaining flag
	req3 := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/flags?cursor="+page2.NextCursor+"&limit=2", nil))
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	var page3 paginatedFlagsResponse
	if err := json.Unmarshal(rec3.Body.Bytes(), &page3); err != nil {
		t.Fatalf("page 3 unmarshal: %v", err)
	}
	if len(page3.Flags) != 1 || page3.Flags[0].Key != "flag-0004" {
		t.Fatalf("page 3 unexpected: %v", page3.Flags)
	}
	if page3.NextCursor != "" {
		t.Fatalf("page 3 next_cursor = %q, want empty", page3.NextCursor)
	}
}

func TestHTTPHandlerStreamWithKeyFilter(t *testing.T) {
	var calledKey string
	svc := &fakeService{
		listEventsSinceForKeyFunc: func(_ context.Context, _ string, _ int64, key string) ([]repository.FlagEvent, error) {
			calledKey = key
			return []repository.FlagEvent{
				{
					EventID:   1,
					FlagKey:   "myFlag",
					EventType: service.EventTypeUpdated,
					Payload:   json.RawMessage(`{"key":"myFlag","enabled":true}`),
				},
			}, nil
		},
	}

	handler := NewHTTPHandlerWithStreamPollInterval(svc, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	req := reqWithProject(httptest.NewRequest(http.MethodGet, "/v1/stream?key=myFlag", nil).WithContext(ctx))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if calledKey != "myFlag" {
		t.Fatalf("ListEventsSinceForKey key = %q, want %q", calledKey, "myFlag")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: update") {
		t.Fatalf("stream body missing update event: %q", body)
	}
}

func TestHTTPHandlerStreamWithoutKeyFilter(t *testing.T) {
	var calledAll bool
	svc := &fakeService{
		listEventsSinceFunc: func(_ context.Context, _ string, _ int64) ([]repository.FlagEvent, error) {
			calledAll = true
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
	if !calledAll {
		t.Fatal("expected ListEventsSince to be called when no key filter is provided")
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
