package middleware

import (
	"context"

	"google.golang.org/grpc"
)

// testServerStream is a minimal grpc.ServerStream for testing interceptors.
type testServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *testServerStream) Context() context.Context {
	return s.ctx
}
