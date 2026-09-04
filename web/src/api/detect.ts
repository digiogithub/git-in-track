import type { Unsubscribe } from '@/api/provider';
import { useAppStore, type AppMode } from '@/app/store';

/** Default companion address, as documented in docs/07-cli-and-api.md. */
export const COMPANION_ORIGIN = 'http://127.0.0.1:7317';

/** Unauthenticated health route used for detection. */
export const HEALTH_PATH = '/api/v1/health';

/** The probe must not delay boot: 1.5 s and then browser-only mode. */
export const PROBE_TIMEOUT_MS = 1500;

/**
 * How often a running tab re-probes, so starting (or stopping) `gintrack serve`
 * flips the mode without a reload (docs/05-web-app.md §4.3 step 5).
 */
export const REPROBE_INTERVAL_MS = 30_000;

/** Port the Vite dev server listens on (`vite.config.ts`). */
const DEV_SERVER_PORT = '5173';

/**
 * Where the companion API lives for this page.
 *
 * - `VITE_COMPANION_URL` wins, for a companion on a non-default port.
 * - Same-origin (`''`) when the companion serves the bundle itself: no CORS
 *   preflight, no port to guess, and it keeps working behind `--bind`.
 * - The documented loopback origin from the Vite dev server, or from any other
 *   static host where a same-origin `/api/v1` cannot exist.
 */
export function resolveCompanionBaseUrl(): string {
  const configured = import.meta.env.VITE_COMPANION_URL;
  if (configured !== undefined && configured !== '') return configured.replace(/\/+$/, '');

  const location = globalThis.location as Location | undefined;
  if (!location) return COMPANION_ORIGIN;
  if (import.meta.env.DEV || location.port === DEV_SERVER_PORT) return COMPANION_ORIGIN;
  return '';
}

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
  return (await detectCompanion(options)).mode;
}

/** What one probe found, including where the companion answered. */
export type CompanionStatus = {
  mode: AppMode;
  version: string | null;
  baseUrl: string;
};

/** Runs one probe and writes the result into the store. */
export async function detectCompanion(options: ProbeOptions = {}): Promise<CompanionStatus> {
  const { setMode, setCompanionUrl } = useAppStore.getState();
  const forced = import.meta.env.VITE_FORCE_PROVIDER;
  const baseUrl = options.baseUrl ?? resolveCompanionBaseUrl();

  if (forced === 'browser' || forced === 'companion') {
    setMode(forced, null);
    setCompanionUrl(forced === 'companion' ? baseUrl : null);
    return { mode: forced, version: null, baseUrl };
  }

  const health = await probeCompanion({ ...options, baseUrl });

  if (health) {
    setMode('companion', health.version);
    setCompanionUrl(baseUrl);
    return { mode: 'companion', version: health.version, baseUrl };
  }

  setMode('browser', null);
  setCompanionUrl(null);
  return { mode: 'browser', version: null, baseUrl };
}

export type WatchCompanionOptions = ProbeOptions & {
  intervalMs?: number;
  /** Called only when the detected mode actually flips. */
  onChange?: (status: CompanionStatus) => void;
};

/**
 * Keeps probing after boot so a companion started (or stopped) mid-session is
 * noticed within one interval, without a reload. The probe is one unauthenticated
 * `GET /health` with a short timeout, and it is cancelled with the returned
 * function; a tick is skipped while the previous one is still in flight.
 *
 * The tab also re-probes as soon as it becomes visible again, which is what
 * makes the upgrade feel immediate after starting `gintrack serve`.
 */
const probeNowHooks = new Set<() => Promise<void>>();

/**
 * Runs the detection cycle immediately — what the "Check again" button in
 * Settings calls. It drives the active watcher when there is one, so a flip
 * rebuilds the provider through the same path as a scheduled probe.
 */
export async function probeCompanionNow(): Promise<void> {
  if (probeNowHooks.size === 0) {
    await detectCompanion();
    return;
  }
  await Promise.all([...probeNowHooks].map((run) => run()));
}

export function watchCompanion(options: WatchCompanionOptions = {}): Unsubscribe {
  const { intervalMs = REPROBE_INTERVAL_MS, onChange, ...probeOptions } = options;
  const forced = import.meta.env.VITE_FORCE_PROVIDER;
  if (forced === 'browser' || forced === 'companion') return () => undefined;

  let stopped = false;
  let inFlight = false;
  let lastRunAt = 0;

  const tick = async (): Promise<void> => {
    if (stopped || inFlight) return;
    inFlight = true;
    lastRunAt = Date.now();
    try {
      const before = useAppStore.getState().mode;
      const status = await detectCompanion(probeOptions);
      if (stopped || status.mode === before) return;
      onChange?.(status);
    } finally {
      inFlight = false;
    }
  };

  probeNowHooks.add(tick);

  const timer = setInterval(() => {
    void tick();
  }, intervalMs);

  const onVisibility = (): void => {
    // Throttled: a tab flipped back and forth must not hammer the companion.
    if (globalThis.document?.visibilityState !== 'visible') return;
    if (Date.now() - lastRunAt < intervalMs) return;
    void tick();
  };
  globalThis.document?.addEventListener('visibilitychange', onVisibility);

  return () => {
    stopped = true;
    probeNowHooks.delete(tick);
    clearInterval(timer);
    globalThis.document?.removeEventListener('visibilitychange', onVisibility);
  };
}
