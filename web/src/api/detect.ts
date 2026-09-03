import { useAppStore, type AppMode } from '@/app/store';

/** Default companion address, as documented in docs/07-cli-and-api.md. */
export const COMPANION_ORIGIN = 'http://127.0.0.1:7317';

/** Unauthenticated health route used for detection. */
export const HEALTH_PATH = '/api/v1/health';

/** The probe must not delay boot: 1.5 s and then browser-only mode. */
export const PROBE_TIMEOUT_MS = 1500;

export type HealthResponse = {
  status: string;
  version: string;
  uptimeSeconds: number;
};

export type ProbeOptions = {
  /** Base URL of the companion. Defaults to same-origin when the app is served by it. */
  baseUrl?: string;
  timeoutMs?: number;
  /** Injected for tests. */
  fetchImpl?: typeof fetch;
};

function isHealthResponse(value: unknown): value is HealthResponse {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate['status'] === 'string' &&
    typeof candidate['version'] === 'string' &&
    typeof candidate['uptimeSeconds'] === 'number'
  );
}

/**
 * `GET /api/v1/health` with a hard timeout.
 *
 * Resolves with the parsed health document when a companion answers, or `null`
 * for every failure mode (timeout, network error, CORS refusal, non-2xx, or a
 * body that is not a health document). It never throws: detection must not be
 * able to break boot.
 */
export async function probeCompanion(options: ProbeOptions = {}): Promise<HealthResponse | null> {
  const { baseUrl = '', timeoutMs = PROBE_TIMEOUT_MS, fetchImpl = globalThis.fetch } = options;
  if (typeof fetchImpl !== 'function') return null;

  const controller = new AbortController();
  const timer = setTimeout(() => {
    controller.abort();
  }, timeoutMs);

  try {
    const response = await fetchImpl(`${baseUrl}${HEALTH_PATH}`, {
      method: 'GET',
      mode: 'cors',
      credentials: 'omit',
      headers: { Accept: 'application/json' },
      signal: controller.signal,
    });
    if (!response.ok) return null;

    const body: unknown = await response.json();
    if (!isHealthResponse(body) || body.status !== 'ok') return null;
    return body;
  } catch {
    // Timeout, DNS failure, connection refused or a CORS refusal: no companion.
    return null;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Decide which runtime the app is talking to and write the result into the
 * store. `VITE_FORCE_PROVIDER` short-circuits the probe during development.
 */
export async function detectMode(options: ProbeOptions = {}): Promise<AppMode> {
  const { setMode } = useAppStore.getState();
  const forced = import.meta.env.VITE_FORCE_PROVIDER;

  if (forced === 'browser' || forced === 'companion') {
    setMode(forced, null);
    return forced;
  }

  const baseUrl = options.baseUrl ?? import.meta.env.VITE_COMPANION_URL ?? '';
  const health = await probeCompanion({ ...options, baseUrl });

  if (health) {
    setMode('companion', health.version);
    return 'companion';
  }

  setMode('browser', null);
  return 'browser';
}
