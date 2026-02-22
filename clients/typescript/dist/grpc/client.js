/**
 * Creates a gRPC client for the flagz feature flag service.
 *
 * Requires `@grpc/grpc-js` and `@grpc/proto-loader` to be installed:
 *   npm install @grpc/grpc-js @grpc/proto-loader
 */
export async function createGRPCClient(config) {
    // Lazy-load gRPC deps — they are optional peer dependencies.
    const [grpc, protoLoader] = await Promise.all([
        import('@grpc/grpc-js').catch(() => {
            throw new Error('flagz: gRPC support requires: npm install @grpc/grpc-js @grpc/proto-loader');
        }),
        import('@grpc/proto-loader').catch(() => {
            throw new Error('flagz: gRPC support requires: npm install @grpc/grpc-js @grpc/proto-loader');
        }),
    ]);
    // Resolve the bundled proto file path relative to this module.
    const { fileURLToPath } = await import('url');
    const { dirname, join } = await import('path');
    const __filename = fileURLToPath(import.meta.url);
    const __dirname = dirname(__filename);
    // Walk up from dist/grpc/ to find the proto directory.
    const protoPath = join(__dirname, '..', '..', 'proto', 'flag_service.proto');
    const pkgDef = protoLoader.loadSync(protoPath, {
        keepCase: true,
        longs: String, // int64 → string to avoid precision loss
        enums: String,
        defaults: true,
        oneofs: true,
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const proto = grpc.loadPackageDefinition(pkgDef);
    const FlagService = proto.flagz.v1.FlagService;
    const creds = config.credentials ?? grpc.credentials.createInsecure();
    const stub = new FlagService(config.address, creds);
    /** Returns gRPC call metadata with bearer token. */
    function authMeta() {
        const meta = new grpc.Metadata();
        meta.set('authorization', `Bearer ${config.apiKey}`);
        return meta;
    }
    /** Wraps a gRPC unary call in a Promise. */
    function call(method, req) {
        return new Promise((resolve, reject) => {
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            ;
            stub[method](req, authMeta(), (err, res) => {
                if (err)
                    reject(err);
                else
                    resolve(res);
            });
        });
    }
    function protoToFlag(p) {
        const f = {
            key: p.key,
            description: p.description || undefined,
            enabled: p.enabled,
        };
        const variantsRaw = p.variants_json;
        if (variantsRaw && variantsRaw.length > 0) {
            try {
                f.variants = JSON.parse(variantsRaw.toString());
            }
            catch { /* ignore malformed */ }
        }
        const rulesRaw = p.rules_json;
        if (rulesRaw && rulesRaw.length > 0) {
            try {
                f.rules = JSON.parse(rulesRaw.toString());
            }
            catch { /* ignore malformed */ }
        }
        return f;
    }
    function flagToProto(f) {
        return {
            key: f.key,
            description: f.description ?? '',
            enabled: f.enabled,
            variants_json: f.variants ? Buffer.from(JSON.stringify(f.variants)) : Buffer.alloc(0),
            rules_json: f.rules ? Buffer.from(JSON.stringify(f.rules)) : Buffer.alloc(0),
        };
    }
    // FlagManager
    async function createFlag(flag) {
        const res = await call('CreateFlag', { flag: flagToProto(flag) });
        return protoToFlag(res.flag);
    }
    async function getFlag(key) {
        const res = await call('GetFlag', { key });
        return protoToFlag(res.flag);
    }
    async function listFlags() {
        const res = await call('ListFlags', {});
        return (res.flags ?? []).map(protoToFlag);
    }
    async function updateFlag(flag) {
        const res = await call('UpdateFlag', { flag: flagToProto(flag) });
        return protoToFlag(res.flag);
    }
    async function deleteFlag(key) {
        await call('DeleteFlag', { key });
    }
    // Evaluator
    async function evaluate(key, ctx, defaultValue) {
        const res = await call('ResolveBoolean', {
            key,
            context_json: Buffer.from(JSON.stringify(ctx)),
            default_value: defaultValue,
        });
        return res.value;
    }
    async function evaluateBatch(requests) {
        const res = await call('ResolveBatch', {
            requests: requests.map(r => ({
                key: r.key,
                context_json: Buffer.from(JSON.stringify(r.context ?? {})),
                default_value: r.defaultValue,
            })),
        });
        return (res.results ?? []).map(r => ({ key: r.key, value: r.value }));
    }
    // Streamer
    async function* stream(lastEventId) {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const grpcStream = stub.WatchFlag({ last_event_id: lastEventId ?? '0' }, authMeta());
        for await (const ev of grpcStream) {
            const fe = {
                type: ev.type === 'FLAG_UPDATED' ? 'update' : ev.type === 'FLAG_DELETED' ? 'delete' : 'error',
                key: ev.key,
                eventId: ev.event_id,
            };
            if (ev.flag) {
                fe.flag = protoToFlag(ev.flag);
            }
            yield fe;
        }
    }
    return { createFlag, getFlag, listFlags, updateFlag, deleteFlag, evaluate, evaluateBatch, stream };
}
//# sourceMappingURL=client.js.map