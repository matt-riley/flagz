# Contributing to flagz 🚩

Hey — thanks for considering a contribution! Whether it's a bug fix, a new feature, a docs tweak, or just a well-written issue, you're making flagz better and we appreciate it.

---

## Table of Contents

- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Code Style](#code-style)
- [Project Structure](#project-structure)
- [Making Changes](#making-changes)
- [Commit Messages](#commit-messages)
- [Client Libraries](#client-libraries)
- [Database Migrations](#database-migrations)
- [Reporting Issues](#reporting-issues)

---

## Getting Started

### Prerequisites

| Tool       | Why                                       |
| ---------- | ----------------------------------------- |
| **Go 1.25+** | The server and Go client are written in Go |
| **PostgreSQL** | Where the flags live                    |
| **[goose](https://github.com/pressly/goose)** | Database migration runner |
| **Docker & Docker Compose** | Easiest way to spin up the full stack |

### Clone the repo

```bash
git clone https://github.com/matt-riley/flagz
cd flagz
```

### Local dev environment (Docker Compose)

The quickest path to a working setup:

```bash
cp docker-compose.example.yml docker-compose.yml
docker compose up --build -d
```

This starts PostgreSQL 18 and the flagz server. You'll still need to run migrations (see below).

### Run migrations

From the host, with Postgres running:

```bash
DATABASE_URL="postgresql://flagz:flagz@localhost:5432/flagz?sslmode=disable" \
  goose -dir migrations postgres "$DATABASE_URL" up
```

---

## Development Workflow

### Build the binary

```bash
go build -trimpath -ldflags="-s -w" -o ./bin/server ./cmd/server
```

### Run tests

```bash
# Full test suite
go test ./...

# With the race detector — CI runs this too, so you should as well
go test -race ./...
```

### Static analysis

```bash
go vet ./...
```

### Build the Docker image

```bash
docker build -t flagz:local .
```

The server listens on `:8080` (HTTP) and `:9090` (gRPC) by default.

> 💡 **Tip:** CI runs `go test ./...`, `go test -race ./...`, and `go vet ./...` on every push and PR. Save yourself a round-trip and run them locally first.

---

## Code Style

- **Format with `gofmt`** (or `goimports`). If your editor doesn't do this on save, now is a great time to fix that.
- Follow standard Go conventions — [Effective Go](https://go.dev/doc/effective_go) is the canonical reference.
- Use meaningful names. `f` is fine inside a three-line loop; `f` is not fine as a package-level variable representing a feature flag.
- Keep functions focused. If a function needs a paragraph-long comment to explain what it does, it probably wants to be two functions.
- Comment the _why_, not the _what_. The code already says what it does.

---

## Project Structure

A quick orientation:

```
cmd/server/          → Entry point: wires config, DB, service, and transport layers
internal/
  config/            → Environment variable loading
  core/              → Pure evaluation logic — no DB, no transport, no side effects
  middleware/        → HTTP middleware (auth, metrics, request logging)
  repository/        → PostgreSQL persistence (flags, api_keys, flag_events)
  server/            → HTTP and gRPC transport handlers
  service/           → Business logic, in-memory flag cache, event publishing
api/proto/v1/        → Protobuf definitions for the gRPC API
clients/
  go/                → Go client library
  typescript/        → TypeScript client library
migrations/          → goose SQL migrations (PostgreSQL)
```

Key things to know:

- **`repository.Flag` uses `Enabled bool`; `core.Flag` uses `Disabled bool`.** The conversion inverts this. Keep it consistent.
- **HTTP and gRPC share the same `server.Service` interface.** Behavior changes should stay consistent across both transports.
- **`ListFlags` reads from the in-memory cache, never the database.** This is intentional.

---

## Making Changes

### The workflow

1. **Fork** the repository
2. **Create a branch** from `main` — name it something descriptive (`fix/stream-reconnect`, `feat/percentage-rollout`, etc.)
3. **Make your changes** — keep them focused
4. **Write tests** for new functionality
5. **Run the checks** locally (`go test ./...`, `go test -race ./...`, `go vet ./...`)
6. **Commit** with a clear message (see [Commit Messages](#commit-messages))
7. **Open a Pull Request** against `main`

### PR guidelines

- **Keep PRs small and focused.** One logical change per PR. If you find yourself writing "also" in the PR description, consider splitting it up.
- **Update documentation** if your change affects user-facing behavior (README, API docs, etc.).
- **Don't break the public API** without discussion. Open an issue first if you think a breaking change is warranted.

---

## Commit Messages

We encourage [Conventional Commits](https://www.conventionalcommits.org/) style:

```
feat: add percentage-based rollout rules
fix: handle nil context in batch evaluation
docs: clarify STREAM_POLL_INTERVAL format
chore: bump pgx to v5.9.0
test: add coverage for SSE reconnection
```

The format is `type: short description` — lowercase, imperative mood, no trailing period. The body is optional but welcome for anything non-trivial.

---

## Client Libraries

flagz ships with client libraries under `clients/`:

- **Go** (`clients/go/`) — tested with `go test ./...` and `go vet ./...`
- **TypeScript** (`clients/typescript/`) — tested with `npm test`, built with `npm run build`

Each client has its own CI job and its own test suite. If you're changing the core API or protobuf definitions, make sure the clients still build and pass. CI checks that the TypeScript client's proto file stays in sync with the canonical `api/proto/v1/flag_service.proto`.

---

## Database Migrations

Migrations use [goose](https://github.com/pressly/goose) and live in the `migrations/` directory.

### Adding a new migration

```bash
goose -dir migrations create your_migration_name sql
```

This creates a pair of timestamped `.sql` files. Fill in the `-- +goose Up` and `-- +goose Down` sections:

```sql
-- +goose Up
ALTER TABLE flags ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE flags DROP COLUMN IF EXISTS metadata;
```

**Guidelines:**

- Always write a reversible `Down` migration.
- Keep migrations small and atomic — one logical change per file.
- Test both `up` and `down` against a local database before pushing.

---

## Reporting Issues

### 🐛 Bug reports

A good bug report includes:

- What you expected to happen
- What actually happened
- Steps to reproduce (the more specific, the better)
- flagz version or commit SHA
- Relevant logs or error messages

### 💡 Feature requests

We're open to ideas! Please describe:

- The problem you're trying to solve
- How you imagine the solution working
- Any alternatives you've considered

Open an issue on [GitHub Issues](https://github.com/matt-riley/flagz/issues) and we'll take a look.

---

Thanks for reading this far. Seriously. Most people stop at the first code block. You're already our favourite contributor. 🎉
