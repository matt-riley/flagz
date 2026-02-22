package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHTTPRequestLogging(t *testing.T) {
	t.Run("logs request with request_id in context", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

		var capturedReqID string
		var capturedLogger *slog.Logger
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := RequestIDFromContext(r.Context())
			if !ok {
				t.Fatal("expected request_id in context")
			}
			capturedReqID = id
			capturedLogger = LoggerFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		handler := HTTPRequestLogging(logger)(inner)
		req := httptest.NewRequest(http.MethodGet, "/v1/flags", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if capturedReqID == "" {
			t.Fatal("expected non-empty request_id")
		}
		if len(capturedReqID) != 16 { // 8 bytes hex-encoded
			t.Fatalf("expected 16-char request_id, got %d: %q", len(capturedReqID), capturedReqID)
		}
		if capturedLogger == nil {
			t.Fatal("expected logger in context")
		}

		output := buf.String()
		if !strings.Contains(output, "request started") {
			t.Fatalf("expected 'request started' in log output, got: %s", output)
		}
		if !strings.Contains(output, "request completed") {
			t.Fatalf("expected 'request completed' in log output, got: %s", output)
		}
		if !strings.Contains(output, capturedReqID) {
			t.Fatalf("expected request_id %q in log output, got: %s", capturedReqID, output)
		}
		if !strings.Contains(output, "method=GET") {
			t.Fatalf("expected method=GET in log output, got: %s", output)
		}
		if !strings.Contains(output, "path=/v1/flags") {
			t.Fatalf("expected path=/v1/flags in log output, got: %s", output)
		}
		if !strings.Contains(output, "status_code=200") {
			t.Fatalf("expected status_code=200 in log output, got: %s", output)
		}
		if !strings.Contains(output, "duration_ms=") {
			t.Fatalf("expected duration_ms in log output, got: %s", output)
		}
	})

	t.Run("captures non-200 status code", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

		inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})

		handler := HTTPRequestLogging(logger)(inner)
		req := httptest.NewRequest(http.MethodGet, "/v1/flags/missing", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
		if !strings.Contains(buf.String(), "status_code=404") {
			t.Fatalf("expected status_code=404 in log output, got: %s", buf.String())
		}
	})

	t.Run("captures status from Write without explicit WriteHeader", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

		inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("hello"))
		})

		handler := HTTPRequestLogging(logger)(inner)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !strings.Contains(buf.String(), "status_code=200") {
			t.Fatalf("expected status_code=200 in log output, got: %s", buf.String())
		}
	})

	t.Run("nil logger uses default", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		handler := HTTPRequestLogging(nil)(inner)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		// Should not panic
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}

func TestUnaryRequestLoggingInterceptor(t *testing.T) {
	t.Run("logs gRPC request with request_id", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

		var capturedReqID string
		interceptor := UnaryRequestLoggingInterceptor(logger)
		info := &grpc.UnaryServerInfo{FullMethod: "/flagz.v1.FlagService/GetFlag"}

		resp, err := interceptor(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
			id, ok := RequestIDFromContext(ctx)
			if !ok {
				t.Fatal("expected request_id in context")
			}
			capturedReqID = id
			return "ok", nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "ok" {
			t.Fatalf("expected response 'ok', got %v", resp)
		}
		if capturedReqID == "" || len(capturedReqID) != 16 {
			t.Fatalf("expected 16-char request_id, got %q", capturedReqID)
		}

		output := buf.String()
		if !strings.Contains(output, "request started") {
			t.Fatalf("expected 'request started' in log output, got: %s", output)
		}
		if !strings.Contains(output, "request completed") {
			t.Fatalf("expected 'request completed' in log output, got: %s", output)
		}
		if !strings.Contains(output, "/flagz.v1.FlagService/GetFlag") {
			t.Fatalf("expected method in log output, got: %s", output)
		}
		if !strings.Contains(output, "status_code=0") {
			t.Fatalf("expected status_code=0 in log output, got: %s", output)
		}
		if !strings.Contains(output, "duration_ms=") {
			t.Fatalf("expected duration_ms in log output, got: %s", output)
		}
	})

	t.Run("logs error status code", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

		interceptor := UnaryRequestLoggingInterceptor(logger)
		info := &grpc.UnaryServerInfo{FullMethod: "/flagz.v1.FlagService/GetFlag"}

		_, err := interceptor(context.Background(), "req", info, func(context.Context, any) (any, error) {
			return nil, status.Error(codes.NotFound, "not found")
		})

		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(buf.String(), "status_code=5") {
			t.Fatalf("expected status_code=5 in log output, got: %s", buf.String())
		}
	})

	t.Run("nil logger uses default", func(t *testing.T) {
		interceptor := UnaryRequestLoggingInterceptor(nil)
		info := &grpc.UnaryServerInfo{FullMethod: "/test"}

		// Should not panic
		resp, err := interceptor(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
			return "ok", nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "ok" {
			t.Fatalf("expected 'ok', got %v", resp)
		}
	})
}

