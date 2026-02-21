# Architecture

`flagz` is a high-performance, self-hosted feature flag service designed for low-latency evaluation and real-time updates. It combines the reliability of PostgreSQL with the speed of in-memory caching.

## System Overview

The system is built as a single binary that serves both HTTP and gRPC APIs. Its core design philosophy is **"read locally, write globally"**:
- **Reads (Evaluations):** Served exclusively from in-memory cache (zero DB IO).
- **Writes (Mutations):** Persisted to PostgreSQL, then propagated to all server nodes via NOTIFY/LISTEN.

## Component Architecture

```ascii
+-----------------------------------------------------------+
|                       Application                         |
|  (cmd/server/main.go - Wiring & Shutdown Coordination)    |
+-----------------------------------------------------------+
         |                       |
         v                       v
+-----------------+     +-----------------+
|   HTTP Server   |     |   gRPC Server   |
| (internal/server) |     | (internal/server) |
+-----------------+     +-----------------+
         |                       |
         +-----------+-----------+
                     |
                     v
      +-----------------------------+
      |      Service Layer          |
      |   (internal/service)        |
      | - In-memory Flag Cache      |
      | - Cache Invalidation Logic  |
      | - Event Publishing          |
      +-----------------------------+
                     |
            +--------+--------+
            |                 |
            v                 v
+-------------------+   +---------------------+
| Evaluation Engine |   |     Repository      |
|  (internal/core)  |   | (internal/repository) |
| - Rule Matching   |   | - PostgreSQL        |
| - Pure Functions  |   | - pgxpool           |
+-------------------+   +---------------------+
                               |
                               v
                     +------------------+
                     |    PostgreSQL    |
                     | - flags          |
                     | - api_keys       |
                     | - flag_events    |
                     +------------------+
```

## Package Structure

- **`cmd/server`**: Entry point. Parses config, initializes DB pool, wires services, and handles graceful shutdown.
- **`internal/config`**: Loads configuration from environment variables (12-factor app style).
- **`internal/core`**: The "brain". Contains pure functions for flag evaluation and rule matching. No side effects, no DB, no I/O.
- **`internal/service`**: Business logic. Manages the flag cache, coordinates DB writes with cache updates, and handles event publishing.
- **`internal/repository`**: Data access layer. Handles all SQL queries and Postgres-specific features (LISTEN/NOTIFY).
- **`internal/server`**: Transport layer. Translates HTTP/JSON and gRPC/Protobuf requests into Service calls.
- **`internal/middleware`**: Cross-cutting concerns like Authentication.

## Data Flow

1. **Evaluation Request**:
   - `GET /v1/flags/{key}` or `POST /v1/evaluate`
   - **Hit:** Service looks up flag in `cache map`.
   - **Eval:** Service converts stored flag to `core.Flag` and calls `core.EvaluateFlag`.
   - **Return:** Result returned immediately. No DB contact.

2. **Mutation Request**:
   - `POST /v1/flags`
   - **Persist:** Service calls Repository to `INSERT` into `flags`.
   - **Cache:** Service updates local `cache map` immediately.
   - **Notify:** Repository inserts into `flag_events` AND emits `pg_notify` on `flag_events` channel.
   - **Propagate:** Other replicas receive notification -> trigger `LoadCache` to resync.

## Caching Strategy

The system uses a **Read-Through / Write-Through** cache with **Event-Based Invalidation**.

1. **Startup:** Service loads *all* flags from DB into a thread-safe map (`sync.RWMutex`).
2. **Invalidation:**
   - The Service subscribes to the Postgres `flag_events` channel.
   - Upon receiving *any* notification, it triggers a full `LoadCache` (reload everything).
   - **Safety Net:** A 1-minute ticker forces a resync to handle any missed notifications.
3. **Local Updates:** The instance creating a flag updates its own cache immediately, so "read-your-writes" consistency is maintained locally.

## Event System & Streaming

Real-time updates to clients (SDKs) are handled via **Polling** the event log, while server-to-server sync uses **Push** (NOTIFY).

- **`flag_events` Table**: An append-only log of all changes (`updated`, `deleted`).
- **Client Streaming**:
  - **SSE (`/v1/stream`)**: Client provides `Last-Event-ID`. Server polls `flag_events` table every 1s (`STREAM_POLL_INTERVAL`) for new rows.
  - **gRPC (`WatchFlag`)**: Same polling mechanism. Supports server-side filtering by key.
- **Why Polling for Clients?** It scales better than holding thousands of open Postgres connections for `LISTEN`.

## Authentication

Authentication is mandatory for all `/v1/` routes.

- **Type:** Bearer Token.
- **Format:** `API_KEY_ID.API_KEY_SECRET`
- **Verification:**
  1. Parse ID and Secret.
  2. Fetch stored hash from `api_keys` table using ID.
  3. Compare Secret against hash.
- **Hashing Algorithms:**
  - **Primary:** Bcrypt (safe, slow).
  - **Legacy:** SHA-256 (fast, used for backward compatibility).

## Evaluation Engine

The engine (`internal/core`) performs rule-based evaluation.

- **Types:** Supports strings, numbers, and booleans.
- **Coercion:** Heavy use of reflection to handle numeric comparisons (e.g., comparing a JSON float `10.0` with a rule integer `10`).
- **Operators:**
  - `equals`: Strict equality (with coercion).
  - `in`: Checks if value exists in a list.
- **Hierarchy:**
  1. **Disabled?** Return `false` (note: DB stores `enabled`, Core uses `disabled`).
  2. **Rules:** Iterate list. First match wins (returns `true`).
  3. **Default:** If no rules match, return configured default (usually `true` or `false`).

## Database Schema

Three main tables in PostgreSQL:

1. **`flags`**: The source of truth.
   - `key` (PK): String identifier.
   - `variants`: JSONB (currently stores default value).
   - `rules`: JSONB array of rules.
2. **`api_keys`**: Credentials.
   - `key_hash`: Stores the bcrypt/sha256 hash, never the secret.
3. **`flag_events`**: Immutable audit log.
   - `event_id`: Serial monotonic counter. Used for resumption tokens (`Last-Event-ID`).
   - `payload`: Snapshot of the flag state at the time of event.

## Deployment

- **Container:** Docker image based on `gcr.io/distroless/static:nonroot` for security and minimal footprint.
- **Configuration:** Environment variables only.
  - `DATABASE_URL`: Postgres connection string.
  - `HTTP_ADDR` / `GRPC_ADDR`: Ports to bind.
  - `STREAM_POLL_INTERVAL`: How often to poll DB for client streams (default 1s).

## Design Decisions

- **In-Memory Cache vs. Redis:** chosen to eliminate network round-trip latency for evaluations. The trade-off is higher memory usage per pod and eventual consistency across pods.
- **Polling for Streams:** chosen over separate `LISTEN` connections per client to avoid exhausting Postgres connection limits.
- **JSONB for Rules:** chosen for flexibility. We can add complex rule structures (segments, percentages) without schema migrations.
- **Distroless Image:** chosen to reduce attack surface (no shell, no package manager).
