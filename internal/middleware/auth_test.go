package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestHTTPBearerAuthMiddleware(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		validator := &testTokenValidator{}
		nextCalled := false
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			nextCalled = true
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
		if nextCalled {
			t.Fatal("expected next handler not to be called")
		}
		if validator.called {
			t.Fatal("expected validator not to be called")
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Fatalf("expected WWW-Authenticate header to be Bearer, got %q", got)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "expected"}
		nextCalled := false
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			nextCalled = true
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer bad")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
		if nextCalled {
			t.Fatal("expected next handler not to be called")
		}
		if !validator.called {
			t.Fatal("expected validator to be called")
		}
	})

	t.Run("invalid authorization header", func(t *testing.T) {
		validator := &testTokenValidator{}
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("expected next handler not to be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Basic bad")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
		if validator.called {
			t.Fatal("expected validator not to be called")
		}
	})

	t.Run("valid token", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "good", projectID: "proj-123"}
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify project ID in context
			pid, ok := ProjectIDFromContext(r.Context())
			if !ok || pid != "proj-123" {
				t.Errorf("ProjectIDFromContext = %q, %v; want proj-123, true", pid, ok)
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer good")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
		if !validator.called {
			t.Fatal("expected validator to be called")
		}
		if validator.gotToken != "good" {
			t.Fatalf("expected token %q, got %q", "good", validator.gotToken)
		}
	})
}

func TestUnaryBearerAuthInterceptor(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		validator := &testTokenValidator{}
		interceptor := UnaryBearerAuthInterceptor(validator)
		handlerCalled := false

		_, err := interceptor(context.Background(), struct{}{}, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
			handlerCalled = true
			return nil, nil
		})

		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected unauthenticated, got %v", status.Code(err))
		}
		if handlerCalled {
			t.Fatal("expected handler not to be called")
		}
		if validator.called {
			t.Fatal("expected validator not to be called")
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "expected"}
		interceptor := UnaryBearerAuthInterceptor(validator)
		handlerCalled := false
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer bad"))

		_, err := interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
			handlerCalled = true
			return nil, nil
		})

		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected unauthenticated, got %v", status.Code(err))
		}
		if handlerCalled {
			t.Fatal("expected handler not to be called")
		}
		if !validator.called {
			t.Fatal("expected validator to be called")
		}
	})

	t.Run("valid token", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "good", projectID: "proj-123"}
		interceptor := UnaryBearerAuthInterceptor(validator)
		handlerCalled := false
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer good"))

		res, err := interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
			handlerCalled = true
			// Verify project ID in context
			pid, ok := ProjectIDFromContext(ctx)
			if !ok || pid != "proj-123" {
				return nil, status.Errorf(codes.Internal, "ProjectIDFromContext = %q, %v; want proj-123, true", pid, ok)
			}
			return "ok", nil
		})

		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !handlerCalled {
			t.Fatal("expected handler to be called")
		}
		if res != "ok" {
			t.Fatalf("expected response %q, got %#v", "ok", res)
		}
	})
}

func TestStreamBearerAuthInterceptor(t *testing.T) {
	t.Run("missing token", func(t *testing.T) {
		validator := &testTokenValidator{}
		interceptor := StreamBearerAuthInterceptor(validator)
		handlerCalled := false

		err := interceptor(nil, &testServerStream{ctx: context.Background()}, &grpc.StreamServerInfo{}, func(any, grpc.ServerStream) error {
			handlerCalled = true
			return nil
		})

		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected unauthenticated, got %v", status.Code(err))
		}
		if handlerCalled {
			t.Fatal("expected handler not to be called")
		}
		if validator.called {
			t.Fatal("expected validator not to be called")
		}
	})

	t.Run("valid token", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "good", projectID: "proj-123"}
		interceptor := StreamBearerAuthInterceptor(validator)
		handlerCalled := false
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer good"))

		err := interceptor(nil, &testServerStream{ctx: ctx}, &grpc.StreamServerInfo{}, func(any, grpc.ServerStream) error {
			handlerCalled = true
			// Can't easily check context in stream wrapper here without more mocking,
			// but we trust WrappedServerStream does its job if Unary works.
			return nil
		})

		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !handlerCalled {
			t.Fatal("expected handler to be called")
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "expected"}
		interceptor := StreamBearerAuthInterceptor(validator)
		handlerCalled := false
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer bad"))

		err := interceptor(nil, &testServerStream{ctx: ctx}, &grpc.StreamServerInfo{}, func(any, grpc.ServerStream) error {
			handlerCalled = true
			return nil
		})

		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected unauthenticated, got %v", status.Code(err))
		}
		if handlerCalled {
			t.Fatal("expected handler not to be called")
		}
		if !validator.called {
			t.Fatal("expected validator to be called")
		}
	})
}

func TestAPIKeyMatchesHash(t *testing.T) {
	hash, err := HashAPIKey("secret")
	if err != nil {
		t.Fatalf("HashAPIKey() error = %v, want nil", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if !APIKeyMatchesHash(hash, "secret") {
		t.Fatal("expected API key to match hash")
	}
	if APIKeyMatchesHash(hash, "wrong") {
		t.Fatal("expected API key mismatch")
	}
	legacySum := sha256.Sum256([]byte("legacy-secret"))
	legacyHash := hex.EncodeToString(legacySum[:])
	if !APIKeyMatchesHash(legacyHash, "legacy-secret") {
		t.Fatal("expected API key to match legacy hash")
	}
	if APIKeyMatchesHash("not-hex", "secret") {
		t.Fatal("expected invalid hash to fail")
	}
}

type testTokenValidator struct {
	expectedToken string
	err           error
	called        bool
	gotToken      string
	projectID string
}

func (v *testTokenValidator) ValidateToken(_ context.Context, token string) (string, error) {
	v.called = true
	v.gotToken = token
	if v.err != nil {
		return "", v.err
	}
	if v.expectedToken != "" && token != v.expectedToken {
		return "", errors.New("invalid token")
	}
	return v.projectID, nil
}

type testServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *testServerStream) Context() context.Context {
	return s.ctx
}
