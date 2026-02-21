export type {
  Flag,
  Rule,
  EvaluationContext,
  EvaluateRequest,
  EvaluateResult,
  FlagEvent,
  FlagManager,
  Evaluator,
  Streamer,
  FlagClient,
} from './types.js'

export { createHTTPClient } from './http/client.js'
export type { HTTPConfig } from './http/client.js'
// Note: createGRPCClient is exported from the './grpc' entry point only,
// to avoid statically importing gRPC types in this barrel.
