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
	ValidateToken(ctx context.Context, token string) error
}

// HTTPBearerAuthMiddleware enforces bearer-token auth for HTTP handlers.
func HTTPBearerAuthMiddleware(validator TokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := authorizeHTTP(r.Context(), r.Header.Get("Authorization"), validator); err != nil {
				writeHTTPUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UnaryBearerAuthInterceptor enforces bearer-token auth for unary gRPC requests.
func UnaryBearerAuthInterceptor(validator TokenValidator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authorizeGRPC(ctx, validator); err != nil {
			return nil, status.Error(codes.Unauthenticated, "unauthorized")
		}
		return handler(ctx, req)
	}
}

// StreamBearerAuthInterceptor enforces bearer-token auth for streaming gRPC requests.
func StreamBearerAuthInterceptor(validator TokenValidator) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authorizeGRPC(ss.Context(), validator); err != nil {
			return status.Error(codes.Unauthenticated, "unauthorized")
		}
		return handler(srv, ss)
	}
}

func authorizeHTTP(ctx context.Context, authorizationHeader string, validator TokenValidator) error {
	if validator == nil {
		return errors.New("token validator is nil")
	}
	if strings.TrimSpace(authorizationHeader) == "" {
		return errMissingAuthorizationHeader
	}

	token, err := parseBearerToken(authorizationHeader)
	if err != nil {
		return err
	}
	return validator.ValidateToken(ctx, token)
}

func authorizeGRPC(ctx context.Context, validator TokenValidator) error {
	if validator == nil {
		return errors.New("token validator is nil")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return errMissingAuthorizationHeader
	}

	authorizationHeaders := md.Get("authorization")
	if len(authorizationHeaders) == 0 {
		return errMissingAuthorizationHeader
	}

	for _, authorizationHeader := range authorizationHeaders {
		token, err := parseBearerToken(authorizationHeader)
		if err != nil {
			continue
		}
		return validator.ValidateToken(ctx, token)
	}

	return errInvalidAuthorizationHeader
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
