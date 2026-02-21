import { describe, it, expect, vi } from 'vitest'
import * as fc from 'fast-check'
import { createHTTPClient } from './client.js'
import type { Flag } from '../types.js'

// --- Shared helpers ---------------------------------------------------------

const baseConfig = {
  baseURL: 'http://localhost:8080',
  apiKey: 'test-id.test-secret',
}

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function makeSSEResponse(events: string): Response {
  const encoded = new TextEncoder().encode(events)
  const stream = new ReadableStream({
    start(c) { c.enqueue(encoded); c.close() },
  })
  return new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  })
}

// --- Arbitraries ------------------------------------------------------------

/** Arbitrary that generates a valid flag key string (printable ASCII, non-empty). */
const arbKey = fc.stringMatching(/^[a-zA-Z0-9_-]{1,64}$/)

/** Arbitrary for a flat variants map: string → boolean. */
const arbVariants = fc.dictionary(arbKey, fc.boolean(), { maxKeys: 10 })

/** Arbitrary for a rule array. */
const arbRules = fc.array(
  fc.record({
    attribute: fc.string({ maxLength: 32 }),
    operator: fc.constantFrom('equals', 'in' as const),
    value: fc.oneof(fc.string(), fc.boolean(), fc.integer()),
  }),
  { maxLength: 5 },
)

/** Arbitrary for a full Flag. */
const arbFlag = fc.record<Flag>({
  key: arbKey,
  enabled: fc.boolean(),
  description: fc.option(fc.string({ maxLength: 64 }), { nil: undefined }),
  variants: fc.option(arbVariants, { nil: undefined }),
  rules: fc.option(arbRules as fc.Arbitrary<Flag['rules']>, { nil: undefined }),
  createdAt: fc.constant(undefined),
  updatedAt: fc.constant(undefined),
})

// --- Property tests ---------------------------------------------------------

describe('HTTP client property tests', () => {

  // 1. Flag key and enabled are always preserved through a mock-server roundtrip.
  it('flag key and enabled are preserved through createFlag roundtrip', async () => {
    await fc.assert(
      fc.asyncProperty(arbFlag, async (flag) => {
        const fetch = vi.fn().mockResolvedValue(
          makeResponse({ flag: { key: flag.key, enabled: flag.enabled } }),
        )
        const client = createHTTPClient({ ...baseConfig, fetch })
        const result = await client.createFlag(flag)
        expect(result.key).toBe(flag.key)
        expect(result.enabled).toBe(flag.enabled)
      }),
      { numRuns: 100 },
    )
  })

  // 2. variants are preserved through getFlag roundtrip.
  it('variants survive wireToFlag(flagToWire(flag)) roundtrip', async () => {
    await fc.assert(
      fc.asyncProperty(arbKey, arbVariants, async (key, variants) => {
        const fetch = vi.fn().mockResolvedValue(
          makeResponse({ flag: { key, enabled: true, variants } }),
        )
        const client = createHTTPClient({ ...baseConfig, fetch })
        const result = await client.getFlag(key)
        expect(result.variants).toEqual(variants)
      }),
      { numRuns: 100 },
    )
  })

  // 3. SSE stream yields exactly N events for N complete SSE messages.
  it('SSE stream yields exactly N events for N complete SSE messages', async () => {
    await fc.assert(
      fc.asyncProperty(
        fc.array(
          fc.record({ key: arbKey, type: fc.constantFrom('update', 'delete') }),
          { minLength: 1, maxLength: 20 },
        ),
        async (eventList) => {
          const sseData = eventList
            .map(ev => `event:${ev.type}\ndata:{"key":"${ev.key}"}\n\n`)
            .join('')
          const fetch = vi.fn().mockResolvedValue(makeSSEResponse(sseData))
          const client = createHTTPClient({ ...baseConfig, fetch })
          let count = 0
          for await (const _ of client.stream()) { count++ }
          expect(count).toBe(eventList.length)
        },
      ),
      { numRuns: 50 },
    )
  })

  // 4. SSE event types are always 'update' | 'delete' | 'error'.
  it('SSE events always have valid type', async () => {
    await fc.assert(
      fc.asyncProperty(
        fc.array(
          fc.record({ key: arbKey, type: fc.constantFrom('update', 'delete') }),
          { minLength: 1, maxLength: 10 },
        ),
        async (eventList) => {
          const sseData = eventList
            .map(ev => `event:${ev.type}\ndata:{"key":"${ev.key}"}\n\n`)
            .join('')
          const fetch = vi.fn().mockResolvedValue(makeSSEResponse(sseData))
          const client = createHTTPClient({ ...baseConfig, fetch })
          for await (const ev of client.stream()) {
            expect(['update', 'delete', 'error']).toContain(ev.type)
          }
        },
      ),
      { numRuns: 50 },
    )
  })

  // 5. SSE events with a data: line always have a non-empty key.
  it('SSE update/delete events always carry the key from data JSON', async () => {
    await fc.assert(
      fc.asyncProperty(arbKey, async (key) => {
        const sseData = `event:update\ndata:{"key":"${key}","enabled":true}\n\n`
        const fetch = vi.fn().mockResolvedValue(makeSSEResponse(sseData))
        const client = createHTTPClient({ ...baseConfig, fetch })
        for await (const ev of client.stream()) {
          expect(ev.key).toBe(key)
        }
      }),
      { numRuns: 100 },
    )
  })

  // 6. evaluateBatch result length always equals request length.
  it('evaluateBatch always returns same number of results as requests', async () => {
    await fc.assert(
      fc.asyncProperty(
        fc.array(fc.record({ key: arbKey, defaultValue: fc.boolean() }), { maxLength: 20 }),
        async (requests) => {
          const fetch = vi.fn().mockResolvedValue(
            makeResponse({
              results: requests.map(r => ({ key: r.key, value: r.defaultValue })),
            }),
          )
          const client = createHTTPClient({ ...baseConfig, fetch })
          const results = await client.evaluateBatch(requests)
          expect(results).toHaveLength(requests.length)
        },
      ),
      { numRuns: 100 },
    )
  })

  // 7. SSE parser does not throw on arbitrary ASCII event data.
  it('SSE parser never throws on arbitrary printable-ASCII data: content', async () => {
    await fc.assert(
      fc.asyncProperty(
        fc.array(fc.string({ maxLength: 128 }), { minLength: 1, maxLength: 10 }),
        async (dataLines) => {
          // Construct a valid SSE message with arbitrary data lines.
          const sseData = dataLines.map(l => `data:${l}\n`).join('') + '\n'
          const fetch = vi.fn().mockResolvedValue(makeSSEResponse(sseData))
          const client = createHTTPClient({ ...baseConfig, fetch })
          // Should not throw; events with malformed JSON will have type 'error'.
          for await (const ev of client.stream()) {
            expect(['update', 'delete', 'error']).toContain(ev.type)
          }
        },
      ),
      { numRuns: 200 },
    )
  })

  // 8. Authorization header is always sent with every request.
  it('Authorization header is always present on all CRUD requests', async () => {
    await fc.assert(
      fc.asyncProperty(arbFlag, async (flag) => {
        const fetch = vi.fn().mockResolvedValue(
          makeResponse({ flag: { key: flag.key, enabled: flag.enabled } }),
        )
        const client = createHTTPClient({ ...baseConfig, fetch })
        await client.createFlag(flag)
        expect(fetch.mock.calls[0][1]?.headers).toMatchObject({
          Authorization: 'Bearer test-id.test-secret',
        })
      }),
      { numRuns: 50 },
    )
  })
})
