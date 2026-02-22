import type { FlagClient } from '../types.js';
/** Configuration for the gRPC client. */
export interface GRPCConfig {
    /** Host and port of the flagz gRPC server, e.g. "localhost:9090" */
    address: string;
    /** Bearer token in "id.secret" format */
    apiKey: string;
    /**
     * Optional channel credentials.
     * Defaults to insecure credentials (suitable for localhost / plain-text).
     * Provide TLS credentials for production.
     */
    credentials?: import('@grpc/grpc-js').ChannelCredentials;
}
/**
 * Creates a gRPC client for the flagz feature flag service.
 *
 * Requires `@grpc/grpc-js` and `@grpc/proto-loader` to be installed:
 *   npm install @grpc/grpc-js @grpc/proto-loader
 */
export declare function createGRPCClient(config: GRPCConfig): Promise<FlagClient>;
//# sourceMappingURL=client.d.ts.map