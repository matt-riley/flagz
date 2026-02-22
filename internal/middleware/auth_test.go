package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

	t.Run("valid token with empty project ID", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "good", projectID: ""}
		nextCalled := false
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			nextCalled = true
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer good")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
		if nextCalled {
			t.Fatal("expected next handler not to be called")
		}
	})

	t.Run("valid token with whitespace project ID", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "good", projectID: "   "}
		nextCalled := false
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			nextCalled = true
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer good")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
		if nextCalled {
			t.Fatal("expected next handler not to be called")
		}
	})

	t.Run("valid token", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "keyid1.secret", projectID: "proj-123"}
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify project ID in context
			pid, ok := ProjectIDFromContext(r.Context())
			if !ok || pid != "proj-123" {
				t.Errorf("ProjectIDFromContext = %q, %v; want proj-123, true", pid, ok)
			}
			// Verify API key ID in context
			kid, ok := APIKeyIDFromContext(r.Context())
			if !ok || kid != "keyid1" {
				t.Errorf("APIKeyIDFromContext = %q, %v; want keyid1, true", kid, ok)
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer keyid1.secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
		if !validator.called {
			t.Fatal("expected validator to be called")
		}
		if validator.gotToken != "keyid1.secret" {
			t.Fatalf("expected token %q, got %q", "keyid1.secret", validator.gotToken)
		}
	})

	t.Run("valid token without dot has no key ID", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "nodottoken", projectID: "proj-123"}
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pid, ok := ProjectIDFromContext(r.Context())
			if !ok || pid != "proj-123" {
				t.Errorf("ProjectIDFromContext = %q, %v; want proj-123, true", pid, ok)
			}
			// No dot in token means no API key ID extracted
			_, ok = APIKeyIDFromContext(r.Context())
			if ok {
				t.Error("expected no API key ID in context for token without dot")
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer nodottoken")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
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

	t.Run("valid token with empty project ID returns unauthenticated", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "good", projectID: ""}
		interceptor := UnaryBearerAuthInterceptor(validator)
		handlerCalled := false
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer good"))

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
	})

	t.Run("valid token", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "keyid2.secret", projectID: "proj-123"}
		interceptor := UnaryBearerAuthInterceptor(validator)
		handlerCalled := false
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer keyid2.secret"))

		res, err := interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
			handlerCalled = true
			pid, ok := ProjectIDFromContext(ctx)
			if !ok || pid != "proj-123" {
				return nil, status.Errorf(codes.Internal, "ProjectIDFromContext = %q, %v; want proj-123, true", pid, ok)
			}
			kid, ok := APIKeyIDFromContext(ctx)
			if !ok || kid != "keyid2" {
				return nil, status.Errorf(codes.Internal, "APIKeyIDFromContext = %q, %v; want keyid2, true", kid, ok)
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

	t.Run("valid token without dot has no key ID", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "nodottoken", projectID: "proj-123"}
		interceptor := UnaryBearerAuthInterceptor(validator)
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer nodottoken"))

		_, err := interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{}, func(ctx context.Context, _ any) (any, error) {
			pid, ok := ProjectIDFromContext(ctx)
			if !ok || pid != "proj-123" {
				t.Errorf("ProjectIDFromContext = %q, %v; want proj-123, true", pid, ok)
			}
			_, ok = APIKeyIDFromContext(ctx)
			if ok {
				t.Error("expected no API key ID in context for token without dot")
			}
			return "ok", nil
		})

		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
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
		validator := &testTokenValidator{expectedToken: "keyid3.secret", projectID: "proj-123"}
		interceptor := StreamBearerAuthInterceptor(validator)
		handlerCalled := false
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer keyid3.secret"))

		err := interceptor(nil, &testServerStream{ctx: ctx}, &grpc.StreamServerInfo{}, func(_ any, ss grpc.ServerStream) error {
			handlerCalled = true
			pid, ok := ProjectIDFromContext(ss.Context())
			if !ok || pid != "proj-123" {
				t.Errorf("ProjectIDFromContext = %q, %v; want proj-123, true", pid, ok)
			}
			kid, ok := APIKeyIDFromContext(ss.Context())
			if !ok || kid != "keyid3" {
				t.Errorf("APIKeyIDFromContext = %q, %v; want keyid3, true", kid, ok)
			}
			return nil
		})

		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !handlerCalled {
			t.Fatal("expected handler to be called")
		}
	})

	t.Run("valid token without dot has no key ID", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "nodottoken", projectID: "proj-123"}
		interceptor := StreamBearerAuthInterceptor(validator)
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer nodottoken"))

		err := interceptor(nil, &testServerStream{ctx: ctx}, &grpc.StreamServerInfo{}, func(_ any, ss grpc.ServerStream) error {
			pid, ok := ProjectIDFromContext(ss.Context())
			if !ok || pid != "proj-123" {
				t.Errorf("ProjectIDFromContext = %q, %v; want proj-123, true", pid, ok)
			}
			_, ok = APIKeyIDFromContext(ss.Context())
			if ok {
				t.Error("expected no API key ID in context for token without dot")
			}
			return nil
		})

		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
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

