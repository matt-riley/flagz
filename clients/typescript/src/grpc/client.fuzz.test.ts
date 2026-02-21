import { describe, it, expect, afterAll } from 'vitest'
import * as fc from 'fast-check'
import { createGRPCClient } from './client.js'

/** Port offset to avoid colliding with client.test.ts (59090). */
const TEST_PORT = 59091

// --- Arbitraries ------------------------------------------------------------

const arbKey = fc.stringMatching(/^[a-zA-Z0-9_-]{1,32}$/)
const arbVariants = fc.dictionary(arbKey, fc.boolean(), { maxKeys: 8 })
const arbRules = fc.array(
  fc.record({
    attribute: fc.string({ maxLength: 16 }),
    operator: fc.constantFrom('equals', 'in'),
    value: fc.string({ maxLength: 16 }),
  }),
  { maxLength: 4 },
)

// --- Test server setup -------------------------------------------------------

async function startFuzzServer() {
  const grpc = await import('@grpc/grpc-js')
  const protoLoader = await import('@grpc/proto-loader')
  const { fileURLToPath } = await import('url')
  const { dirname, join } = await import('path')
  const __filename = fileURLToPath(import.meta.url)
  const __dirname = dirname(__filename)
  const protoPath = join(__dirname, '..', '..', 'proto', 'flag_service.proto')

  const pkgDef = protoLoader.loadSync(protoPath, {
    keepCase: true,
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
  })
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const proto = grpc.loadPackageDefinition(pkgDef) as any
  const FlagService = proto.flagz.v1.FlagService

  const flags: Record<string, unknown> = {}

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const impl: Record<string, any> = {
    CreateFlag(call: grpc.ServerUnaryCall<{ flag: unknown }, unknown>, cb: grpc.sendUnaryData<unknown>) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const f = (call.request as any).flag
      flags[f.key] = f
      cb(null, { flag: f })
    },
    GetFlag(call: grpc.ServerUnaryCall<{ key: string }, unknown>, cb: grpc.sendUnaryData<unknown>) {
      const f = flags[call.request.key]
      if (!f) { cb({ code: grpc.status.NOT_FOUND, message: 'not found' } as grpc.ServiceError, null); return }
      cb(null, { flag: f })
    },
    UpdateFlag(call: grpc.ServerUnaryCall<{ flag: unknown }, unknown>, cb: grpc.sendUnaryData<unknown>) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const f = (call.request as any).flag
      flags[f.key] = f
      cb(null, { flag: f })
    },
    DeleteFlag(call: grpc.ServerUnaryCall<{ key: string }, unknown>, cb: grpc.sendUnaryData<unknown>) {
      delete flags[call.request.key]
      cb(null, {})
    },
    ListFlags(_call: unknown, cb: grpc.sendUnaryData<unknown>) {
      cb(null, { flags: Object.values(flags) })
    },
    ResolveBoolean(call: grpc.ServerUnaryCall<{ key: string; default_value: boolean }, unknown>, cb: grpc.sendUnaryData<unknown>) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const f = flags[call.request.key] as any
      cb(null, { key: call.request.key, value: f ? f.enabled : call.request.default_value })
    },
    ResolveBatch(call: grpc.ServerUnaryCall<{ requests: Array<{ key: string; default_value: boolean }> }, unknown>, cb: grpc.sendUnaryData<unknown>) {
      const results = call.request.requests.map(r => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const f = flags[r.key] as any
        return { key: r.key, value: f ? f.enabled : r.default_value }
      })
      cb(null, { results })
    },
    WatchFlag(call: grpc.ServerWritableStream<unknown, unknown>) { call.end() },
  }

  const server = new grpc.Server()
  server.addService(FlagService.service, impl)
  await new Promise<void>((resolve, reject) => {
    server.bindAsync(`127.0.0.1:${TEST_PORT}`, grpc.ServerCredentials.createInsecure(), (err) => {
      if (err) reject(err); else resolve()
    })
  })
  return { server, flags }
}

let serverCtx: Awaited<ReturnType<typeof startFuzzServer>> | null = null
async function getServer() {
  if (!serverCtx) serverCtx = await startFuzzServer()
  return serverCtx
}

afterAll(() => {
  serverCtx?.server.forceShutdown()
  serverCtx = null
})

// --- Property tests ---------------------------------------------------------

describe('gRPC client property tests', () => {

  // 1. Flag key and enabled are always preserved through createFlag roundtrip.
  it('key and enabled are preserved through createFlag/getFlag roundtrip', async () => {
    await fc.assert(
      fc.asyncProperty(arbKey, fc.boolean(), async (key, enabled) => {
        await getServer()
        const client = await createGRPCClient({ address: `127.0.0.1:${TEST_PORT}`, apiKey: 'k' })
        const created = await client.createFlag({ key, enabled })
        expect(created.key).toBe(key)
        expect(created.enabled).toBe(enabled)
      }),
      { numRuns: 50 },
    )
  })

  // 2. Arbitrary variants maps (string→bool) survive the encode→decode roundtrip.
  it('arbitrary variants survive gRPC proto encode/decode roundtrip', async () => {
    await fc.assert(
      fc.asyncProperty(arbKey, fc.boolean(), arbVariants, async (key, enabled, variants) => {
        await getServer()
        const client = await createGRPCClient({ address: `127.0.0.1:${TEST_PORT}`, apiKey: 'k' })
        const created = await client.createFlag({ key, enabled, variants })
        // Variants with non-empty values must roundtrip.
        if (Object.keys(variants).length > 0) {
          expect(created.variants).toEqual(variants)
        }
      }),
      { numRuns: 50 },
    )
  })

  // 3. Rules survive the encode→decode roundtrip.
  it('arbitrary rules survive gRPC proto encode/decode roundtrip', async () => {
    await fc.assert(
      fc.asyncProperty(arbKey, arbRules, async (key, rules) => {
        await getServer()
        const client = await createGRPCClient({ address: `127.0.0.1:${TEST_PORT}`, apiKey: 'k' })
        const created = await client.createFlag({ key, enabled: true, rules })
        if (rules.length > 0) {
          expect(created.rules).toHaveLength(rules.length)
          for (let i = 0; i < rules.length; i++) {
            expect(created.rules?.[i].attribute).toBe(rules[i].attribute)
            expect(created.rules?.[i].operator).toBe(rules[i].operator)
          }
        }
      }),
      { numRuns: 50 },
    )
  })

  // 4. evaluateBatch always returns same count as requests.
  it('evaluateBatch result count always equals request count', async () => {
    await fc.assert(
      fc.asyncProperty(
        fc.array(fc.record({ key: arbKey, defaultValue: fc.boolean() }), { maxLength: 10 }),
        async (requests) => {
          await getServer()
          const client = await createGRPCClient({ address: `127.0.0.1:${TEST_PORT}`, apiKey: 'k' })
          const results = await client.evaluateBatch(requests)
          expect(results).toHaveLength(requests.length)
        },
      ),
      { numRuns: 50 },
    )
  })

  // 5. evaluate returns a boolean for any key.
  it('evaluate always returns a boolean', async () => {
    await fc.assert(
      fc.asyncProperty(arbKey, fc.boolean(), async (key, defaultValue) => {
        await getServer()
        const client = await createGRPCClient({ address: `127.0.0.1:${TEST_PORT}`, apiKey: 'k' })
        const result = await client.evaluate(key, {}, defaultValue)
        expect(typeof result).toBe('boolean')
      }),
      { numRuns: 50 },
    )
  })
})
