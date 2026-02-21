# flagz Go client

Go client library for the [flagz](../../README.md) feature flag service. Supports HTTP and gRPC transports.

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

## Requirements

- A supported version of Go (see `go.mod` for the minimum version)
