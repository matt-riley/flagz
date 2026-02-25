# flagz 🚩

[![CI](https://github.com/matt-riley/flagz/actions/workflows/ci.yml/badge.svg)](https://github.com/matt-riley/flagz/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Container-distroless-2496ED?logo=docker&logoColor=white)](https://github.com/matt-riley/flagz/pkgs/container/flagz)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> _Feature flags for your code's wildest adventures._

**flagz** is a self-hosted feature flag service. It stores flags in PostgreSQL, evaluates them at the speed of an in-memory cache, and shouts about changes over HTTP Server-Sent Events and gRPC streams. It speaks both REST and gRPC, fits in a distroless container, and won't phone home.

## Why flagz?

- **Self-hosted** — your data stays on your infrastructure. No SaaS pricing surprises at 3am.
- **Fast** — flag evaluation hits an in-memory cache, not the database. Your hot path stays hot.
- **Dual protocol** — REST for simplicity, gRPC for speed. Same service, same auth, same flags.
- **Real-time** — SSE and gRPC streaming push flag changes to your app as they happen.
- **Tiny footprint** — a single statically-linked binary in a distroless container. Your Kubernetes cluster will barely notice.
- **Client libraries** — official [Go](clients/go/) and [TypeScript](clients/typescript/) clients with matching interfaces.

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
- [Client libraries](#client-libraries)
- [Migrations](#migrations)
- [Observability](#observability)
- [Development](#development)
- [Architecture](#architecture)
- [Contributing](#contributing)
- [Security](#security)

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
2. Every write (create / update / delete) immediately updates the cache _and_ appends a row to `flag_events`, then fires a best-effort PostgreSQL `NOTIFY` on the `flag_events` channel.
3. The cache listener wakes on `NOTIFY` (and re-syncs periodically — every `CACHE_RESYNC_INTERVAL`, default 1 minute — as a safety net) to stay current.
4. `ListFlags` and all evaluations read exclusively from the cache — the database is never touched during hot-path reads.

---

## Quickstart

**Prerequisites:** Docker, a running PostgreSQL instance, and the goose migration tool.

```bash
# 1. Clone and build
git clone https://github.com/matt-riley/flagz
cd flagz

# 2. Start a local Postgres + flagz stack (see docker-compose.example.yml)
cp docker-compose.example.yml docker-compose.yml
docker compose up --build -d

# 3. Run migrations (from the host, against the local Postgres)
DATABASE_URL="postgresql://flagz:flagz@localhost:5432/flagz?sslmode=disable" \
  goose -dir migrations postgres "$DATABASE_URL" up

# 4. Create an API key (see "Creating an API key" below)

# 5. Kick the tyres
curl -s -H "Authorization: Bearer myapp.my-super-secret" http://localhost:8080/v1/flags
```

> The `docker-compose.example.yml` file is a starting point. Copy it, customise it, don't commit your secrets.

### Creating an API key

Your **first** API key must be bootstrapped directly into PostgreSQL (or through the Admin Portal, if you have it configured). Once you have one valid key, you can create, list, and delete additional keys via the REST API — see [API Keys](#api-keys) below.

For the initial key, a bearer token has the format `<id>.<secret>` — you choose both, hash the secret with bcrypt, and insert the hash.

**Option A: One-liner with `htpasswd`** (if you have Apache utils installed)

```bash
# Pick an id and secret
API_KEY_ID="myapp"
API_KEY_SECRET="my-super-secret"

# Generate a bcrypt hash
HASH=$(htpasswd -nbBC 10 "" "$API_KEY_SECRET" | cut -d: -f2)

# Insert into the database
psql "$DATABASE_URL" -c \
  "INSERT INTO api_keys (id, name, key_hash) VALUES ('$API_KEY_ID', 'My App', '$HASH');"

# Your bearer token is: myapp.my-super-secret
```

**Option B: Using Go** (works anywhere Go is installed)

```bash
# Generate a bcrypt hash using the same function flagz uses internally
HASH=$(go run -C /path/to/flagz -mod=mod -e '
  import "golang.org/x/crypto/bcrypt"
  import "fmt"
  import "os"
  h, _ := bcrypt.GenerateFromPassword([]byte(os.Args[1]), bcrypt.DefaultCost)
  fmt.Print(string(h))
' "my-super-secret" 2>/dev/null)

# Or simply use a Go one-liner
HASH=$(go run golang.org/x/crypto/bcrypt@latest hash "my-super-secret")
```

**Option C: Using Docker** (no local tools needed)

```bash
# Spin up a quick container to generate the hash
HASH=$(docker run --rm -it python:3-slim \
  python -c "import bcrypt; print(bcrypt.hashpw(b'my-super-secret', bcrypt.gensalt()).decode())")

# Insert it
docker exec -it $(docker compose ps -q postgres) \
  psql -U flagz -d flagz -c \
  "INSERT INTO api_keys (id, name, key_hash) VALUES ('myapp', 'My App', '$HASH');"
```

Your bearer token is then `myapp.my-super-secret`. Use it as:

```
Authorization: Bearer myapp.my-super-secret
```

> **Tip:** The `id` can be anything you like — use it to identify which application or team owns the key. The `secret` should be long and random in production. What you see above is for kicking tyres only.

---

## Configuration

All configuration is via environment variables.

| Variable               | Required | Default       | Description                                                              |
| ---------------------- | -------- | ------------- | ------------------------------------------------------------------------ |
| `DATABASE_URL`         | ✅       | —             | PostgreSQL connection string (pgx format)                                |
| `HTTP_ADDR`            |          | `:8080`       | Address for the HTTP server                                              |
| `GRPC_ADDR`            |          | `:9090`       | Address for the gRPC server                                              |
| `STREAM_POLL_INTERVAL` |          | `1s`          | How often streams poll for new events (must be > 0)                      |
| `CACHE_RESYNC_INTERVAL`|          | `1m`          | Periodic safety-net cache resync interval (must be > 0)                  |
| `MAX_JSON_BODY_SIZE`   |          | `1048576`     | Maximum HTTP request body size in bytes (must be > 0)                    |
| `EVENT_BATCH_SIZE`     |          | `1000`        | Maximum events returned per stream poll query (must be > 0)              |
| `AUTH_RATE_LIMIT`      |          | `10`          | Max failed authentication attempts per minute per IP before rate-limiting (must be > 0) |
| `LOG_LEVEL`            |          | `info`        | Log verbosity (`debug`, `info`, `warn`, `error`)                         |
| `ADMIN_HOSTNAME`       |          | —             | Hostname for the Admin Portal on Tailscale                               |
| `TS_AUTH_KEY`          |          | —             | Tailscale Auth Key (required if `ADMIN_HOSTNAME` set)                    |
| `TS_STATE_DIR`         |          | `tsnet-state` | Directory to store Tailscale state                                       |
| `SESSION_SECRET`       |          | —             | Secret for signing admin sessions (32+ chars, required if `ADMIN_HOSTNAME` set) |

`STREAM_POLL_INTERVAL` accepts any Go duration string: `500ms`, `2s`, `1m`, etc.

---

## Admin Portal

flagz includes a built-in Admin Portal for managing projects and flags. It is designed to be accessible **only** via a Tailscale network (tailnet) for security.

To enable the Admin Portal:

1.  Set `ADMIN_HOSTNAME` to the desired hostname (e.g., `flagz-admin`).
2.  Set `TS_AUTH_KEY` to a valid Tailscale Auth Key (reusable, ephemeral recommended).
3.  Set `SESSION_SECRET` to a random string (at least 32 bytes) for secure session cookies.

The portal will start and register itself on your tailnet. Access it at `http://<ADMIN_HOSTNAME>` from any device on your tailnet.

### First-run setup

The first time you access the portal, you will be redirected to a setup page to create the initial admin user. Subsequent accesses will require login.

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
  "key": "dark-mode",
  "description": "Enable the dark side of the UI",
  "enabled": true,
  "variants": { "default": false },
  "rules": [
    { "attribute": "user_id", "operator": "in", "value": [42, 99, 1337] },
    { "attribute": "plan", "operator": "equals", "value": "enterprise" }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

| Field         | Type        | Notes                                                              |
| ------------- | ----------- | ------------------------------------------------------------------ |
| `key`         | string      | Unique identifier. Required. Immutable after creation.             |
| `description` | string      | Human-readable label. Optional.                                    |
| `enabled`     | bool        | Master switch. `false` → always evaluates to `false`.              |
| `variants`    | JSON object | Optional. `{ "default": bool }` sets the fallback value.           |
| `rules`       | JSON array  | Optional. List of targeting rules (see [Evaluation](#evaluation)). |
| `created_at`  | RFC3339     | Set by the database.                                               |
| `updated_at`  | RFC3339     | Updated by the database on every write.                            |

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

| Operator | Matches when…                                                                     |
| -------- | --------------------------------------------------------------------------------- |
| `equals` | The attribute value equals the rule value (type-coercion-safe numeric comparison) |
| `in`     | The attribute value is present in the rule's value array                          |

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

| Method   | Path              | Description                 |
| -------- | ----------------- | --------------------------- |
| `POST`   | `/v1/flags`       | Create a flag               |
| `GET`    | `/v1/flags`       | List all flags (from cache) |
| `GET`    | `/v1/flags/{key}` | Get a single flag           |
| `PUT`    | `/v1/flags/{key}` | Replace a flag              |
| `DELETE` | `/v1/flags/{key}` | Delete a flag               |

### API Keys

| Method   | Path                  | Description                             |
| -------- | --------------------- | --------------------------------------- |
| `POST`   | `/v1/api-keys`        | Create an API key (returns id + secret) |
| `GET`    | `/v1/api-keys`        | List API key metadata for this project  |
| `DELETE` | `/v1/api-keys/{id}`   | Revoke an API key                       |

The `POST /v1/api-keys` response is:

```json
{ "id": "a3f9b2c4d8e1f067b82a5c3d9e0f1234", "secret": "a3f9b2c4d8e1f067b82a5c3d9e0f1234.c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8" }
```

Both `id` and `secret` are server-generated random hex strings — there is no request body.

The `secret` value is the full bearer token. Store it somewhere safe — it is shown **once** and cannot be retrieved again.

### Audit log

| Method | Path             | Description                                          |
| ------ | ---------------- | ---------------------------------------------------- |
| `GET`  | `/v1/audit-log`  | List audit log entries for this project (newest first) |

Supports `limit` (default 50, max 1000) and `offset` query parameters.

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
    { "key": "dark-mode", "value": true },
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

| Method           | Request                 | Response                   |
| ---------------- | ----------------------- | -------------------------- |
| `CreateFlag`     | `CreateFlagRequest`     | `CreateFlagResponse`       |
| `UpdateFlag`     | `UpdateFlagRequest`     | `UpdateFlagResponse`       |
| `GetFlag`        | `GetFlagRequest`        | `GetFlagResponse`          |
| `ListFlags`      | `ListFlagsRequest`      | `ListFlagsResponse`        |
| `DeleteFlag`     | `DeleteFlagRequest`     | `DeleteFlagResponse`       |
| `ResolveBoolean` | `ResolveBooleanRequest` | `ResolveBooleanResponse`   |
| `ResolveBatch`   | `ResolveBatchRequest`   | `ResolveBatchResponse`     |
| `WatchFlag`      | `WatchFlagRequest`      | stream of `WatchFlagEvent` |

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

To filter events to a single flag, add a `key` query parameter:

```bash
curl -N -H "Authorization: Bearer <id>.<secret>" \
     "http://localhost:8080/v1/stream?key=dark-mode"
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

| Table         | Purpose                                                       |
| ------------- | ------------------------------------------------------------- |
| `flags`       | Flag definitions (key, description, enabled, variants, rules) |
| `api_keys`    | Authentication credentials (id, name, bcrypt key_hash)        |
| `flag_events` | Append-only event log for streaming and cache invalidation    |

---

## Observability

| Endpoint       | Auth required | Description                                     |
| -------------- | ------------- | ----------------------------------------------- |
| `GET /healthz` | No            | Returns `{"status":"ok"}` when the server is up |
| `GET /metrics` | No            | Prometheus-compatible text exposition           |

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

---

## Client libraries

Official clients are available for Go and TypeScript, both implementing the same interface pattern: **FlagManager** (CRUD), **Evaluator** (flag resolution), and **Streamer** (real-time events). Swap transports without changing your application code.

| Language   | Transport    | Package                                              |
| ---------- | ------------ | ---------------------------------------------------- |
| Go         | HTTP + gRPC  | [`github.com/matt-riley/flagz/clients/go`](clients/go/)       |
| TypeScript | HTTP + gRPC  | [`@matt-riley/flagz`](clients/typescript/)           |

The TypeScript HTTP client has **zero runtime dependencies** — just `fetch` and dreams.

See each client's README for quickstarts, examples, and integration guides.

---

## Architecture

For a deep dive into how flagz is built — caching strategy, event system, data flow, and design decisions — see the **[Architecture Guide](docs/ARCHITECTURE.md)**.

---

## Contributing

Contributions are welcome! See **[CONTRIBUTING.md](CONTRIBUTING.md)** for development setup, coding guidelines, and the PR process.

---

## Security

For reporting vulnerabilities and security considerations, see **[SECURITY.md](SECURITY.md)**.