func TestHTTPOnAuthFailure(t *testing.T) {
	t.Run("callback invoked on invalid token", func(t *testing.T) {
		var counter atomic.Int64
		validator := &testTokenValidator{expectedToken: "expected"}
		handler := HTTPBearerAuthMiddleware(validator, WithOnAuthFailure(func() { counter.Add(1) }))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("expected next handler not to be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer bad")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
		if got := counter.Load(); got != 1 {
			t.Fatalf("expected callback count 1, got %d", got)
		}
	})

	t.Run("callback invoked on missing Authorization header", func(t *testing.T) {
		var counter atomic.Int64
		validator := &testTokenValidator{}
		handler := HTTPBearerAuthMiddleware(validator, WithOnAuthFailure(func() { counter.Add(1) }))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("expected next handler not to be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
		if got := counter.Load(); got != 1 {
			t.Fatalf("expected callback count 1, got %d", got)
		}
	})

	t.Run("callback NOT invoked on successful auth", func(t *testing.T) {
		var counter atomic.Int64
		validator := &testTokenValidator{expectedToken: "good", projectID: "proj-123"}
		handler := HTTPBearerAuthMiddleware(validator, WithOnAuthFailure(func() { counter.Add(1) }))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer good")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected %d, got %d", http.StatusNoContent, rec.Code)
		}
		if got := counter.Load(); got != 0 {
			t.Fatalf("expected callback count 0, got %d", got)
		}
	})

	t.Run("nil callback does not panic", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "expected"}
		handler := HTTPBearerAuthMiddleware(validator)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("expected next handler not to be called")
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer bad")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rec.Code)
		}
	})
}

func TestUnaryOnAuthFailure(t *testing.T) {
	t.Run("callback invoked on auth failure", func(t *testing.T) {
		var counter atomic.Int64
		validator := &testTokenValidator{expectedToken: "expected"}
		interceptor := UnaryBearerAuthInterceptor(validator, WithOnAuthFailure(func() { counter.Add(1) }))
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer bad"))

		_, err := interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
			t.Fatal("expected handler not to be called")
			return nil, nil
		})

		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected unauthenticated, got %v", status.Code(err))
		}
		if got := counter.Load(); got != 1 {
			t.Fatalf("expected callback count 1, got %d", got)
		}
	})

	t.Run("callback NOT invoked on success", func(t *testing.T) {
		var counter atomic.Int64
		validator := &testTokenValidator{expectedToken: "good", projectID: "proj-123"}
		interceptor := UnaryBearerAuthInterceptor(validator, WithOnAuthFailure(func() { counter.Add(1) }))
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer good"))

		_, err := interceptor(ctx, struct{}{}, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
			return "ok", nil
		})

		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if got := counter.Load(); got != 0 {
			t.Fatalf("expected callback count 0, got %d", got)
		}
	})
}

func TestStreamOnAuthFailure(t *testing.T) {
	t.Run("callback invoked on auth failure", func(t *testing.T) {
		var counter atomic.Int64
		validator := &testTokenValidator{expectedToken: "expected"}
		interceptor := StreamBearerAuthInterceptor(validator, WithOnAuthFailure(func() { counter.Add(1) }))

		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer bad"))
		stream := &testServerStream{ctx: ctx}

		err := interceptor(struct{}{}, stream, &grpc.StreamServerInfo{}, func(srv any, ss grpc.ServerStream) error {
			t.Fatal("expected handler not to be called")
			return nil
		})

		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected unauthenticated, got %v", status.Code(err))
		}
		if got := counter.Load(); got != 1 {
			t.Fatalf("expected callback count 1, got %d", got)
		}
	})

	t.Run("callback NOT invoked on success", func(t *testing.T) {
		var counter atomic.Int64
		validator := &testTokenValidator{expectedToken: "good", projectID: "proj-123"}
		interceptor := StreamBearerAuthInterceptor(validator, WithOnAuthFailure(func() { counter.Add(1) }))

		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer good"))
		stream := &testServerStream{ctx: ctx}

		err := interceptor(struct{}{}, stream, &grpc.StreamServerInfo{}, func(srv any, ss grpc.ServerStream) error {
			return nil
		})

		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if got := counter.Load(); got != 0 {
			t.Fatalf("expected callback count 0, got %d", got)
		}
	})

	t.Run("nil callback does not panic", func(t *testing.T) {
		validator := &testTokenValidator{expectedToken: "expected"}
		interceptor := StreamBearerAuthInterceptor(validator, WithOnAuthFailure(nil))

		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer bad"))
		stream := &testServerStream{ctx: ctx}

		err := interceptor(struct{}{}, stream, &grpc.StreamServerInfo{}, func(srv any, ss grpc.ServerStream) error {
			t.Fatal("expected handler not to be called")
			return nil
		})

		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("expected unauthenticated, got %v", status.Code(err))
		}
	})
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
