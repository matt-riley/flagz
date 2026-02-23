package middleware

import (
"context"
"errors"
"net/http"
"strings"

"google.golang.org/grpc"
"google.golang.org/grpc/codes"
"google.golang.org/grpc/metadata"
"google.golang.org/grpc/peer"
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

// AuthOption configures optional auth middleware parameters.
type AuthOption func(*authConfig)

type authConfig struct {
onFailure   func()
rateLimiter *RateLimiter
}

// WithOnAuthFailure registers a callback invoked on every authentication
// failure (e.g. to increment a Prometheus counter).
func WithOnAuthFailure(fn func()) AuthOption {
return func(c *authConfig) { c.onFailure = fn }
}

// WithRateLimiter attaches a per-IP rate limiter that throttles repeated
// authentication failures.
func WithRateLimiter(rl *RateLimiter) AuthOption {
return func(c *authConfig) { c.rateLimiter = rl }
}

// HTTPBearerAuthMiddleware enforces bearer-token auth for HTTP handlers.
func HTTPBearerAuthMiddleware(validator TokenValidator, opts ...AuthOption) func(http.Handler) http.Handler {
cfg := authConfig{}
for _, o := range opts {
o(&cfg)
}
return func(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
projectID, err := authorizeHTTP(r.Context(), r.Header.Get("Authorization"), validator)
if err != nil {
if cfg.onFailure != nil {
cfg.onFailure()
}
if cfg.rateLimiter != nil {
ip := ExtractIP(r.RemoteAddr)
if !cfg.rateLimiter.RecordFailureAndAllow(ip) {
http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
return
}
}
writeHTTPUnauthorized(w)
return
}
ctx := context.WithValue(r.Context(), projectIDKey, projectID)
if keyID := apiKeyIDFromBearer(r.Header.Get("Authorization")); keyID != "" {
ctx = context.WithValue(ctx, apiKeyIDKey, keyID)
}
next.ServeHTTP(w, r.WithContext(ctx))
})
}
}

// UnaryBearerAuthInterceptor enforces bearer-token auth for unary gRPC requests.
func UnaryBearerAuthInterceptor(validator TokenValidator, opts ...AuthOption) grpc.UnaryServerInterceptor {
cfg := authConfig{}
for _, o := range opts {
o(&cfg)
}
return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
projectID, err := authorizeGRPC(ctx, validator)
if err != nil {
if cfg.onFailure != nil {
cfg.onFailure()
}
if cfg.rateLimiter != nil {
if ip := extractGRPCPeerIP(ctx); ip != "" {
if !cfg.rateLimiter.RecordFailureAndAllow(ip) {
return nil, status.Error(codes.ResourceExhausted, "too many failed auth attempts")
}
}
}
return nil, status.Error(codes.Unauthenticated, "unauthorized")
}
newCtx := context.WithValue(ctx, projectIDKey, projectID)
if keyID := apiKeyIDFromGRPCMetadata(ctx); keyID != "" {
newCtx = context.WithValue(newCtx, apiKeyIDKey, keyID)
}
return handler(newCtx, req)
}
}

// StreamBearerAuthInterceptor enforces bearer-token auth for streaming gRPC requests.
func StreamBearerAuthInterceptor(validator TokenValidator, opts ...AuthOption) grpc.StreamServerInterceptor {
cfg := authConfig{}
for _, o := range opts {
o(&cfg)
}
return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
ctx := ss.Context()
projectID, err := authorizeGRPC(ctx, validator)
if err != nil {
if cfg.onFailure != nil {
cfg.onFailure()
}
if cfg.rateLimiter != nil {
if ip := extractGRPCPeerIP(ctx); ip != "" {
if !cfg.rateLimiter.RecordFailureAndAllow(ip) {
return status.Error(codes.ResourceExhausted, "too many failed auth attempts")
}
}
}
return status.Error(codes.Unauthenticated, "unauthorized")
}

ctx = context.WithValue(ctx, projectIDKey, projectID)
if keyID := apiKeyIDFromGRPCMetadata(ss.Context()); keyID != "" {
ctx = context.WithValue(ctx, apiKeyIDKey, keyID)
}

// Wrap the stream to inject context with project and API key IDs
wrappedStream := &wrappedServerStream{
ServerStream: ss,
ctx:          ctx,
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

const (
projectIDKey contextKey = "project_id"
apiKeyIDKey  contextKey = "api_key_id"
)

// ProjectIDFromContext retrieves the project ID from the context.
func ProjectIDFromContext(ctx context.Context) (string, bool) {
id, ok := ctx.Value(projectIDKey).(string)
return id, ok
}

// NewContextWithProjectID returns a new context with the given project ID.
func NewContextWithProjectID(ctx context.Context, projectID string) context.Context {
return context.WithValue(ctx, projectIDKey, projectID)
}

// APIKeyIDFromContext retrieves the API key ID from the context.
func APIKeyIDFromContext(ctx context.Context) (string, bool) {
id, ok := ctx.Value(apiKeyIDKey).(string)
return id, ok
}

// NewContextWithAPIKeyID returns a new context with the given API key ID.
func NewContextWithAPIKeyID(ctx context.Context, keyID string) context.Context {
return context.WithValue(ctx, apiKeyIDKey, keyID)
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
projectID, err := validator.ValidateToken(ctx, token)
if err != nil {
return "", err
}
if strings.TrimSpace(projectID) == "" {
return "", errInvalidAuthorizationHeader
}
return projectID, nil
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
if strings.TrimSpace(projectID) == "" {
return "", errInvalidAuthorizationHeader
}
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

// apiKeyIDFromBearer extracts the API key ID (the part before the dot) from
// a bearer token in format "Bearer keyID.secret".
func apiKeyIDFromBearer(authHeader string) string {
token, err := parseBearerToken(authHeader)
if err != nil {
return ""
}
keyID, _, ok := strings.Cut(token, ".")
if !ok || keyID == "" {
return ""
}
return keyID
}

// apiKeyIDFromGRPCMetadata extracts the API key ID from gRPC metadata.
func apiKeyIDFromGRPCMetadata(ctx context.Context) string {
md, ok := metadata.FromIncomingContext(ctx)
if !ok {
return ""
}
for _, h := range md.Get("authorization") {
if keyID := apiKeyIDFromBearer(h); keyID != "" {
return keyID
}
}
return ""
}

func extractGRPCPeerIP(ctx context.Context) string {
p, ok := peer.FromContext(ctx)
if !ok || p.Addr == nil {
return ""
}
return ExtractIP(p.Addr.String())
}
