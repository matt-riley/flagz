package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matt-riley/flagz/internal/middleware"
)

func mustHashAPIKey(t *testing.T, apiKey string) string {
	t.Helper()

	hash, err := middleware.HashAPIKey(apiKey)
	if err != nil {
		t.Fatalf("HashAPIKey(%q) error = %v", apiKey, err)
	}

	return hash
}

func TestNewHTTPHandlerProtectsV1RoutesIncludingEscapedPaths(t *testing.T) {
	apiHandler := http.NewServeMux()
	apiHandler.HandleFunc("GET /v1/flags", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	validator := &fakeHTTPTokenValidator{projectID: "proj-test"}
	handler := newHTTPHandler(apiHandler, validator)

	t.Run("unauthenticated escaped v1 path is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/%76%31/flags", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Fatalf("WWW-Authenticate = %q, want %q", got, "Bearer")
		}
	})

	t.Run("authenticated v1 path is allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/flags", nil)
		req.Header.Set("Authorization", "Bearer key.secret")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if validator.calls != 1 {
			t.Fatalf("ValidateToken calls = %d, want %d", validator.calls, 1)
		}
	})
}

func TestNewHTTPHandlerKeepsPublicEndpointsAccessible(t *testing.T) {
	apiHandler := http.NewServeMux()
	apiHandler.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	apiHandler.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	apiHandler.HandleFunc("GET /debug", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newHTTPHandler(apiHandler, &fakeHTTPTokenValidator{err: errors.New("invalid token")})

	for _, path := range []string{"/healthz", "/metrics"} {
		path := path
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer bad")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}

	t.Run("non-whitelisted public routes are not exposed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/debug", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestAPIKeyTokenValidatorValidateToken(t *testing.T) {
	t.Run("nil validator", func(t *testing.T) {
		var validator *apiKeyTokenValidator
		_, err := validator.ValidateToken(context.Background(), "key.secret")
		if err == nil || err.Error() != "api key validator is nil" {
			t.Fatalf("ValidateToken() error = %v, want api key validator is nil", err)
		}
	})

	t.Run("invalid token format", func(t *testing.T) {
		lookup := &fakeAPIKeyHashLookup{}
		validator := &apiKeyTokenValidator{lookup: lookup}

		tests := []string{
			"",
			"no-delimiter",
			".secret",
			"key.",
		}
		for _, token := range tests {
			token := token
			t.Run(token, func(t *testing.T) {
				_, err := validator.ValidateToken(context.Background(), token)
				if err == nil || err.Error() != "invalid token format" {
					t.Fatalf("ValidateToken(%q) error = %v, want invalid token format", token, err)
				}
			})
		}

		if lookup.calls != 0 {
			t.Fatalf("ValidateAPIKey calls = %d, want 0", lookup.calls)
		}
	})

	t.Run("lookup error", func(t *testing.T) {
		lookupErr := errors.New("lookup failed")
		validator := &apiKeyTokenValidator{
			lookup: &fakeAPIKeyHashLookup{err: lookupErr},
		}

		_, err := validator.ValidateToken(context.Background(), "key.secret")
		if err == nil || !strings.Contains(err.Error(), "lookup key hash") {
			t.Fatalf("ValidateToken() error = %v, want wrapped lookup error", err)
		}
		if !errors.Is(err, lookupErr) {
			t.Fatalf("ValidateToken() error = %v, want wrapped %v", err, lookupErr)
		}
	})

	t.Run("invalid secret", func(t *testing.T) {
		lookup := &fakeAPIKeyHashLookup{
			hash: mustHashAPIKey(t, "expected-secret"),
		}
		validator := &apiKeyTokenValidator{lookup: lookup}

		_, err := validator.ValidateToken(context.Background(), "key.bad-secret")
		if err == nil || err.Error() != "invalid token" {
			t.Fatalf("ValidateToken() error = %v, want invalid token", err)
		}
		if lookup.gotID != "key" {
			t.Fatalf("ValidateAPIKey id = %q, want %q", lookup.gotID, "key")
		}
	})

	t.Run("valid token", func(t *testing.T) {
		lookup := &fakeAPIKeyHashLookup{
			hash:      mustHashAPIKey(t, "good-secret"),
			projectID: "proj-123",
		}
		validator := &apiKeyTokenValidator{lookup: lookup}

		pid, err := validator.ValidateToken(context.Background(), "my-key.good-secret")
		if err != nil {
			t.Fatalf("ValidateToken() error = %v, want nil", err)
		}
		if pid != "proj-123" {
			t.Fatalf("ValidateToken() projectID = %q, want proj-123", pid)
		}
		if lookup.gotID != "my-key" {
			t.Fatalf("ValidateAPIKey id = %q, want %q", lookup.gotID, "my-key")
		}
	})
}

type fakeAPIKeyHashLookup struct {
	hash      string
	projectID string
	err       error
	calls     int
	gotID     string
}

type fakeHTTPTokenValidator struct {
	err       error
	calls     int
	projectID string
}

func (f *fakeAPIKeyHashLookup) ValidateAPIKey(_ context.Context, id string) (string, string, error) {
	f.calls++
	f.gotID = id
	if f.err != nil {
		return "", "", f.err
	}
	return f.hash, f.projectID, nil
}

func (f *fakeHTTPTokenValidator) ValidateToken(_ context.Context, _ string) (string, error) {
	f.calls++
	return f.projectID, f.err
}
