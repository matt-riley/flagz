# @matt-riley/flagz

TypeScript client for the [flagz](../../README.md) feature flag service. Supports HTTP and gRPC transports, with **zero runtime dependencies** for HTTP-only usage.

> Ship features, not if-statements. Type-safe feature flags with real-time streaming — batteries included, node_modules guilt not included.

## Features

- **Zero runtime dependencies** — HTTP client uses only `fetch` and `ReadableStream` (built into Node 18+, browsers, Deno, Bun, Cloudflare Workers)
- **Full TypeScript types** — strict interfaces for flags, rules, evaluation contexts, and events; mistakes are caught at compile time, not 3 AM
- **Two transports, one interface** — swap between HTTP and gRPC without changing application code
- **Real-time streaming** — SSE (HTTP) and server-streaming RPC (gRPC) with `lastEventId` for resumption
- **Batch evaluation** — resolve multiple flags in a single round-trip
- **Mockable by design** — `FlagManager`, `Evaluator`, and `Streamer` interfaces make testing trivial

## Install

### HTTP only (zero additional dependencies)

```sh
npm install @matt-riley/flagz
```

### With gRPC support

```sh
npm install @matt-riley/flagz @grpc/grpc-js @grpc/proto-loader
```

## Quick start — HTTP

```typescript
import { createHTTPClient } from '@matt-riley/flagz/http'

const client = createHTTPClient({
  baseURL: 'http://localhost:8080',
  apiKey: 'your-api-key-id.your-secret',
})

// Evaluate a flag
const enabled = await client.evaluate('my-feature', {}, false)

// CRUD
const flag = await client.getFlag('my-feature')
const flags = await client.listFlags()

// Stream real-time changes
for await (const event of client.stream()) {
  console.log(event.type, event.key)
}
```

## Quick start — gRPC

```typescript
import { createGRPCClient } from '@matt-riley/flagz/grpc'

const client = await createGRPCClient({
  address: 'localhost:9090',
  apiKey: 'your-api-key-id.your-secret',
})

const enabled = await client.evaluate('my-feature', {}, false)
```

## Complete CRUD example

```typescript
import { createHTTPClient } from '@matt-riley/flagz/http'
import type { Flag } from '@matt-riley/flagz'

const client = createHTTPClient({
  baseURL: 'http://localhost:8080',
  apiKey: 'key-id.secret',
})

// Create
const flag = await client.createFlag({
  key: 'dark-mode',
  enabled: false,
  description: 'Toggle dark mode for all users',
  rules: [
    { attribute: 'plan', operator: 'equals', value: 'enterprise' },
  ],
})
console.log(`Created: ${flag.key} (enabled=${flag.enabled})`)

// Read
const fetched = await client.getFlag('dark-mode')
console.log(`Description: ${fetched.description}`)

// List
const allFlags = await client.listFlags()
console.log(`Total flags: ${allFlags.length}`)

// Update — enable the flag and add a variant
const updated = await client.updateFlag({
  ...fetched,
  enabled: true,
  variants: { beta: true, stable: false },
})
console.log(`Updated: enabled=${updated.enabled}`)

// Delete
await client.deleteFlag('dark-mode')
console.log('Deleted dark-mode')
```

## Batch evaluation

```typescript
const results = await client.evaluateBatch([
  { key: 'feature-a', defaultValue: false },
  { key: 'feature-b', defaultValue: true },
])
for (const { key, value } of results) {
  console.log(`${key} = ${value}`)
}
```

## Error handling

The HTTP client throws `HTTPError` for non-2xx responses, carrying the status code and server message:

```typescript
import { createHTTPClient, HTTPError } from '@matt-riley/flagz/http'

const client = createHTTPClient({ baseURL: 'http://localhost:8080', apiKey: '...' })

try {
  await client.getFlag('nonexistent')
} catch (err) {
  if (err instanceof HTTPError) {
    switch (err.status) {
      case 401: console.error('Bad API key — check the id.secret format'); break
      case 404: console.error('Flag not found'); break
      case 400: console.error(`Bad request: ${err.message}`); break
      default:  console.error(`Server error (${err.status}): ${err.message}`)
    }
  } else {
    // Network error, DNS failure, etc.
    console.error('Connection failed:', err)
  }
}
```

