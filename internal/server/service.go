package server

import (
	"context"

	"github.com/matt-riley/flagz/internal/core"
	"github.com/matt-riley/flagz/internal/repository"
	"github.com/matt-riley/flagz/internal/service"
)

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
