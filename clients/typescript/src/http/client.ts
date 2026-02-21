import type { Flag, EvaluationContext, EvaluateRequest, EvaluateResult, FlagEvent, FlagClient } from '../types.js'

/** Configuration for the HTTP client. */
export interface HTTPConfig {
  /** Base URL of the flagz server, e.g. "http://localhost:8080" */
  baseURL: string
  /** Bearer token in "id.secret" format */
  apiKey: string
  /**
   * Optional fetch implementation. Defaults to globalThis.fetch.
   * Inject a mock for testing or a custom implementation for edge runtimes.
   */
  fetch?: typeof globalThis.fetch
}

/** An HTTP error returned by the flagz server. */
export class HTTPError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(`flagz: HTTP ${status}: ${message}`)
    this.name = 'HTTPError'
  }
}

/** Creates an HTTP client for the flagz feature flag service. */
export function createHTTPClient(config: HTTPConfig): FlagClient {
  const fetchFn = config.fetch ?? globalThis.fetch

  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = {
      Authorization: `Bearer ${config.apiKey}`,
    }
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json'
    }
    const res = await fetchFn(`${config.baseURL}${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
    if (!res.ok) {
      const text = await res.text().catch(() => '')
      throw new HTTPError(res.status, text.trim())
    }
    // 204 No Content
    if (res.status === 204) return undefined as T
    return res.json() as Promise<T>
  }

  // Wire flag shape from the HTTP API
  type WireFlag = {
    key: string
    description?: string
    enabled: boolean
    variants?: Record<string, boolean>
    rules?: Array<{ attribute: string; operator: string; value: unknown }>
    created_at?: string
    updated_at?: string
  }

  function wireToFlag(w: WireFlag): Flag {
    return {
      key: w.key,
      description: w.description,
      enabled: w.enabled,
      variants: w.variants,
      rules: w.rules as Flag['rules'],
      createdAt: w.created_at,
      updatedAt: w.updated_at,
    }
  }

  function flagToWire(f: Flag): WireFlag {
    return {
      key: f.key,
      description: f.description,
      enabled: f.enabled,
      variants: f.variants,
      rules: f.rules as WireFlag['rules'],
    }
  }

  // FlagManager
  async function createFlag(flag: Flag): Promise<Flag> {
    const res = await request<{ flag: WireFlag }>('POST', '/v1/flags', { flag: flagToWire(flag) })
    return wireToFlag(res.flag)
  }

  async function getFlag(key: string): Promise<Flag> {
    const res = await request<{ flag: WireFlag }>('GET', `/v1/flags/${encodeURIComponent(key)}`)
    return wireToFlag(res.flag)
  }

  async function listFlags(): Promise<Flag[]> {
    const res = await request<{ flags: WireFlag[] }>('GET', '/v1/flags')
    return (res.flags ?? []).map(wireToFlag)
  }

  async function updateFlag(flag: Flag): Promise<Flag> {
    const res = await request<{ flag: WireFlag }>('PUT', `/v1/flags/${encodeURIComponent(flag.key)}`, { flag: flagToWire(flag) })
    return wireToFlag(res.flag)
  }

  async function deleteFlag(key: string): Promise<void> {
    await request<void>('DELETE', `/v1/flags/${encodeURIComponent(key)}`)
  }

  // Evaluator
  async function evaluate(key: string, ctx: EvaluationContext, defaultValue: boolean): Promise<boolean> {
    const res = await request<{ results: Array<{ key: string; value: boolean }> }>('POST', '/v1/evaluate', {
      key,
      context: ctx,
      default_value: defaultValue,
    })
    const results = res.results ?? []
    if (results.length !== 1) {
      throw new Error(`flagz: expected exactly 1 evaluation result, got ${results.length}`)
    }
    return results[0].value
  }

  async function evaluateBatch(requests: EvaluateRequest[]): Promise<EvaluateResult[]> {
    const res = await request<{ results: Array<{ key: string; value: boolean }> }>('POST', '/v1/evaluate', {
      requests: requests.map(r => ({
        key: r.key,
        context: r.context,
        default_value: r.defaultValue,
      })),
    })
    return (res.results ?? []).map(r => ({ key: r.key, value: r.value }))
  }

  // Streamer — SSE via fetch + ReadableStream (no extra deps, supports Authorization header)
  async function* stream(lastEventId?: string): AsyncIterable<FlagEvent> {
    const headers: Record<string, string> = {
      Authorization: `Bearer ${config.apiKey}`,
    }
    if (lastEventId !== undefined) {
      headers['Last-Event-ID'] = lastEventId
    }
    const res = await fetchFn(`${config.baseURL}/v1/stream`, { headers })
    if (!res.ok) {
      const text = await res.text().catch(() => '')
      throw new HTTPError(res.status, text.trim())
    }
    if (!res.body) throw new Error('flagz: SSE response has no body')

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    // Per-event accumulators
    let eventType = ''
    let dataLines: string[] = []
    let eventId: string | undefined

    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() ?? ''

        for (const rawLine of lines) {
          const line = rawLine.replace(/\r$/, '')
          if (line === '') {
            // Blank line: dispatch if we have data
            if (dataLines.length > 0) {
              const data = dataLines.join('\n')
              const ev: FlagEvent = {
                type: (eventType as FlagEvent['type']) || 'update',
                key: '',
                eventId,
              }
              if (ev.type === 'update' || ev.type === 'delete') {
                try {
                  const parsed = JSON.parse(data) as WireFlag
                  ev.flag = wireToFlag(parsed)
                  ev.key = parsed.key ?? ''
                } catch {
                  // malformed JSON: emit error event
                  ev.type = 'error'
                }
              }
              yield ev
            }
            // Reset accumulators
            eventType = ''
            dataLines = []
            eventId = undefined
          } else if (line.startsWith('id:')) {
            eventId = line.slice(3).trimStart()
          } else if (line.startsWith('event:')) {
            eventType = line.slice(6).trimStart()
          } else if (line.startsWith('data:')) {
            dataLines.push(line.slice(5).trimStart())
          }
          // Lines starting with ':' are SSE comments — ignored.
        }
      }
    } finally {
      reader.cancel().catch(() => {})
    }
  }

  return { createFlag, getFlag, listFlags, updateFlag, deleteFlag, evaluate, evaluateBatch, stream }
}
