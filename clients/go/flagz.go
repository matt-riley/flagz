// Package flagz provides client interfaces and domain types for the flagz feature flag service.
//
// Use the sub-packages to create transport-specific clients:
//
//	import flagzhttp "github.com/matt-riley/flagz/clients/go/http"
//	import flagzgrpc "github.com/matt-riley/flagz/clients/go/grpc"
package flagz

import (
	"context"
	"time"
)

// FlagManager covers CRUD operations on feature flags.
type FlagManager interface {
	CreateFlag(ctx context.Context, flag Flag) (Flag, error)
	GetFlag(ctx context.Context, key string) (Flag, error)
	ListFlags(ctx context.Context) ([]Flag, error)
	UpdateFlag(ctx context.Context, flag Flag) (Flag, error)
	DeleteFlag(ctx context.Context, key string) error
}

// Evaluator covers flag resolution for a given evaluation context.
type Evaluator interface {
	Evaluate(ctx context.Context, key string, evalCtx EvaluationContext, defaultValue bool) (bool, error)
	EvaluateBatch(ctx context.Context, reqs []EvaluateRequest) ([]EvaluateResult, error)
}

// Streamer delivers real-time flag change events.
// The returned channel is closed when ctx is cancelled or the connection drops.
type Streamer interface {
	Stream(ctx context.Context, lastEventID int64) (<-chan FlagEvent, error)
}

// Flag is the domain representation of a feature flag.
type Flag struct {
	Key         string
	Description string
	Enabled     bool
	Variants    map[string]bool // may be nil
	Rules       []Rule          // may be nil
	CreatedAt   time.Time       // zero on gRPC (not on wire)
	UpdatedAt   time.Time       // zero on gRPC (not on wire)
}

// Rule is a targeting rule that determines flag evaluation.
type Rule struct {
	Attribute string
	Operator  string // "equals" | "in"
	Value     any
}

// EvaluationContext provides attribute data used when evaluating flag rules.
type EvaluationContext struct {
	Attributes map[string]any
}

// EvaluateRequest is a single flag evaluation request.
type EvaluateRequest struct {
	Key          string
	Context      EvaluationContext
	DefaultValue bool
}

// EvaluateResult is the outcome of a single flag evaluation.
type EvaluateResult struct {
	Key   string
	Value bool
}

// FlagEvent is a real-time notification of a flag change.
type FlagEvent struct {
	Type    string // "update" | "delete" | "error"
	Key     string
	Flag    *Flag // nil on delete/error
	EventID int64
}
