---
name: local-dev
description: Run flagz locally for development — start PostgreSQL via Docker Compose, apply goose migrations, run the server, and create a test API key. Use when setting up a local environment, resetting state, or reproducing issues against a real database.
---

# Local development — flagz

## Default credentials (compose stack)

| Variable | Value |
|---|---|
| `DATABASE_URL` | `postgresql://flagz:flagz@localhost:5432/flagz?sslmode=disable` |
| HTTP | `localhost:8080` |
| gRPC | `localhost:9090` |

## Start PostgreSQL

`docker-compose.example.yml` is a template — copy it first if you want to customise it, or use it directly with `-f`:

```bash
# Start only Postgres (no app container — run the app from source instead)
docker compose -f docker-compose.example.yml up -d postgres
```

Wait for the healthcheck: `docker compose -f docker-compose.example.yml ps` should show `healthy`.

## Apply migrations

Install goose once:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
```

Then apply all pending migrations:

```bash
goose -dir ./migrations postgres \
  "postgresql://flagz:flagz@localhost:5432/flagz?sslmode=disable" up
```

Other useful goose commands:

```bash
goose ... status   # see applied vs pending
goose ... down     # roll back the last migration
goose ... reset    # roll back all migrations
```

## Run the server (from source)

```bash
DATABASE_URL=postgresql://flagz:flagz@localhost:5432/flagz?sslmode=disable \
  go run ./cmd/server
```

Optional overrides: `HTTP_ADDR`, `GRPC_ADDR`, `STREAM_POLL_INTERVAL` (e.g. `500ms`).

## Run the full stack (app + Postgres in Docker)

```bash
# Build the local image first
docker build -t flagz:local .

# Start everything (migrations must still be applied separately from the host)
docker compose -f docker-compose.example.yml up -d
```

## Create a test API key

There is no CLI — insert directly into the database. The token format is `<id>.<raw_secret>`; the DB stores the SHA-256 hex of `<raw_secret>`.

Compute the hash and insert in one step:

```bash
SECRET="mysecret"
HASH=$(echo -n "$SECRET" | sha256sum | awk '{print $1}')

psql "postgresql://flagz:flagz@localhost:5432/flagz?sslmode=disable" \
  -c "INSERT INTO api_keys (id, name, key_hash) VALUES ('dev-key', 'Dev Key', '$HASH');"
```

The bearer token to use in requests is then: `dev-key.mysecret`

## Reset state

```bash
# Wipe the DB volume and start fresh
docker compose -f docker-compose.example.yml down -v
docker compose -f docker-compose.example.yml up -d postgres
goose -dir ./migrations postgres "postgresql://flagz:flagz@localhost:5432/flagz?sslmode=disable" up
```
