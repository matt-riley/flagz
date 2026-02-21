// Package server provides HTTP and gRPC transport layers for the flagz
// feature-flag service. Both transports delegate to the same [Service]
// interface so that behaviour stays consistent regardless of protocol.
//
// The HTTP layer serves a JSON REST API under /v1/*, SSE streaming at
// GET /v1/stream, plus /healthz and /metrics endpoints. The gRPC layer
// implements the FlagService proto, including server-streaming WatchFlag
// with optional per-key filtering.
package server

import (
	"context"

	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/service"
)

// Service defines the operations that both the HTTP and gRPC transports
// require from the business logic layer. It is implemented by
// [service.Service].
type Service interface {
	CreateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error)
	UpdateFlag(ctx context.Context, flag repository.Flag) (repository.Flag, error)
	GetFlag(ctx context.Context, key string) (repository.Flag, error)
	ListFlags(ctx context.Context) ([]repository.Flag, error)
	DeleteFlag(ctx context.Context, key string) error
	ResolveBoolean(ctx context.Context, key string, evalContext core.EvaluationContext, defaultValue bool) (bool, error)
	ResolveBatch(ctx context.Context, requests []service.ResolveRequest) ([]service.ResolveResult, error)
	ListEventsSince(ctx context.Context, eventID int64) ([]repository.FlagEvent, error)
	ListEventsSinceForKey(ctx context.Context, eventID int64, key string) ([]repository.FlagEvent, error)
}

var _ Service = (*service.Service)(nil)
