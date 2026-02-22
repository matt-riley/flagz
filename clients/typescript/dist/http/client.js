/** An HTTP error returned by the flagz server. */
export class HTTPError extends Error {
    status;
    constructor(status, message) {
        super(`flagz: HTTP ${status}: ${message}`);
        this.status = status;
        this.name = 'HTTPError';
    }
}
/** Creates an HTTP client for the flagz feature flag service. */
export function createHTTPClient(config) {
    const fetchFn = config.fetch ?? globalThis.fetch;
    async function request(method, path, body) {
        const headers = {
            Authorization: `Bearer ${config.apiKey}`,
        };
        if (body !== undefined) {
            headers['Content-Type'] = 'application/json';
        }
        const res = await fetchFn(`${config.baseURL}${path}`, {
            method,
            headers,
            body: body !== undefined ? JSON.stringify(body) : undefined,
        });
        if (!res.ok) {
            const text = await res.text().catch(() => '');
            throw new HTTPError(res.status, text.trim());
        }
        // 204 No Content
        if (res.status === 204)
            return undefined;
        return res.json();
    }
    function wireToFlag(w) {
        return {
            key: w.key,
            description: w.description,
            enabled: w.enabled,
            variants: w.variants,
            rules: w.rules,
            createdAt: w.created_at,
            updatedAt: w.updated_at,
        };
    }
    function flagToWire(f) {
        return {
            key: f.key,
            description: f.description,
            enabled: f.enabled,
            variants: f.variants,
            rules: f.rules,
        };
    }
    // FlagManager
    async function createFlag(flag) {
        const res = await request('POST', '/v1/flags', { flag: flagToWire(flag) });
        return wireToFlag(res.flag);
    }
    async function getFlag(key) {
        const res = await request('GET', `/v1/flags/${encodeURIComponent(key)}`);
        return wireToFlag(res.flag);
    }
    async function listFlags() {
        const res = await request('GET', '/v1/flags');
        return (res.flags ?? []).map(wireToFlag);
    }
    async function updateFlag(flag) {
        const res = await request('PUT', `/v1/flags/${encodeURIComponent(flag.key)}`, { flag: flagToWire(flag) });
        return wireToFlag(res.flag);
    }
    async function deleteFlag(key) {
        await request('DELETE', `/v1/flags/${encodeURIComponent(key)}`);
    }
    // Evaluator
    async function evaluate(key, ctx, defaultValue) {
        const res = await request('POST', '/v1/evaluate', {
            key,
            context: ctx,
            default_value: defaultValue,
        });
        return res.value;
    }
    async function evaluateBatch(requests) {
        const res = await request('POST', '/v1/evaluate', {
            requests: requests.map(r => ({
                key: r.key,
                context: r.context,
                default_value: r.defaultValue,
            })),
        });
        return (res.results ?? []).map(r => ({ key: r.key, value: r.value }));
    }
    // Streamer — SSE via fetch + ReadableStream (no extra deps, supports Authorization header)
    async function* stream(lastEventId) {
        const headers = {
            Authorization: `Bearer ${config.apiKey}`,
        };
        if (lastEventId !== undefined) {
            headers['Last-Event-ID'] = lastEventId;
        }
        const res = await fetchFn(`${config.baseURL}/v1/stream`, { headers });
        if (!res.ok) {
            const text = await res.text().catch(() => '');
            throw new HTTPError(res.status, text.trim());
        }
        if (!res.body)
            throw new Error('flagz: SSE response has no body');
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = '';
        // Per-event accumulators
        let eventType = '';
        let dataLines = [];
        let eventId;
        try {
            while (true) {
                const { done, value } = await reader.read();
                if (done)
                    break;
                buf += decoder.decode(value, { stream: true });
                const lines = buf.split('\n');
                buf = lines.pop() ?? '';
                for (const rawLine of lines) {
                    const line = rawLine.replace(/\r$/, '');
                    if (line === '') {
                        // Blank line: dispatch if we have data
                        if (dataLines.length > 0) {
                            const data = dataLines.join('\n');
                            const ev = {
                                type: eventType || 'update',
                                key: '',
                                eventId,
                            };
                            if (ev.type === 'update' || ev.type === 'delete') {
                                try {
                                    const parsed = JSON.parse(data);
                                    ev.flag = parsed;
                                    ev.key = parsed.key ?? '';
                                }
                                catch {
                                    // malformed JSON: emit error event
                                    ev.type = 'error';
                                }
                            }
                            yield ev;
                        }
                        // Reset accumulators
                        eventType = '';
                        dataLines = [];
                        eventId = undefined;
                    }
                    else if (line.startsWith('id:')) {
                        eventId = line.slice(3).trimStart();
                    }
                    else if (line.startsWith('event:')) {
                        eventType = line.slice(6).trimStart();
                    }
                    else if (line.startsWith('data:')) {
                        dataLines.push(line.slice(5).trimStart());
                    }
                    // Lines starting with ':' are SSE comments — ignored.
                }
            }
        }
        finally {
            reader.cancel().catch(() => { });
        }
    }
    return { createFlag, getFlag, listFlags, updateFlag, deleteFlag, evaluate, evaluateBatch, stream };
}
//# sourceMappingURL=client.js.map