func TestRequestIDFromContext(t *testing.T) {
	t.Run("returns false for empty context", func(t *testing.T) {
		_, ok := RequestIDFromContext(context.Background())
		if ok {
			t.Fatal("expected false for empty context")
		}
	})

	t.Run("returns value when set", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestIDKey, "test-id")
		id, ok := RequestIDFromContext(ctx)
		if !ok {
			t.Fatal("expected true")
		}
		if id != "test-id" {
			t.Fatalf("expected 'test-id', got %q", id)
		}
	})
}

func TestLoggerFromContext(t *testing.T) {
	t.Run("returns default for empty context", func(t *testing.T) {
		logger := LoggerFromContext(context.Background())
		if logger == nil {
			t.Fatal("expected non-nil logger")
		}
	})

	t.Run("returns custom logger when set", func(t *testing.T) {
		custom := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
		ctx := context.WithValue(context.Background(), loggerKey, custom)
		got := LoggerFromContext(ctx)
		if got != custom {
			t.Fatal("expected custom logger")
		}
	})
}

func TestResponseWriterCapture(t *testing.T) {
	t.Run("double WriteHeader only records first", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

		rw.WriteHeader(http.StatusCreated)
		rw.WriteHeader(http.StatusInternalServerError) // should be ignored

		if rw.statusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rw.statusCode)
		}
	})

	t.Run("Unwrap returns underlying writer", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: rec}
		if rw.Unwrap() != rec {
			t.Fatal("Unwrap should return underlying ResponseWriter")
		}
	})
}

func TestStreamRequestLoggingInterceptor(t *testing.T) {
	t.Run("logs stream with request_id", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

		interceptor := StreamRequestLoggingInterceptor(logger)
		info := &grpc.StreamServerInfo{FullMethod: "/flagz.v1.FlagService/WatchFlag"}
		stream := &testServerStream{ctx: context.Background()}

		err := interceptor(struct{}{}, stream, info, func(srv any, ss grpc.ServerStream) error {
			id, ok := RequestIDFromContext(ss.Context())
			if !ok {
				t.Fatal("expected request_id in context")
			}
			if len(id) != 16 {
				t.Fatalf("expected 16-char request_id, got %q", id)
			}
			return nil
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "stream started") {
			t.Fatalf("expected 'stream started' in log output, got: %s", output)
		}
		if !strings.Contains(output, "stream completed") {
			t.Fatalf("expected 'stream completed' in log output, got: %s", output)
		}
		if !strings.Contains(output, "/flagz.v1.FlagService/WatchFlag") {
			t.Fatalf("expected method in log output, got: %s", output)
		}
		if !strings.Contains(output, "status_code=0") {
			t.Fatalf("expected status_code=0 in log output, got: %s", output)
		}
	})

	t.Run("logs error status code", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

		interceptor := StreamRequestLoggingInterceptor(logger)
		info := &grpc.StreamServerInfo{FullMethod: "/flagz.v1.FlagService/WatchFlag"}
		stream := &testServerStream{ctx: context.Background()}

		err := interceptor(struct{}{}, stream, info, func(srv any, ss grpc.ServerStream) error {
			return status.Error(codes.Internal, "oops")
		})

		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(buf.String(), "status_code=13") {
			t.Fatalf("expected status_code=13 in log output, got: %s", buf.String())
		}
	})

	t.Run("nil logger uses default", func(t *testing.T) {
		interceptor := StreamRequestLoggingInterceptor(nil)
		info := &grpc.StreamServerInfo{FullMethod: "/test"}
		stream := &testServerStream{ctx: context.Background()}

		err := interceptor(struct{}{}, stream, info, func(srv any, ss grpc.ServerStream) error {
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
