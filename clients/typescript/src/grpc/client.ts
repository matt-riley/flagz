import type { Flag, EvaluationContext, EvaluateRequest, EvaluateResult, FlagEvent, FlagClient } from '../types.js'

/** Configuration for the gRPC client. */
export interface GRPCConfig {
  /** Host and port of the flagz gRPC server, e.g. "localhost:9090" */
  address: string
  /** Bearer token in "id.secret" format */
  apiKey: string
  /**
   * Optional channel credentials.
   * Defaults to insecure credentials (suitable for localhost / plain-text).
   * Provide TLS credentials for production.
   */
  credentials?: import('@grpc/grpc-js').ChannelCredentials
}

/**
 * Creates a gRPC client for the flagz feature flag service.
 *
 * Requires `@grpc/grpc-js` and `@grpc/proto-loader` to be installed:
 *   npm install @grpc/grpc-js @grpc/proto-loader
 */
export async function createGRPCClient(config: GRPCConfig): Promise<FlagClient> {
  // Lazy-load gRPC deps — they are optional peer dependencies.
  const [grpc, protoLoader] = await Promise.all([
    import('@grpc/grpc-js').catch(() => {
      throw new Error('flagz: gRPC support requires: npm install @grpc/grpc-js @grpc/proto-loader')
    }),
    import('@grpc/proto-loader').catch(() => {
      throw new Error('flagz: gRPC support requires: npm install @grpc/grpc-js @grpc/proto-loader')
    }),
  ])

  // Resolve the bundled proto file path relative to this module.
  const { fileURLToPath } = await import('url')
  const { dirname, join } = await import('path')
  const __filename = fileURLToPath(import.meta.url)
  const __dirname = dirname(__filename)
  // Walk up from dist/grpc/ to find the proto directory.
  const protoPath = join(__dirname, '..', '..', 'proto', 'flag_service.proto')

  const pkgDef = protoLoader.loadSync(protoPath, {
    keepCase: true,
    longs: String, // int64 → string to avoid precision loss
    enums: String,
    defaults: true,
    oneofs: true,
  })

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const proto = grpc.loadPackageDefinition(pkgDef) as any
  const FlagService = proto.flagz.v1.FlagService as new (
    address: string,
    credentials: import('@grpc/grpc-js').ChannelCredentials,
    options?: object,
  ) => import('@grpc/grpc-js').Client

  const creds = config.credentials ?? grpc.credentials.createInsecure()
  const stub = new FlagService(config.address, creds)

  /** Returns gRPC call metadata with bearer token. */
  function authMeta(): import('@grpc/grpc-js').Metadata {
    const meta = new grpc.Metadata()
    meta.set('authorization', `Bearer ${config.apiKey}`)
    return meta
  }

  /** Wraps a gRPC unary call in a Promise. */
  function call<Req, Res>(method: string, req: Req): Promise<Res> {
    return new Promise((resolve, reject) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      ;(stub as any)[method](req, authMeta(), (err: Error | null, res: Res) => {
        if (err) reject(err)
        else resolve(res)
      })
    })
  }

  // Wire flag shape from the proto
  type ProtoFlag = {
    key: string
    description: string
    enabled: boolean
    variants_json: Buffer | string
    rules_json: Buffer | string
  }

  function protoToFlag(p: ProtoFlag): Flag {
    const f: Flag = {
      key: p.key,
      description: p.description || undefined,
      enabled: p.enabled,
    }
    const variantsRaw = p.variants_json
    if (variantsRaw && variantsRaw.length > 0) {
      try {
        f.variants = JSON.parse(variantsRaw.toString())
      } catch { /* ignore malformed */ }
    }
    const rulesRaw = p.rules_json
    if (rulesRaw && rulesRaw.length > 0) {
      try {
        f.rules = JSON.parse(rulesRaw.toString())
      } catch { /* ignore malformed */ }
    }
    return f
  }

  function flagToProto(f: Flag): ProtoFlag {
    return {
      key: f.key,
      description: f.description ?? '',
      enabled: f.enabled,
      variants_json: f.variants ? Buffer.from(JSON.stringify(f.variants)) : Buffer.alloc(0),
      rules_json: f.rules ? Buffer.from(JSON.stringify(f.rules)) : Buffer.alloc(0),
    }
  }

  // FlagManager
  async function createFlag(flag: Flag): Promise<Flag> {
    const res = await call<object, { flag: ProtoFlag }>('CreateFlag', { flag: flagToProto(flag) })
    return protoToFlag(res.flag)
  }

  async function getFlag(key: string): Promise<Flag> {
    const res = await call<object, { flag: ProtoFlag }>('GetFlag', { key })
    return protoToFlag(res.flag)
  }

  async function listFlags(): Promise<Flag[]> {
    const res = await call<object, { flags: ProtoFlag[] }>('ListFlags', {})
    return (res.flags ?? []).map(protoToFlag)
  }

  async function updateFlag(flag: Flag): Promise<Flag> {
    const res = await call<object, { flag: ProtoFlag }>('UpdateFlag', { flag: flagToProto(flag) })
    return protoToFlag(res.flag)
  }

  async function deleteFlag(key: string): Promise<void> {
    await call('DeleteFlag', { key })
  }

  // Evaluator
  async function evaluate(key: string, ctx: EvaluationContext, defaultValue: boolean): Promise<boolean> {
    const res = await call<object, { value: boolean }>('ResolveBoolean', {
      key,
      context_json: Buffer.from(JSON.stringify(ctx)),
      default_value: defaultValue,
    })
    return res.value
  }

  async function evaluateBatch(requests: EvaluateRequest[]): Promise<EvaluateResult[]> {
    const res = await call<object, { results: Array<{ key: string; value: boolean }> }>('ResolveBatch', {
      requests: requests.map(r => ({
        key: r.key,
        context_json: Buffer.from(JSON.stringify(r.context ?? {})),
        default_value: r.defaultValue,
      })),
    })
    return (res.results ?? []).map(r => ({ key: r.key, value: r.value }))
  }

  // Streamer
  async function* stream(lastEventId?: string): AsyncIterable<FlagEvent> {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const grpcStream = (stub as any).WatchFlag(
      { last_event_id: lastEventId ?? '0' },
      authMeta(),
    ) as import('@grpc/grpc-js').ClientReadableStream<{
      type: string
      key: string
      flag: ProtoFlag
      event_id: string
    }>

    for await (const ev of grpcStream) {
      const fe: FlagEvent = {
        type: ev.type === 'FLAG_UPDATED' ? 'update' : ev.type === 'FLAG_DELETED' ? 'delete' : 'error',
        key: ev.key,
        eventId: ev.event_id,
      }
      if (ev.flag) {
        fe.flag = protoToFlag(ev.flag)
      }
      yield fe
    }
  }

  return { createFlag, getFlag, listFlags, updateFlag, deleteFlag, evaluate, evaluateBatch, stream }
}
