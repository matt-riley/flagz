---
name: proto-grpc
description: Regenerate Go code from the flagz protobuf definition, and manually test gRPC endpoints. Use when changing api/proto/v1/flag_service.proto, verifying the generated .pb.go files are up to date, or calling gRPC methods during development.
---

# Proto & gRPC — flagz

## Proto layout

```
api/proto/v1/
├── flag_service.proto          ← source of truth
├── flag_service.pb.go          ← generated (messages)
└── flag_service_grpc.pb.go     ← generated (service + client stubs)
```

Go package: `github.com/mattriley/flagz/api/proto/v1` (alias `flagspb`)

## Regenerating Go code

### Prerequisites

```bash
# Install protoc (Debian/Ubuntu)
apt install -y protobuf-compiler

# Install Go plugins (run once; re-run to upgrade)
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Ensure $GOPATH/bin is on PATH
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Generate

Run from the repo root:

```bash
protoc \
  --go_out=. --go_opt=paths=source_relative \
  --go-grpc_out=. --go-grpc_opt=paths=source_relative \
  api/proto/v1/flag_service.proto
```

This overwrites `flag_service.pb.go` and `flag_service_grpc.pb.go` in place. Commit both generated files.

After regenerating, run `go build ./...` to confirm nothing is broken.

## Manually testing gRPC endpoints

Install grpcurl:

```bash
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
```

The server must be running (default gRPC port `:9090`). All methods require a bearer token header (`-H "Authorization: Bearer <id>.<secret>"`).

```bash
# List services
grpcurl -plaintext localhost:9090 list

# Describe the service
grpcurl -plaintext localhost:9090 describe flagz.v1.FlagService

# Create a flag
grpcurl -plaintext \
  -H "Authorization: Bearer <id>.<secret>" \
  -d '{"flag": {"key": "my-flag", "enabled": true}}' \
  localhost:9090 flagz.v1.FlagService/CreateFlag

# Resolve a boolean flag
grpcurl -plaintext \
  -H "Authorization: Bearer <id>.<secret>" \
  -d '{"key": "my-flag", "default_value": false}' \
  localhost:9090 flagz.v1.FlagService/ResolveBoolean

# Watch a flag (streaming; Ctrl-C to stop)
grpcurl -plaintext \
  -H "Authorization: Bearer <id>.<secret>" \
  -d '{"key": "my-flag"}' \
  localhost:9090 flagz.v1.FlagService/WatchFlag
```

`context_json` in `ResolveBoolean`/`ResolveBatch` is a JSON-encoded `EvaluationContext` passed as bytes, e.g. `"context_json": "{\"attributes\":{\"env\":\"prod\"}}"`.
