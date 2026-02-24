package service

import (
"context"
"encoding/json"
"fmt"
"testing"

"github.com/matt-riley/flagz/internal/core"
"github.com/matt-riley/flagz/internal/repository"
)

func BenchmarkListFlags(b *testing.B) {
ctx := context.Background()
repo := newFakeServiceRepository()

for i := range 100 {
repo.setFlag(repository.Flag{
ProjectID:   "default",
Key:         fmt.Sprintf("flag-%03d", i),
Description: fmt.Sprintf("benchmark flag %d", i),
Enabled:     i%3 != 0,
Variants:    json.RawMessage(`{}`),
Rules:       json.RawMessage(`[]`),
})
}

svc, err := New(ctx, repo)
if err != nil {
b.Fatalf("New() error = %v", err)
}

b.ResetTimer()
for b.Loop() {
_, _ = svc.ListFlags(ctx, "default")
}
}

func BenchmarkResolveBoolean(b *testing.B) {
ctx := context.Background()
repo := newFakeServiceRepository()
repo.setFlag(repository.Flag{
ProjectID:   "default",
Key:         "feature-rollout",
Description: "benchmark flag",
Enabled:     true,
Variants:    json.RawMessage(`{"default":false}`),
Rules:       json.RawMessage(`[{"attribute":"country","operator":"equals","value":"US"}]`),
})

svc, err := New(ctx, repo)
if err != nil {
b.Fatalf("New() error = %v", err)
}

evalCtx := core.EvaluationContext{
Attributes: map[string]any{"country": "US"},
}

b.ResetTimer()
for b.Loop() {
_, _ = svc.ResolveBoolean(ctx, "default", "feature-rollout", evalCtx, false)
}
}
