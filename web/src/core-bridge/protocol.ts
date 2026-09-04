/**
 * Message protocol between the main thread and the WASM core worker.
 *
 * Every request carries an id; every response echoes it. The `method` of a
 * request is one of the names declared by the `CoreApi` map in `./api.ts`, which
 * is the authoritative contract: this file only describes the envelope that
 * carries them, plus the two lifecycle methods the worker answers on its own so
 * that the app can boot before `core.wasm` has finished loading.
 */
import type { CoreMethodName, CoreResult } from './api';

export const CORE_PROTOCOL_VERSION = 1;

/** Every method the worker accepts, including the two it answers by itself. */
export type CoreMethod = CoreMethodName;

export type CoreRequest = {
  id: number;
  method: string;
  params?: unknown;
};

export type CoreErrorPayload = {
  code: CoreErrorCode;
  message: string;
  /** Vault-relative path the failure is about, when it is about one file. */
  path?: string;
};

/**
 * Stable error codes. The first group is produced by the bridge in TypeScript,
 * the second by the Go core (`wasm/bridge.go`, `errorPayload`).
 */
export type CoreErrorCode =
  | 'unknown_method'
  | 'core_unavailable'
  | 'invalid_request'
  | 'timeout'
  | 'worker_crashed'
  | 'internal'
  | 'not_found'
  | 'stale_revision'
  | 'workflow_transition_denied'
  | 'validation_failed'
  | 'invalid_front_matter'
  | 'duplicate_id'
  | 'read_only';

export type CoreResponse =
  { id: number; ok: true; result: unknown } | { id: number; ok: false; error: CoreErrorPayload };

/** The envelope `gintrackCore.call` returns, as a parsed JSON value. */
export type CoreEnvelope = { ok: true; result: unknown } | { ok: false; error: CoreErrorPayload };

export type PingResult = CoreResult<'ping'>;
export type VersionResult = CoreResult<'version'>;

export function isCoreResponse(value: unknown): value is CoreResponse {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return typeof candidate['id'] === 'number' && typeof candidate['ok'] === 'boolean';
}

/** Narrows a value parsed from the core's JSON string into an envelope. */
export function isCoreEnvelope(value: unknown): value is CoreEnvelope {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as Record<string, unknown>;
  if (candidate['ok'] === true) return true;
  return candidate['ok'] === false && typeof candidate['error'] === 'object';
}