For gRPC, errors surface as standard `@grpc/grpc-js` `ServiceError` instances with `.code` and `.details`:

```typescript
import { status as grpcStatus } from '@grpc/grpc-js'

try {
  await grpcClient.getFlag('nonexistent')
} catch (err: any) {
  if (err.code === grpcStatus.NOT_FOUND) {
    console.error('Flag does not exist')
  }
}
```

## Streaming with reconnection

The `stream()` method returns an `AsyncIterable<FlagEvent>`. For production use, wrap it in a reconnection loop with exponential backoff:

```typescript
async function watchFlags(client: FlagClient) {
  let lastEventId: string | undefined
  let backoff = 1000

  while (true) {
    try {
      for await (const event of client.stream(lastEventId)) {
        backoff = 1000 // reset on each successful event
        lastEventId = event.eventId

        switch (event.type) {
          case 'update':
            console.log(`Flag updated: ${event.key} → enabled=${event.flag?.enabled}`)
            break
          case 'delete':
            console.log(`Flag deleted: ${event.key}`)
            break
          case 'error':
            console.warn('Malformed event received')
            break
        }
      }
      // Stream ended cleanly (server closed connection) — reconnect
    } catch (err) {
      console.error('Stream error, reconnecting…', err)
    }

    await new Promise(r => setTimeout(r, backoff))
    backoff = Math.min(backoff * 2, 30_000) // cap at 30 s
  }
}
```

The `lastEventId` parameter lets the server resume from where you left off, so you won't miss events during the reconnection window.

## SSE reconnect

The HTTP `stream()` method accepts an optional `lastEventId` string for resuming a stream after a disconnect:

```typescript
let lastId: string | undefined

for await (const event of client.stream(lastId)) {
  lastId = event.eventId
  console.log(event.type, event.key)
}
```

## Framework integration

### React — custom hook

```typescript
import { useEffect, useState } from 'react'
import type { FlagClient } from '@matt-riley/flagz'

export function useFlag(client: FlagClient, key: string, defaultValue = false) {
  const [enabled, setEnabled] = useState(defaultValue)

  useEffect(() => {
    client.evaluate(key, {}, defaultValue).then(setEnabled)
  }, [client, key, defaultValue])

  return enabled
}
```

### Next.js — server-side evaluation

```typescript
// app/page.tsx (Server Component)
import { createHTTPClient } from '@matt-riley/flagz/http'

const flagz = createHTTPClient({
  baseURL: process.env.FLAGZ_URL!,
  apiKey: process.env.FLAGZ_API_KEY!,
})

export default async function Page() {
  const showBanner = await flagz.evaluate('promo-banner', {}, false)
  return showBanner ? <PromoBanner /> : null
}
```

### Express — middleware

```typescript
import type { FlagClient } from '@matt-riley/flagz'
import type { RequestHandler } from 'express'

export function requireFlag(client: FlagClient, key: string): RequestHandler {
  return async (_req, res, next) => {
    const enabled = await client.evaluate(key, {}, false)
    if (!enabled) return res.status(404).end()
    next()
  }
}
```

## Testing & mocking

Both clients implement `FlagClient` (`FlagManager & Evaluator & Streamer`), so mocks slot in with zero fuss:

```typescript
import type { FlagClient, Flag, FlagEvent } from '@matt-riley/flagz'
import { describe, it, expect } from 'vitest'

function createMockClient(overrides: Partial<FlagClient> = {}): FlagClient {
  const flags = new Map<string, Flag>()

  return {
    createFlag: async (f) => { flags.set(f.key, f); return f },
    getFlag: async (key) => {
      const f = flags.get(key)
      if (!f) throw new Error('not found')
      return f
    },
    listFlags: async () => [...flags.values()],
    updateFlag: async (f) => { flags.set(f.key, f); return f },
    deleteFlag: async (key) => { flags.delete(key) },
    evaluate: async (_key, _ctx, defaultValue) => defaultValue,
    evaluateBatch: async (reqs) => reqs.map(r => ({ key: r.key, value: r.defaultValue })),
    async *stream() { /* yields nothing — override if your test needs events */ },
    ...overrides,
  }
}

describe('feature gating', () => {
  it('returns default when flag is missing', async () => {
    const client = createMockClient({
      evaluate: async () => false,
    })
    expect(await client.evaluate('new-ui', {}, false)).toBe(false)
  })
})
```

