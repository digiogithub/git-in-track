/**
 * Message protocol between the main thread and the WASM core worker.
 *
 * Every request carries an id; every response echoes it. Phase 0 ships the two
 * lifecycle methods (`ping`, `version`); the index/query/parse/serialise
 * operations of docs/05-web-app.md §6.4 are added in Phase 1 as extra `method`
 * values, without changing the envelope.
 */

export const CORE_PROTOCOL_VERSION = 1;

export type CoreMethod = 'ping' | 'version';

export type CoreRequest = {
  id: number;
  method: string;
  params?: unknown;
};

export type CoreErrorPayload = {
  code: CoreErrorCode;
  message: string;
  path?: string;
};

export type CoreErrorCode =
  | 'unknown_method'
  | 'core_unavailable'
  | 'invalid_request'
  | 'timeout'
  | 'worker_crashed'
  | 'internal';

export type CoreResponse =
  { id: number; ok: true; result: unknown } | { id: number; ok: false; error: CoreErrorPayload };

export type PingResult = {
  pong: true;
  /** Whether `core.wasm` was found and instantiated. */
  wasm: boolean;
};

export type VersionResult = {
  protocol: number;
  /** Version string exported by the Go core, or `null` when it is not loaded. */
  core: string | null;
};

export function isCoreResponse(value: unknown): value is CoreResponse {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return typeof candidate['id'] === 'number' && typeof candidate['ok'] === 'boolean';
}
