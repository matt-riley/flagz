# flagz Go client

> Type-safe, dual-transport Go client for the [flagz](../../README.md) feature flag service.
> Flip flags, not tables.

[![Go Reference](https://pkg.go.dev/badge/github.com/matt-riley/flagz/clients/go.svg)](https://pkg.go.dev/github.com/matt-riley/flagz/clients/go)
[![CI](https://github.com/matt-riley/flagz/actions/workflows/ci.yml/badge.svg)](https://github.com/matt-riley/flagz/actions/workflows/ci.yml)

## Features

- **Dual transport** — HTTP (REST + SSE) and gRPC from the same interfaces
- **Full CRUD** — create, get, list, update, delete feature flags
- **Flag evaluation** — single and batch evaluation with targeting rules
- **Real-time streaming** — SSE (HTTP) and server-streaming RPC (gRPC) for live flag changes
- **Type-safe** — shared `flagz.Flag`, `flagz.Rule`, `flagz.EvaluationContext` types across transports
- **Interface-driven** — `FlagManager`, `Evaluator`, `Streamer` interfaces for easy mocking
- **Thread-safe** — clients are safe for concurrent use from multiple goroutines

## Install

```sh
go get github.com/matt-riley/flagz/clients/go@latest
```

## Quick start — HTTP

```go
import (
    flagz     "github.com/matt-riley/flagz/clients/go"
    flagzhttp "github.com/matt-riley/flagz/clients/go/http"
)

client := flagzhttp.NewHTTPClient(flagzhttp.Config{
    BaseURL: "http://localhost:8080",
    APIKey:  "your-api-key-id.your-secret",
})

// Evaluate a flag
enabled, err := client.Evaluate(ctx, "my-feature", flagz.EvaluationContext{}, false)

// CRUD
flag, err := client.GetFlag(ctx, "my-feature")
flags, err := client.ListFlags(ctx)

// Stream real-time changes (channel closed when ctx is cancelled)
events, err := client.Stream(ctx, 0)
for ev := range events {
    fmt.Printf("event: %s key: %s\n", ev.Type, ev.Key)
}
```

## Quick start — gRPC

```go
import (
    flagz     "github.com/matt-riley/flagz/clients/go"
    flagzgrpc "github.com/matt-riley/flagz/clients/go/grpc"
)

client, err := flagzgrpc.NewGRPCClient(flagzgrpc.Config{
    Address: "localhost:9090",
    APIKey:  "your-api-key-id.your-secret",
})
if err != nil {
    log.Fatal(err)
}
defer client.Close()

enabled, err := client.Evaluate(ctx, "my-feature", flagz.EvaluationContext{}, false)
```

## Batch evaluation

```go
results, err := client.EvaluateBatch(ctx, []flagz.EvaluateRequest{
    {Key: "feature-a", DefaultValue: false},
    {Key: "feature-b", DefaultValue: true},
})
for _, r := range results {
    fmt.Printf("%s = %v\n", r.Key, r.Value)
}
```

## Extending

Both clients implement the shared interfaces defined in the root `flagz` package:

```go
var _ flagz.FlagManager = client // createFlag, getFlag, listFlags, updateFlag, deleteFlag
var _ flagz.Evaluator   = client // evaluate, evaluateBatch
var _ flagz.Streamer    = client // stream
```

You can swap transports, wrap clients, or provide mocks in tests by implementing these interfaces.

## TLS (gRPC)

Pass `grpc.DialOption` entries via `Config.DialOpts`:

```go
import "google.golang.org/grpc/credentials"

creds, _ := credentials.NewClientTLSFromFile("ca.crt", "")
client, err := flagzgrpc.NewGRPCClient(flagzgrpc.Config{
    Address:  "flagz.example.com:443",
    APIKey:   "...",
    DialOpts: []grpc.DialOption{grpc.WithTransportCredentials(creds)},
})
```

## Complete CRUD example

Life-cycle of a flag from cradle to grave — in one function:

```go
package main

import (
    "context"
    "fmt"
    "log"

    flagz     "github.com/matt-riley/flagz/clients/go"
    flagzhttp "github.com/matt-riley/flagz/clients/go/http"
)

func main() {
    ctx := context.Background()
    client := flagzhttp.NewHTTPClient(flagzhttp.Config{
        BaseURL: "http://localhost:8080",
        APIKey:  "your-api-key-id.your-secret",
    })

    // Create
    created, err := client.CreateFlag(ctx, flagz.Flag{
        Key:         "dark-mode",
        Description: "Enable dark mode for all users",
        Enabled:     false,
        Rules: []flagz.Rule{
            {Attribute: "plan", Operator: "equals", Value: "premium"},
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("created: %s (enabled=%v)\n", created.Key, created.Enabled)

    // Get
    flag, err := client.GetFlag(ctx, "dark-mode")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("got: %s — %s\n", flag.Key, flag.Description)

    // List
    flags, err := client.ListFlags(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("total flags: %d\n", len(flags))

    // Update — flip it on
    flag.Enabled = true
    flag.Description = "Dark mode — now live!"
    updated, err := client.UpdateFlag(ctx, flag)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("updated: %s (enabled=%v)\n", updated.Key, updated.Enabled)

    // Delete
    if err := client.DeleteFlag(ctx, "dark-mode"); err != nil {
        log.Fatal(err)
    }
    fmt.Println("deleted: dark-mode 🗑️")
}
```

## Error handling

Nobody likes surprises at 2 AM. Both clients surface errors in predictable ways.

### HTTP client

The HTTP client returns `*http.APIError` for server errors, which exposes the status code and response body:

```go
import (
    "errors"
    flagzhttp "github.com/matt-riley/flagz/clients/go/http"
)

flag, err := client.GetFlag(ctx, "nope")
if err != nil {
    var apiErr *flagzhttp.APIError
    if errors.As(err, &apiErr) {
        switch apiErr.StatusCode {
        case 404:
            fmt.Println("flag not found")
        case 401:
            fmt.Println("check your API key")
        case 409:
            fmt.Println("flag already exists")
        default:
            fmt.Printf("HTTP %d: %s\n", apiErr.StatusCode, apiErr.Message)
        }
    } else {
        // Network error, DNS failure, context cancelled, etc.
        fmt.Printf("transport error: %v\n", err)
    }
}
```

### gRPC client

The gRPC client returns standard gRPC status errors. Use `google.golang.org/grpc/status` to inspect them:

```go
import "google.golang.org/grpc/status"

flag, err := client.GetFlag(ctx, "nope")
if err != nil {
    if st, ok := status.FromError(err); ok {
        fmt.Printf("gRPC %s: %s\n", st.Code(), st.Message())
    }
}
```

### Common HTTP status codes

| Status | Meaning |
|--------|---------|
| `400`  | Bad request — malformed JSON, missing fields, or providing both `key` and `requests` to evaluate |
| `401`  | Unauthorized — invalid or missing API key |
| `404`  | Flag not found |
| `409`  | Conflict — flag with that key already exists |
| `500`  | Internal server error |

## Streaming with reconnection

Streams don't last forever (yet). Here's a resilient pattern that reconnects on errors, using `lastEventID` to resume without missing events:

```go
func streamWithReconnect(ctx context.Context, client flagz.Streamer) {
    var lastEventID int64
    backoff := time.Second

    for {
        events, err := client.Stream(ctx, lastEventID)
        if err != nil {
            if ctx.Err() != nil {
                return // context cancelled — clean shutdown
            }
            log.Printf("stream connect failed: %v — retrying in %v", err, backoff)
            time.Sleep(backoff)
            backoff = min(backoff*2, 30*time.Second)
            continue
        }

        backoff = time.Second // reset on successful connect

        for ev := range events {
            lastEventID = ev.EventID
            switch ev.Type {
            case "update":
                log.Printf("flag %s updated (enabled=%v)", ev.Key, ev.Flag.Enabled)
            case "delete":
                log.Printf("flag %s deleted", ev.Key)
            }
        }

        // Channel closed — stream ended. Reconnect unless cancelled.
        if ctx.Err() != nil {
            return
        }
        log.Printf("stream ended — reconnecting from event %d", lastEventID)
    }
}
```

> **Tip:** The `lastEventID` parameter tells the server to replay events after that ID, so you never miss a beat between reconnects.

## Testing & mocking

Because the client is interface-driven, mocking is straightforward — no code generation tools required.

```go
package myapp_test

import (
    "context"
    "testing"

    flagz "github.com/matt-riley/flagz/clients/go"
)

// MockClient implements all three interfaces for testing.
type MockClient struct {
    flags    map[string]flagz.Flag
    evalFunc func(key string, ctx flagz.EvaluationContext) bool
}

func NewMockClient() *MockClient {
    return &MockClient{flags: make(map[string]flagz.Flag)}
}

func (m *MockClient) CreateFlag(_ context.Context, f flagz.Flag) (flagz.Flag, error) {
    m.flags[f.Key] = f
    return f, nil
}

func (m *MockClient) GetFlag(_ context.Context, key string) (flagz.Flag, error) {
    f, ok := m.flags[key]
    if !ok {
        return flagz.Flag{}, fmt.Errorf("not found: %s", key)
    }
    return f, nil
}

func (m *MockClient) ListFlags(_ context.Context) ([]flagz.Flag, error) {
    out := make([]flagz.Flag, 0, len(m.flags))
    for _, f := range m.flags {
        out = append(out, f)
    }
    return out, nil
}

func (m *MockClient) UpdateFlag(_ context.Context, f flagz.Flag) (flagz.Flag, error) {
    m.flags[f.Key] = f
    return f, nil
}

func (m *MockClient) DeleteFlag(_ context.Context, key string) error {
    delete(m.flags, key)
    return nil
}

func (m *MockClient) Evaluate(_ context.Context, key string, evalCtx flagz.EvaluationContext, defaultValue bool) (bool, error) {
    if m.evalFunc != nil {
        return m.evalFunc(key, evalCtx), nil
    }
    f, ok := m.flags[key]
    if !ok {
        return defaultValue, nil
    }
    return f.Enabled, nil
}

func (m *MockClient) EvaluateBatch(_ context.Context, reqs []flagz.EvaluateRequest) ([]flagz.EvaluateResult, error) {
    results := make([]flagz.EvaluateResult, len(reqs))
    for i, r := range reqs {
        val, _ := m.Evaluate(context.Background(), r.Key, r.Context, r.DefaultValue)
        results[i] = flagz.EvaluateResult{Key: r.Key, Value: val}
    }
    return results, nil
}

func (m *MockClient) Stream(_ context.Context, _ int64) (<-chan flagz.FlagEvent, error) {
    ch := make(chan flagz.FlagEvent)
    close(ch) // no events in tests
    return ch, nil
}

// Compile-time interface checks.
var _ flagz.FlagManager = (*MockClient)(nil)
var _ flagz.Evaluator   = (*MockClient)(nil)
var _ flagz.Streamer    = (*MockClient)(nil)

func TestFeatureGate(t *testing.T) {
    mock := NewMockClient()
    mock.CreateFlag(context.Background(), flagz.Flag{Key: "dark-mode", Enabled: true})

    // Your code under test accepts flagz.Evaluator:
    enabled, err := mock.Evaluate(context.Background(), "dark-mode", flagz.EvaluationContext{}, false)
    if err != nil {
        t.Fatal(err)
    }
    if !enabled {
        t.Error("expected dark-mode to be enabled")
    }
}
```

## Configuration reference

Every knob you can turn, in one place.

### HTTP — `flagzhttp.Config`

| Field        | Type           | Required | Default              | Description |
|--------------|----------------|----------|----------------------|-------------|
| `BaseURL`    | `string`       | ✅       | —                    | Base URL of the flagz server, e.g. `"http://localhost:8080"` |
| `APIKey`     | `string`       | ✅       | —                    | Bearer token in `"id.secret"` format |
| `HTTPClient` | `*http.Client` | ❌       | `http.DefaultClient` | Custom HTTP client — use this to configure timeouts, transports, or proxies |

### gRPC — `flagzgrpc.Config`

| Field      | Type                 | Required | Default              | Description |
|------------|----------------------|----------|----------------------|-------------|
| `Address`  | `string`             | ✅       | —                    | Host and port of the gRPC server, e.g. `"localhost:9090"` |
| `APIKey`   | `string`             | ✅       | —                    | Bearer token in `"id.secret"` format |
| `DialOpts` | `[]grpc.DialOption`  | ❌       | Insecure credentials | Additional gRPC dial options (TLS, interceptors, etc.) |

## Context & cancellation

Every method takes a `context.Context` as its first argument. This isn't just a Go convention — it's load-bearing:

```go
// Timeout after 5 seconds
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

flag, err := client.GetFlag(ctx, "my-feature")
// err will be context.DeadlineExceeded if it takes too long
```

```go
// Cancel a long-running stream
ctx, cancel := context.WithCancel(context.Background())
events, _ := client.Stream(ctx, 0)

go func() {
    for ev := range events {
        fmt.Println(ev.Key)
    }
    fmt.Println("stream closed")
}()

// Later: stop the stream
cancel()
```

For the HTTP client, context cancellation aborts in-flight HTTP requests. For gRPC, it cancels the underlying RPC. Both streaming implementations close the event channel when the context is done.

## Thread safety

Both `http.Client` and `grpc.Client` are safe for concurrent use from multiple goroutines. Create one client at startup and share it — no mutexes, no per-request clients, no drama.

```go
// ✅ Do this
var client = flagzhttp.NewHTTPClient(cfg)

func handlerA(w http.ResponseWriter, r *http.Request) {
    enabled, _ := client.Evaluate(r.Context(), "feature-a", flagz.EvaluationContext{}, false)
    // ...
}

func handlerB(w http.ResponseWriter, r *http.Request) {
    enabled, _ := client.Evaluate(r.Context(), "feature-b", flagz.EvaluationContext{}, false)
    // ...
}
```

## Requirements

- A supported version of Go (see `go.mod` for the minimum version)