You can also inject a mock `fetch` into the HTTP client for lower-level tests:

```typescript
import { createHTTPClient } from '@matt-riley/flagz/http'

const mockFetch = async (input: RequestInfo | URL, init?: RequestInit) => {
  return new Response(JSON.stringify({ flag: { key: 'test', enabled: true } }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

const client = createHTTPClient({
  baseURL: 'http://test',
  apiKey: 'test.key',
  fetch: mockFetch as typeof globalThis.fetch,
})
```

## Configuration reference

### HTTPConfig

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `baseURL` | `string` | ✅ | — | Server URL, e.g. `"http://localhost:8080"` |
| `apiKey` | `string` | ✅ | — | Bearer token in `"id.secret"` format |
| `fetch` | `typeof globalThis.fetch` | — | `globalThis.fetch` | Custom fetch implementation for testing or edge runtimes |

### GRPCConfig

| Option | Type | Required | Default | Description |
|--------|------|----------|---------|-------------|
| `address` | `string` | ✅ | — | Host and port, e.g. `"localhost:9090"` |
| `apiKey` | `string` | ✅ | — | Bearer token in `"id.secret"` format |
| `credentials` | `ChannelCredentials` | — | `createInsecure()` | TLS credentials for production use |

## Extending

Both clients return objects that implement the shared interfaces exported from `@matt-riley/flagz`:

```typescript
import type { FlagManager, Evaluator, Streamer, FlagClient } from '@matt-riley/flagz'

// Use individual interfaces for narrow contracts
function displayFlags(manager: FlagManager) { ... }

// Or the combined type when you need everything
function initApp(client: FlagClient) { ... }
```

You can swap transports or provide mocks in tests by implementing these interfaces.

## TLS (gRPC)

Pass `credentials` via the config:

```typescript
import { credentials } from '@grpc/grpc-js'
import { createGRPCClient } from '@matt-riley/flagz/grpc'

const client = await createGRPCClient({
  address: 'flagz.example.com:443',
  apiKey: '...',
  credentials: credentials.createSsl(),
})
```

## Browser vs Node.js

The HTTP client works anywhere `fetch` and `ReadableStream` are available:

| Environment | Support | Notes |
|-------------|---------|-------|
| **Node.js 18+** | ✅ | Built-in `fetch` — no polyfills needed |
| **Browsers** | ✅ | Native `fetch` + `EventSource`-style streaming via `ReadableStream` |
| **Deno / Bun** | ✅ | `fetch` is a first-class citizen |
| **Cloudflare Workers** | ✅ | Inject the Workers `fetch` via the `config.fetch` option |

> **Note:** The gRPC client requires Node.js — `@grpc/grpc-js` does not run in browsers or edge runtimes.

SSE streaming in browsers works out of the box. Unlike `EventSource`, the client uses `fetch` directly, which means the `Authorization` header is sent properly (no query-string token workarounds).

## TypeScript strictness

The types are designed to catch mistakes at compile time. Here are some things the compiler will flag for you:

```typescript
import type { Flag, Rule, EvaluationContext } from '@matt-riley/flagz'

// ✅ Rule operators are a union type — typos are caught
const rule: Rule = { attribute: 'plan', operator: 'equals', value: 'pro' }

// ❌ Compile error: '"contains"' is not assignable to type '"equals" | "in"'
const bad: Rule = { attribute: 'plan', operator: 'contains', value: 'pro' }

// ✅ evaluate() requires all three arguments — no accidental undefined defaults
const enabled = await client.evaluate('flag-key', {}, false)

// ❌ Compile error: Expected 3 arguments, got 1
const oops = await client.evaluate('flag-key')

// ✅ FlagEvent.type is a discriminated union — exhaustive switch checking works
function handle(event: FlagEvent) {
  switch (event.type) {
    case 'update': console.log(event.flag?.enabled); break
    case 'delete': console.log(`${event.key} removed`); break
    case 'error':  console.warn('bad event'); break
    // No default needed — TypeScript knows this is exhaustive
  }
}
```

## Requirements

- Node.js >= 18 (for built-in `fetch` and `ReadableStream`)
- gRPC transport additionally requires `@grpc/grpc-js` and `@grpc/proto-loader`

## License

[MIT](../../LICENSE)
