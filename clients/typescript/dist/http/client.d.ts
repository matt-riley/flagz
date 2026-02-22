import type { FlagClient } from '../types.js';
/** Configuration for the HTTP client. */
export interface HTTPConfig {
    /** Base URL of the flagz server, e.g. "http://localhost:8080" */
    baseURL: string;
    /** Bearer token in "id.secret" format */
    apiKey: string;
    /**
     * Optional fetch implementation. Defaults to globalThis.fetch.
     * Inject a mock for testing or a custom implementation for edge runtimes.
     */
    fetch?: typeof globalThis.fetch;
}
/** An HTTP error returned by the flagz server. */
export declare class HTTPError extends Error {
    readonly status: number;
    constructor(status: number, message: string);
}
/** Creates an HTTP client for the flagz feature flag service. */
export declare function createHTTPClient(config: HTTPConfig): FlagClient;
//# sourceMappingURL=client.d.ts.map