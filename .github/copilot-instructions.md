# Copilot instructions for `flagz`

## Build, test, and lint commands
- Full test suite (used in CI): `go test ./...`
- Race-enabled tests (used in CI): `go test -race ./...`
- Static analysis used in CI: `go vet ./...`
- Build server binary (matches Docker build flags): `go build -trimpath -ldflags="-s -w" -o ./bin/server ./cmd/server`
- Run tests for one package: `go test ./internal/service`
- Run a single test: `go test ./internal/service -run '^TestServiceCRUDAndEvaluation$'`

## High-level architecture
- `cmd/server/main.go` wires the app: load env config, open PostgreSQL connection (`pgxpool`), create repository/service, then serve both HTTP (`:8080`) and gRPC (`:9090`) in one process.
- `internal/repository/postgres.go` is the persistence boundary for `flags`, `api_keys`, and `flag_events`; it also emits PostgreSQL `NOTIFY` messages (`flag_events` channel) after writing flag events.
- `internal/service/service.go` owns feature-flag business logic and an in-memory cache of flags; it eagerly loads cache on startup and refreshes on invalidations plus periodic resync.
- Transport layers are `internal/server/http.go` and `internal/server/grpc.go`, both delegating to the same `server.Service` interface (`internal/server/service.go`), so behavior changes should stay consistent across both APIs.
- Streaming updates are event-table based: HTTP SSE (`GET /v1/stream`) and gRPC `WatchFlag` both poll `ListEventsSince` using `STREAM_POLL_INTERVAL` (default 1s).

## Key repository conventions
- Auth is bearer token based for all `/v1/*` HTTP endpoints and all gRPC methods; token format is `<api_key_id>.<raw_secret>`, and secrets are compared via SHA-256 + constant-time compare (`internal/middleware/api_key.go`, `cmd/server/main.go`).
- `/healthz` and `/metrics` are intentionally outside the `/v1/*` auth gate; keep this split when adding routes (`cmd/server/main.go` + `internal/server/http.go`).
- JSON request decoding in HTTP handlers uses `DisallowUnknownFields` and enforces a single JSON object (`decodeJSONBody`), so new request fields must be added explicitly.
- Error mapping pattern: repository DB misses (`pgx.ErrNoRows`) are translated to `service.ErrFlagNotFound`, then mapped to transport-specific not-found responses.
- Flag mutation writes and event publishing are decoupled: event publish is best-effort and should not fail the completed mutation path (`publishFlagEventBestEffort` behavior is covered by service tests).
- SQL schema changes are tracked as goose migrations under `migrations/` with `-- +goose Up/Down` headers.
