package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	flagz "github.com/matt-riley/flagz/clients/go"
	flagzhttp "github.com/matt-riley/flagz/clients/go/http"
)

// helpers

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func flagJSON(key string, enabled bool) string {
	return fmt.Sprintf(`{"flag":{"key":%q,"description":"desc","enabled":%v,"variants":null,"rules":null,"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}}`, key, enabled)
}

func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *flagzhttp.Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := flagzhttp.NewHTTPClient(flagzhttp.Config{
		BaseURL: srv.URL,
		APIKey:  "test-key",
	})
	return srv, c
}

func assertAuth(t *testing.T, r *http.Request) {
	t.Helper()
	got := r.Header.Get("Authorization")
	if got != "Bearer test-key" {
		t.Errorf("auth header: got %q, want %q", got, "Bearer test-key")
	}
}

// -- CRUD tests --------------------------------------------------------------

func TestCreateFlag(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/flags" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, flagJSON("my-flag", true))
	})
	f, err := c.CreateFlag(context.Background(), flagz.Flag{Key: "my-flag", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if f.Key != "my-flag" || !f.Enabled {
		t.Errorf("unexpected flag: %+v", f)
	}
	if f.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestGetFlag(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/flags/my-flag" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, flagJSON("my-flag", true))
	})
	f, err := c.GetFlag(context.Background(), "my-flag")
	if err != nil {
		t.Fatal(err)
	}
	if f.Key != "my-flag" {
		t.Errorf("got key %q", f.Key)
	}
}

func TestGetFlagNotFound(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	_, err := c.GetFlag(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *flagzhttp.APIError
	if !isAPIError(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 APIError, got %v", err)
	}
}

func TestGetFlagUnauthorized(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	_, err := c.GetFlag(context.Background(), "x")
	var apiErr *flagzhttp.APIError
	if !isAPIError(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 APIError, got %v", err)
	}
}

func TestListFlags(t *testing.T) {
	// Use a simpler server for list
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"flags":[{"key":"a","enabled":true},{"key":"b","enabled":false}]}`)
	}))
	defer srv.Close()
	cl := flagzhttp.NewHTTPClient(flagzhttp.Config{BaseURL: srv.URL, APIKey: "k"})
	flags, err := cl.ListFlags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 2 {
		t.Fatalf("want 2 flags, got %d", len(flags))
	}
}

func TestUpdateFlag(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodPut || r.URL.Path != "/v1/flags/my-flag" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, flagJSON("my-flag", false))
	})
	f, err := c.UpdateFlag(context.Background(), flagz.Flag{Key: "my-flag", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if f.Enabled {
		t.Error("expected Enabled=false")
	}
}

func TestDeleteFlag(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/flags/my-flag" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.DeleteFlag(context.Background(), "my-flag"); err != nil {
		t.Fatal(err)
	}
}

// -- Evaluate tests ----------------------------------------------------------

func TestEvaluate(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/evaluate" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["key"] != "my-flag" {
			t.Errorf("unexpected key: %v", body["key"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"key":"my-flag","value":true}`)
	})
	v, err := c.Evaluate(context.Background(), "my-flag", flagz.EvaluationContext{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !v {
		t.Error("expected true")
	}
}

func TestEvaluateBatch(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertAuth(t, r)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		reqs, ok := body["requests"].([]any)
		if !ok || len(reqs) != 2 {
			t.Errorf("expected 2 requests, got %v", body["requests"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"key":"a","value":true},{"key":"b","value":false}]}`)
	})
	results, err := c.EvaluateBatch(context.Background(), []flagz.EvaluateRequest{
		{Key: "a", DefaultValue: false},
		{Key: "b", DefaultValue: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Key != "a" || !results[0].Value {
		t.Errorf("unexpected results: %+v", results)
	}
}

// -- SSE streaming tests -----------------------------------------------------

func TestStream(t *testing.T) {
	events := []string{
		"id:1\nevent:update\ndata:{\"key\":\"flag-a\",\"enabled\":true}\n\n",
		"id:2\nevent:delete\ndata:{\"key\":\"flag-b\"}\n\n",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		for _, ev := range events {
			fmt.Fprint(w, ev)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := flagzhttp.NewHTTPClient(flagzhttp.Config{BaseURL: srv.URL, APIKey: "test-key"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := c.Stream(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	var received []flagz.FlagEvent
	for ev := range ch {
		received = append(received, ev)
	}

	if len(received) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(received), received)
	}
	if received[0].Type != "update" || received[0].EventID != 1 {
		t.Errorf("event 0: %+v", received[0])
	}
	if received[1].Type != "delete" || received[1].EventID != 2 {
		t.Errorf("event 1: %+v", received[1])
	}
}

func TestStreamLastEventIDHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Last-Event-ID")
		if got != "42" {
			t.Errorf("Last-Event-ID: got %q, want %q", got, "42")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// No events; just close.
	}))
	defer srv.Close()

	c := flagzhttp.NewHTTPClient(flagzhttp.Config{BaseURL: srv.URL, APIKey: "k"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := c.Stream(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
}

func TestStreamContextCancellation(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		flusher.Flush()
		// Hold open until the request context is cancelled.
		<-r.Context().Done()
		close(done)
	}))
	defer srv.Close()

	c := flagzhttp.NewHTTPClient(flagzhttp.Config{BaseURL: srv.URL, APIKey: "k"})
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := c.Stream(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Cancel after a brief moment.
	time.AfterFunc(100*time.Millisecond, cancel)

	// Channel should close without hanging.
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed as expected
			}
		case <-timeout:
			t.Fatal("timed out waiting for stream channel to close")
		}
	}
}

// -- helpers -----------------------------------------------------------------

func isAPIError(err error, target **flagzhttp.APIError) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*flagzhttp.APIError); ok {
		*target = e
		return true
	}
	return false
}

// Ensure Client satisfies interfaces at compile time.
var _ flagz.FlagManager = (*flagzhttp.Client)(nil)
var _ flagz.Evaluator = (*flagzhttp.Client)(nil)
var _ flagz.Streamer = (*flagzhttp.Client)(nil)

// Ensure types are usable.
var _ = strings.TrimSpace
