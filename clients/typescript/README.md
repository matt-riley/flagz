# @matt-riley/flagz

TypeScript client for the [flagz](../../README.md) feature flag service. Supports HTTP and gRPC transports, with **zero runtime dependencies** for HTTP-only usage.

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

## SSE reconnect

The HTTP `stream()` method accepts an optional `lastEventId` string for resuming a stream after a disconnect:

```typescript
let lastId: string | undefined

for await (const event of client.stream(lastId)) {
  lastId = event.eventId
  console.log(event.type, event.key)
}
```

## Extending

Both clients return objects that implement the shared interfaces exported from `@matt-riley/flagz`:

```typescript
import type { FlagManager, Evaluator, Streamer } from '@matt-riley/flagz'

function useClient(client: FlagManager & Evaluator & Streamer) { ... }
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

## Requirements

- Node.js >= 18 (for built-in `fetch` and `ReadableStream`)
- gRPC transport additionally requires `@grpc/grpc-js` and `@grpc/proto-loader`
