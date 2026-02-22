package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

type logContextKey string

const (
	requestIDKey logContextKey = "request_id"
	loggerKey    logContextKey = "logger"
)

// RequestIDFromContext retrieves the request ID from the context.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey).(string)
	return id, ok
}

// LoggerFromContext retrieves the request-scoped logger from the context.
// Falls back to slog.Default() if none is set.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

func generateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// Unwrap supports http.ResponseController and middleware that unwrap writers.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// HTTPRequestLogging returns middleware that logs each HTTP request with a
// unique request ID, method, path, status code, and duration.
func HTTPRequestLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := generateRequestID()
			reqLogger := logger.With(slog.String("request_id", reqID))

			ctx := context.WithValue(r.Context(), requestIDKey, reqID)
			ctx = context.WithValue(ctx, loggerKey, reqLogger)

			reqLogger.InfoContext(ctx, "request started",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote_addr", r.RemoteAddr),
			)

			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(wrapped, r.WithContext(ctx))
			duration := time.Since(start)

			reqLogger.InfoContext(ctx, "request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status_code", wrapped.statusCode),
				slog.Float64("duration_ms", float64(duration.Nanoseconds())/1e6),
			)
		})
	}
}

// UnaryRequestLoggingInterceptor returns a gRPC unary server interceptor that
// logs each call with a unique request ID, method, status code, and duration.
func UnaryRequestLoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		reqID := generateRequestID()
		reqLogger := logger.With(slog.String("request_id", reqID))

		ctx = context.WithValue(ctx, requestIDKey, reqID)
		ctx = context.WithValue(ctx, loggerKey, reqLogger)

		reqLogger.InfoContext(ctx, "request started",
			slog.String("method", info.FullMethod),
		)

		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		code := status.Code(err)
		reqLogger.InfoContext(ctx, "request completed",
			slog.String("method", info.FullMethod),
			slog.String("status_code", code.String()),
			slog.Float64("duration_ms", float64(duration.Nanoseconds())/1e6),
		)

		return resp, err
	}
}
