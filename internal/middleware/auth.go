package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	errMissingAuthorizationHeader = errors.New("missing authorization header")
	errInvalidAuthorizationHeader = errors.New("invalid authorization header")
)

// TokenValidator validates a bearer token.
type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (string, error)
}

// HTTPBearerAuthMiddleware enforces bearer-token auth for HTTP handlers.
func HTTPBearerAuthMiddleware(validator TokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			projectID, err := authorizeHTTP(r.Context(), r.Header.Get("Authorization"), validator)
			if err != nil {
				writeHTTPUnauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), projectIDKey, projectID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UnaryBearerAuthInterceptor enforces bearer-token auth for unary gRPC requests.
func UnaryBearerAuthInterceptor(validator TokenValidator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		projectID, err := authorizeGRPC(ctx, validator)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "unauthorized")
		}
		newCtx := context.WithValue(ctx, projectIDKey, projectID)
		return handler(newCtx, req)
	}
}

// StreamBearerAuthInterceptor enforces bearer-token auth for streaming gRPC requests.
func StreamBearerAuthInterceptor(validator TokenValidator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		projectID, err := authorizeGRPC(ss.Context(), validator)
		if err != nil {
			return status.Error(codes.Unauthenticated, "unauthorized")
		}
		
		// Wrap the stream to inject context with project ID
		wrappedStream := &wrappedServerStream{
			ServerStream: ss,
			ctx:          context.WithValue(ss.Context(), projectIDKey, projectID),
		}
		
		return handler(srv, wrappedStream)
	}
}

type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

type contextKey string

const projectIDKey contextKey = "project_id"

// ProjectIDFromContext retrieves the project ID from the context.
func ProjectIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(projectIDKey).(string)
	return id, ok
}

// NewContextWithProjectID returns a new context with the given project ID.
func NewContextWithProjectID(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, projectIDKey, projectID)
}

func authorizeHTTP(ctx context.Context, authorizationHeader string, validator TokenValidator) (string, error) {
	if validator == nil {
		return "", errors.New("token validator is nil")
	}
	if strings.TrimSpace(authorizationHeader) == "" {
		return "", errMissingAuthorizationHeader
	}

	token, err := parseBearerToken(authorizationHeader)
	if err != nil {
		return "", err
	}
	return validator.ValidateToken(ctx, token)
}

func authorizeGRPC(ctx context.Context, validator TokenValidator) (string, error) {
	if validator == nil {
		return "", errors.New("token validator is nil")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errMissingAuthorizationHeader
	}

	authorizationHeaders := md.Get("authorization")
	if len(authorizationHeaders) == 0 {
		return "", errMissingAuthorizationHeader
	}

	for _, authorizationHeader := range authorizationHeaders {
		token, err := parseBearerToken(authorizationHeader)
		if err != nil {
			continue
		}
		projectID, err := validator.ValidateToken(ctx, token)
		if err == nil {
			return projectID, nil
		}
	}

	return "", errInvalidAuthorizationHeader
}

func parseBearerToken(authorizationHeader string) (string, error) {
	parts := strings.Fields(authorizationHeader)
	if len(parts) != 2 {
		return "", errInvalidAuthorizationHeader
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return "", errInvalidAuthorizationHeader
	}
	if parts[1] == "" {
		return "", errInvalidAuthorizationHeader
	}

	return parts[1], nil
}

func writeHTTPUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
}
