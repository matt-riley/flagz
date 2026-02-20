# flagz 🚩

> *Feature flags for your code's wildest adventures.*

**flagz** is a self-hosted feature flag service. It stores flags in PostgreSQL, evaluates them at the speed of an in-memory cache, and shouts about changes over HTTP Server-Sent Events and gRPC streams. It speaks both REST and gRPC, fits in a distroless container, and won't phone home.

---

## Contents

- [How it works](#how-it-works)
- [Quickstart](#quickstart)
- [Configuration](#configuration)
- [Authentication](#authentication)
- [Flag model](#flag-model)
- [Evaluation](#evaluation)
- [HTTP API](#http-api)
- [gRPC API](#grpc-api)
- [Streaming changes](#streaming-changes)
- [Migrations](#migrations)
- [Observability](#observability)
- [Development](#development)

---

## How it works

```
  your app
     │
     ▼ bearer token
┌────────────┐   REST / gRPC   ┌──────────────────────────────────────┐
│  flagz     │◄────────────────┤  flags · rules · events              │
│  :8080     │                 │                                      │
│  :9090     │  in-memory      │  ┌──────────┐                        │
└────────────┘  cache          │  │ postgres │  NOTIFY flag_events    │
     │          warm on start  │  └──────────┘  ──────────────────►  │
     │          refresh on     │                  cache invalidation  │
     │          NOTIFY + 1min  └──────────────────────────────────────┘
     │
     ▼
POST /v1/evaluate  →  { "key": "dark-mode", "value": true }
GET  /v1/stream    →  SSE: flag updated / deleted events
```

1. On startup the service loads all flags into an in-memory cache.
2. Every write (create / update / delete) immediately updates the cache *and* appends a row to `flag_events`, then fires a best-effort PostgreSQL `NOTIFY` on the `flag_events` channel.
3. The cache listener wakes on `NOTIFY` (and re-syncs every minute regardless, as a safety net) to stay current.
4. `ListFlags` and all evaluations read exclusively from the cache — the database is never touched during hot-path reads.

---

## Quickstart

**Prerequisites:** Docker, a running PostgreSQL instance, and the goose migration tool.

```bash
# 1. Clone and build
git clone https://github.com/mattriley/flagz
cd flagz

# 2. Start a local Postgres + flagz stack (see docker-compose.example.yml)
cp docker-compose.example.yml docker-compose.yml
docker compose up --build -d

# 3. Run migrations (from the host, against the local Postgres)
DATABASE_URL="postgresql://flagz:flagz@localhost:5432/flagz?sslmode=disable" \
  goose -dir migrations postgres "$DATABASE_URL" up

# 4. Create an API key (directly in the database — flagz doesn't expose a key-management endpoint yet)
#    Insert a bcrypt hash of your chosen secret alongside an id of your choice.

# 5. Kick the tyres
curl -s -H "Authorization: Bearer <id>.<secret>" http://localhost:8080/v1/flags
```

> The `docker-compose.example.yml` file is a starting point. Copy it, customise it, don't commit your secrets.

---

## Configuration

All configuration is via environment variables.

| Variable               | Required | Default | Description                                         |
|------------------------|----------|---------|-----------------------------------------------------|
| `DATABASE_URL`         | ✅        | —       | PostgreSQL connection string (pgx format)           |
| `HTTP_ADDR`            |          | `:8080` | Address for the HTTP server                         |
| `GRPC_ADDR`            |          | `:9090` | Address for the gRPC server                         |
| `STREAM_POLL_INTERVAL` |          | `1s`    | How often streams poll for new events (must be > 0) |

`STREAM_POLL_INTERVAL` accepts any Go duration string: `500ms`, `2s`, `1m`, etc.

---

## Authentication

Every `/v1/*` HTTP endpoint and every gRPC method requires a **bearer token** of the form:

```
Authorization: Bearer <api_key_id>.<raw_secret>
```

- `api_key_id` — the `id` column in the `api_keys` table.
- `raw_secret` — the plaintext secret whose bcrypt hash is stored in `key_hash`.

Legacy SHA-256 hashes in `key_hash` are still accepted for backwards compatibility.

`GET /healthz` and `GET /metrics` are intentionally unprotected — keep firewalls in mind if that's a concern.

---

## Flag model

```json
{
  "key":         "dark-mode",
  "description": "Enable the dark side of the UI",
  "enabled":     true,
  "variants":    { "default": false },
  "rules": [
    { "attribute": "user_id", "operator": "in",     "value": [42, 99, 1337] },
    { "attribute": "plan",    "operator": "equals", "value": "enterprise"   }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

| Field         | Type           | Notes                                                           |
|---------------|----------------|-----------------------------------------------------------------|
| `key`         | string         | Unique identifier. Required. Immutable after creation.          |
| `description` | string         | Human-readable label. Optional.                                 |
| `enabled`     | bool           | Master switch. `false` → always evaluates to `false`.           |
| `variants`    | JSON object    | Optional. `{ "default": bool }` sets the fallback value.        |
| `rules`       | JSON array     | Optional. List of targeting rules (see [Evaluation](#evaluation)). |
| `created_at`  | RFC3339        | Set by the database.                                            |
| `updated_at`  | RFC3339        | Updated by the database on every write.                         |

---

## Evaluation

The evaluation engine is pure Go with no external dependencies (`internal/core`).

```
flag disabled?  →  false
    ↓ no
any rule matches?  →  true
    ↓ no
variants.default exists?  →  use it
    ↓ no
→  true  (built-in default)
```

Rules are evaluated in order; the first match short-circuits to `true`. All rules are OR'd — there is no AND nesting.

### Operators

| Operator  | Matches when…                                                              |
|-----------|----------------------------------------------------------------------------|
| `equals`  | The attribute value equals the rule value (type-coercion-safe numeric comparison) |
| `in`      | The attribute value is present in the rule's value array                   |

Attributes and rule values can be strings, booleans, or numbers. Numeric comparisons handle cross-type equality correctly (e.g. `int64(42) == float64(42.0)`).

### Evaluation context

Pass arbitrary key/value attributes with each evaluation request. They are matched against flag rules but never persisted.

```json
{ "attributes": { "user_id": 42, "plan": "pro", "beta": true } }
```

---

## HTTP API

Base path: `http://<host>:8080`

All request and response bodies are JSON. Unknown fields in request bodies are rejected with `400`.

### Flags

| Method   | Path                  | Description                   |
|----------|-----------------------|-------------------------------|
| `POST`   | `/v1/flags`           | Create a flag                 |
| `GET`    | `/v1/flags`           | List all flags (from cache)   |
| `GET`    | `/v1/flags/{key}`     | Get a single flag             |
| `PUT`    | `/v1/flags/{key}`     | Replace a flag                |
| `DELETE` | `/v1/flags/{key}`     | Delete a flag                 |

**Create a flag**

```bash
curl -X POST http://localhost:8080/v1/flags \
  -H "Authorization: Bearer <id>.<secret>" \
  -H "Content-Type: application/json" \
  -d '{
    "key": "dark-mode",
    "description": "Darkness beckons",
    "enabled": true,
    "rules": [
      { "attribute": "beta_user", "operator": "equals", "value": true }
    ]
  }'
```

**Update a flag**

```bash
curl -X PUT http://localhost:8080/v1/flags/dark-mode \
  -H "Authorization: Bearer <id>.<secret>" \
  -H "Content-Type: application/json" \
  -d '{ "enabled": false }'
```

If a `key` field is included in the body it must match the path key.

### Evaluate

`POST /v1/evaluate` — resolve one or more flags for a given context.

**Single flag:**

```bash
curl -X POST http://localhost:8080/v1/evaluate \
  -H "Authorization: Bearer <id>.<secret>" \
  -H "Content-Type: application/json" \
  -d '{
    "key": "dark-mode",
    "context": { "attributes": { "user_id": 42 } },
    "default_value": false
  }'
```

```json
{ "results": [{ "key": "dark-mode", "value": true }] }
```

**Batch (multiple flags in one round-trip):**

```bash
curl -X POST http://localhost:8080/v1/evaluate \
  -H "Authorization: Bearer <id>.<secret>" \
  -H "Content-Type: application/json" \
  -d '{
    "requests": [
      { "key": "dark-mode",     "context": { "attributes": { "user_id": 42 } } },
      { "key": "new-checkout",  "context": { "attributes": { "plan": "pro" } }, "default_value": true }
    ]
  }'
```

```json
{
  "results": [
    { "key": "dark-mode",    "value": true  },
    { "key": "new-checkout", "value": false }
  ]
}
```

`key` and `requests` are mutually exclusive. Providing both returns `400`.

If a flag key does not exist the request still succeeds — `default_value` is returned for that key.

---

## gRPC API

The proto definition lives in `api/proto/v1/`. The service listens on `:9090`.

All gRPC methods require the same bearer token as HTTP, passed as metadata:

```
authorization: Bearer <id>.<secret>
```

### Methods

| Method           | Request                   | Response                    |
|------------------|---------------------------|-----------------------------|
| `CreateFlag`     | `CreateFlagRequest`       | `CreateFlagResponse`        |
| `UpdateFlag`     | `UpdateFlagRequest`       | `UpdateFlagResponse`        |
| `GetFlag`        | `GetFlagRequest`          | `GetFlagResponse`           |
| `ListFlags`      | `ListFlagsRequest`        | `ListFlagsResponse`         |
| `DeleteFlag`     | `DeleteFlagRequest`       | `DeleteFlagResponse`        |
| `ResolveBoolean` | `ResolveBooleanRequest`   | `ResolveBooleanResponse`    |
| `ResolveBatch`   | `ResolveBatchRequest`     | `ResolveBatchResponse`      |
| `WatchFlag`      | `WatchFlagRequest`        | stream of `WatchFlagEvent`  |

`ListFlags` supports cursor-based pagination via `page_size` and `page_token` fields.

Evaluation context is passed as a JSON-encoded `context_json` bytes field.

---

## Streaming changes

flagz publishes real-time flag change events over both transports. Both use polling under the hood (configurable via `STREAM_POLL_INTERVAL`).

### HTTP — Server-Sent Events

```bash
curl -N -H "Authorization: Bearer <id>.<secret>" \
     http://localhost:8080/v1/stream
```

To resume from a known position, pass the last received event ID:

```
Last-Event-ID: 42
```

Events:

```
id: 43
event: update
data: {"key":"dark-mode","enabled":false,...}

id: 44
event: delete
data: {"key":"old-flag",...}
```

An `event: error` frame is emitted if the server encounters a problem mid-stream.

### gRPC — `WatchFlag`

`WatchFlag` is a server-side streaming RPC. Set `last_event_id` to resume. Optionally set `key` to filter events to a single flag.

---

## Migrations

Migrations are managed with [goose](https://github.com/pressly/goose) and live in `migrations/`.

```bash
# Apply all pending migrations
goose -dir migrations postgres "$DATABASE_URL" up

# Roll back the last migration
goose -dir migrations postgres "$DATABASE_URL" down
```

### Schema overview

| Table          | Purpose                                                         |
|----------------|-----------------------------------------------------------------|
| `flags`        | Flag definitions (key, description, enabled, variants, rules)  |
| `api_keys`     | Authentication credentials (id, name, bcrypt key_hash)         |
| `flag_events`  | Append-only event log for streaming and cache invalidation      |

---

## Observability

| Endpoint      | Auth required | Description                                  |
|---------------|---------------|----------------------------------------------|
| `GET /healthz` | No           | Returns `{"status":"ok"}` when the server is up |
| `GET /metrics` | No           | Prometheus-compatible text exposition        |

Current metrics:

```
# HELP flagz_http_requests_total Total number of HTTP requests.
# TYPE flagz_http_requests_total counter
flagz_http_requests_total 42
```

---

## Development

```bash
# Run tests
go test ./...

# Run tests with race detector
go test -race ./...

# Static analysis
go vet ./...

# Build the server binary
go build -trimpath -ldflags="-s -w" -o ./bin/server ./cmd/server

# Build the Docker image
docker build -t flagz:local .
```

The server runs on `:8080` (HTTP) and `:9090` (gRPC) by default.
