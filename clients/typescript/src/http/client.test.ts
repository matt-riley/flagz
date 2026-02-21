import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createHTTPClient, HTTPError } from './client.js'
import type { HTTPConfig } from './client.js'
import type { Flag } from '../types.js'

// Helpers

const baseConfig: HTTPConfig = {
  baseURL: 'http://localhost:8080',
  apiKey: 'test-id.test-secret',
}

const sampleFlag: Flag = {
  key: 'my-flag',
  description: 'A test flag',
  enabled: true,
}

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function mockFetch(response: Response | (() => Response)) {
  const fn = vi.fn().mockImplementation(() =>
    typeof response === 'function' ? response() : response,
  )
  return fn
}

function captureRequests(): [typeof globalThis.fetch, { url: string; init?: RequestInit }[]] {
  const calls: { url: string; init?: RequestInit }[] = []
  const fn = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
    calls.push({ url, init })
    return Promise.resolve(makeResponse({ flag: { key: 'x', enabled: true } }))
  }) as typeof globalThis.fetch
  return [fn, calls]
}

// -- FlagManager tests -------------------------------------------------------

describe('createHTTPClient', () => {
  describe('createFlag', () => {
    it('POSTs to /v1/flags and returns mapped flag', async () => {
      const fetch = mockFetch(makeResponse({ flag: { key: 'my-flag', enabled: true, created_at: '2024-01-01T00:00:00Z' } }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      const result = await client.createFlag(sampleFlag)
      expect(result.key).toBe('my-flag')
      expect(result.enabled).toBe(true)
      expect(result.createdAt).toBe('2024-01-01T00:00:00Z')
      expect(fetch).toHaveBeenCalledWith(
        'http://localhost:8080/v1/flags',
        expect.objectContaining({ method: 'POST' }),
      )
    })

    it('sends Authorization header', async () => {
      const [fetch, calls] = captureRequests()
      const client = createHTTPClient({ ...baseConfig, fetch })
      await client.createFlag(sampleFlag)
      expect(calls[0].init?.headers).toMatchObject({ Authorization: 'Bearer test-id.test-secret' })
    })
  })

  describe('getFlag', () => {
    it('GETs /v1/flags/:key', async () => {
      const fetch = mockFetch(makeResponse({ flag: { key: 'my-flag', enabled: false } }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      const result = await client.getFlag('my-flag')
      expect(result.key).toBe('my-flag')
      expect(fetch).toHaveBeenCalledWith(
        'http://localhost:8080/v1/flags/my-flag',
        expect.objectContaining({ method: 'GET' }),
      )
    })

    it('throws HTTPError on 404', async () => {
      const fetch = vi.fn().mockResolvedValue(new Response('not found', { status: 404 }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      await expect(client.getFlag('missing')).rejects.toThrow(HTTPError)
      await expect(client.getFlag('missing')).rejects.toMatchObject({ status: 404 })
    })

    it('throws HTTPError on 401', async () => {
      const fetch = vi.fn().mockResolvedValue(new Response('unauthorized', { status: 401 }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      await expect(client.getFlag('x')).rejects.toMatchObject({ status: 401 })
    })
  })

  describe('listFlags', () => {
    it('GETs /v1/flags and returns all flags', async () => {
      const fetch = mockFetch(
        makeResponse({ flags: [{ key: 'a', enabled: true }, { key: 'b', enabled: false }] }),
      )
      const client = createHTTPClient({ ...baseConfig, fetch })
      const flags = await client.listFlags()
      expect(flags).toHaveLength(2)
      expect(flags[0].key).toBe('a')
    })

    it('returns empty array when flags is null/absent', async () => {
      const fetch = mockFetch(makeResponse({ flags: null }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      const flags = await client.listFlags()
      expect(flags).toEqual([])
    })
  })

  describe('updateFlag', () => {
    it('PUTs to /v1/flags/:key', async () => {
      const fetch = mockFetch(makeResponse({ flag: { key: 'my-flag', enabled: false } }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      const result = await client.updateFlag({ ...sampleFlag, enabled: false })
      expect(result.enabled).toBe(false)
      expect(fetch).toHaveBeenCalledWith(
        'http://localhost:8080/v1/flags/my-flag',
        expect.objectContaining({ method: 'PUT' }),
      )
    })
  })

  describe('deleteFlag', () => {
    it('DELETEs /v1/flags/:key', async () => {
      const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      await expect(client.deleteFlag('my-flag')).resolves.toBeUndefined()
      expect(fetch).toHaveBeenCalledWith(
        'http://localhost:8080/v1/flags/my-flag',
        expect.objectContaining({ method: 'DELETE' }),
      )
    })
  })

  // -- Evaluator tests -------------------------------------------------------

  describe('evaluate', () => {
    it('POSTs to /v1/evaluate with single key', async () => {
      const fetch = vi.fn().mockResolvedValue(makeResponse({ results: [{ key: 'my-flag', value: true }] }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      const v = await client.evaluate('my-flag', {}, false)
      expect(v).toBe(true)
      const body = JSON.parse(fetch.mock.calls[0][1]?.body as string)
      expect(body.key).toBe('my-flag')
    })

    it('sends Authorization header', async () => {
      const fetch = vi.fn().mockResolvedValue(makeResponse({ results: [{ key: 'x', value: false }] }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      await client.evaluate('x', {}, false)
      expect(fetch.mock.calls[0][1]?.headers).toMatchObject({ Authorization: 'Bearer test-id.test-secret' })
    })
  })

  describe('evaluateBatch', () => {
    it('POSTs requests array to /v1/evaluate', async () => {
      const fetch = vi.fn().mockResolvedValue(
        makeResponse({ results: [{ key: 'a', value: true }, { key: 'b', value: false }] }),
      )
      const client = createHTTPClient({ ...baseConfig, fetch })
      const results = await client.evaluateBatch([
        { key: 'a', defaultValue: false },
        { key: 'b', defaultValue: true },
      ])
      expect(results).toEqual([{ key: 'a', value: true }, { key: 'b', value: false }])
      const body = JSON.parse(fetch.mock.calls[0][1]?.body as string)
      expect(Array.isArray(body.requests)).toBe(true)
      expect(body.requests).toHaveLength(2)
    })
  })

  // -- SSE streaming tests ---------------------------------------------------

  describe('stream', () => {
    function makeSSEResponse(events: string): Response {
      const encoder = new TextEncoder()
      const encoded = encoder.encode(events)
      const stream = new ReadableStream({
        start(controller) {
          controller.enqueue(encoded)
          controller.close()
        },
      })
      return new Response(stream, {
        status: 200,
        headers: { 'Content-Type': 'text/event-stream' },
      })
    }

    it('yields update and delete events from SSE stream', async () => {
      const sseData = [
        'id:1\nevent:update\ndata:{"key":"flag-a","enabled":true}\n\n',
        'id:2\nevent:delete\ndata:{"key":"flag-b"}\n\n',
      ].join('')

      const fetch = vi.fn().mockResolvedValue(makeSSEResponse(sseData))
      const client = createHTTPClient({ ...baseConfig, fetch })

      const events = []
      for await (const ev of client.stream()) {
        events.push(ev)
      }

      expect(events).toHaveLength(2)
      expect(events[0]).toMatchObject({ type: 'update', key: 'flag-a', eventId: '1' })
      expect(events[1]).toMatchObject({ type: 'delete', key: 'flag-b', eventId: '2' })
    })

    it('sends Last-Event-ID header on reconnect', async () => {
      const fetch = vi.fn().mockResolvedValue(makeSSEResponse(''))
      const client = createHTTPClient({ ...baseConfig, fetch })
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      for await (const _ of client.stream('42')) { break }
      expect(fetch.mock.calls[0][1]?.headers).toMatchObject({ 'Last-Event-ID': '42' })
    })

    it('throws HTTPError on non-2xx SSE response', async () => {
      const fetch = vi.fn().mockResolvedValue(new Response('forbidden', { status: 403 }))
      const client = createHTTPClient({ ...baseConfig, fetch })
      await expect(async () => {
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        for await (const _ of client.stream()) { /* no-op */ }
      }).rejects.toThrow(HTTPError)
    })

    it('sends Authorization header on stream request', async () => {
      const fetch = vi.fn().mockResolvedValue(makeSSEResponse(''))
      const client = createHTTPClient({ ...baseConfig, fetch })
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      for await (const _ of client.stream()) { /* no-op */ }
      expect(fetch.mock.calls[0][1]?.headers).toMatchObject({ Authorization: 'Bearer test-id.test-secret' })
    })

    it('concatenates multi-line data fields into a single JSON object', async () => {
      // SSE spec: consecutive data: lines are concatenated with '\n'.
      // Split a valid JSON object across two data: lines to verify concatenation.
      const sseData = 'event:update\ndata:{"key":"multi-flag",\ndata:"enabled":true}\n\n'
      const fetch = vi.fn().mockResolvedValue(makeSSEResponse(sseData))
      const client = createHTTPClient({ ...baseConfig, fetch })
      const events = []
      for await (const ev of client.stream()) {
        events.push(ev)
      }
      expect(events).toHaveLength(1)
      expect(events[0]).toMatchObject({ type: 'update', key: 'multi-flag' })
    })
  })
})

// Ensure the module doesn't import @grpc/* types at the top level.
// (If it did, this test file would fail to load without @grpc/grpc-js installed.)
it('module imports without gRPC dependency', async () => {
  const mod = await import('./client.js')
  expect(typeof mod.createHTTPClient).toBe('function')
})
