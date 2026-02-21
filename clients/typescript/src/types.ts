/**
 * Domain types for the flagz feature flag service client.
 */

/** A feature flag definition. */
export interface Flag {
  key: string
  description?: string
  enabled: boolean
  variants?: Record<string, boolean>
  rules?: Rule[]
  /** ISO 8601 timestamp; absent when using gRPC transport */
  createdAt?: string
  /** ISO 8601 timestamp; absent when using gRPC transport */
  updatedAt?: string
}

/** A targeting rule that determines flag evaluation. */
export interface Rule {
  attribute: string
  operator: 'equals' | 'in'
  value: unknown
}

/** Attribute data used when evaluating flag rules. */
export interface EvaluationContext {
  attributes?: Record<string, unknown>
}

/** A single flag evaluation request. */
export interface EvaluateRequest {
  key: string
  context?: EvaluationContext
  defaultValue: boolean
}

/** The outcome of a single flag evaluation. */
export interface EvaluateResult {
  key: string
  value: boolean
}

/** A real-time notification of a flag change from the stream. */
export interface FlagEvent {
  type: 'update' | 'delete' | 'error'
  key: string
  flag?: Flag
  /** Stringified int64 to avoid precision loss for large event IDs. */
  eventId?: string
}

/** CRUD operations on feature flags. */
export interface FlagManager {
  createFlag(flag: Flag): Promise<Flag>
  getFlag(key: string): Promise<Flag>
  listFlags(): Promise<Flag[]>
  updateFlag(flag: Flag): Promise<Flag>
  deleteFlag(key: string): Promise<void>
}

/** Flag resolution for a given evaluation context. */
export interface Evaluator {
  evaluate(key: string, ctx: EvaluationContext, defaultValue: boolean): Promise<boolean>
  evaluateBatch(requests: EvaluateRequest[]): Promise<EvaluateResult[]>
}

/** Real-time flag change event delivery. */
export interface Streamer {
  stream(lastEventId?: string): AsyncIterable<FlagEvent>
}

/** Combined client interface. */
export type FlagClient = FlagManager & Evaluator & Streamer
