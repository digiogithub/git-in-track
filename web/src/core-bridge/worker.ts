/**
 * WASM core worker.
 *
 * Runs the Go core (`web/public/core.wasm`, built by `make wasm`) off the main
 * thread. Both `wasm_exec.js` and `core.wasm` are build artifacts and are not
 * present in a clean checkout, so loading is lazy and failure is not fatal:
 * `ping` simply reports `wasm: false` and core-backed methods answer with
 * `core_unavailable`.
 *
 * Every method of the `CoreApi` contract other than `ping` and `version` is
 * forwarded verbatim to `globalThis.gintrackCore.call(method, paramsJSON)`,
 * whose JSON envelope is unwrapped into the worker's own response envelope.
 */
import {
  CORE_PROTOCOL_VERSION,
  isCoreEnvelope,
  type CoreErrorCode,
  type CoreErrorPayload,
  type CoreRequest,
  type CoreResponse,
  type PingResult,
  type VersionResult,
} from './protocol';

/** Minimal view of the worker global, so the app tsconfig does not need the WebWorker lib. */
type WorkerScope = {
  postMessage: (message: unknown) => void;
  addEventListener: (type: 'message', listener: (event: MessageEvent) => void) => void;
  location: { origin: string };
};

/** The Go runtime shim declared by `wasm_exec.js`. */
type GoRuntime = {
  importObject: WebAssembly.Imports;
  run: (instance: WebAssembly.Instance) => Promise<void>;
};

type GoConstructor = new () => GoRuntime;

/** The surface `wasm/main_js.go` registers on `globalThis`. */
type GintrackCore = {
  version?: () => string;
  parseItem?: (path: string, text: string) => string;
  call?: (method: string, params: string) => string;
};

type WasmGlobals = {
  Go?: GoConstructor;
  gintrackCore?: GintrackCore;
};

const ctx = self as unknown as WorkerScope;
const globals = globalThis as unknown as WasmGlobals;

const WASM_EXEC_URL = '/wasm_exec.js';
const CORE_WASM_URL = '/core.wasm';

let corePromise: Promise<GintrackCore | null> | null = null;

async function instantiate(go: GoRuntime): Promise<WebAssembly.Instance | null> {
  const response = await fetch(CORE_WASM_URL);
  if (!response.ok) return null;

  // `instantiateStreaming` needs `Content-Type: application/wasm`; static hosts
  // that get it wrong fall back to the buffer form.
  if (typeof WebAssembly.instantiateStreaming === 'function') {
    try {
      const streamed = await WebAssembly.instantiateStreaming(response.clone(), go.importObject);
      return streamed.instance;
    } catch {
      // fall through to the buffer path
    }
  }
  const bytes = await response.arrayBuffer();
  const instantiated = await WebAssembly.instantiate(bytes, go.importObject);
  return instantiated.instance;
}

/** Loads the Go core once. Resolves with `null` when the artifacts are absent. */
async function loadCore(): Promise<GintrackCore | null> {
  try {
    if (!globals.Go) {
      // `wasm_exec.js` is a classic script that assigns `globalThis.Go`; it is
      // side-effect only, hence the ignored dynamic import.
      await import(/* @vite-ignore */ new URL(WASM_EXEC_URL, ctx.location.origin).href);
    }
    const Go = globals.Go;
    if (!Go) return null;

    const go = new Go();
    const instance = await instantiate(go);
    if (!instance) return null;

    // `go.run` resolves when the Go program exits; the core keeps running, so
    // the promise is deliberately not awaited.
    void go.run(instance);
    return globals.gintrackCore ?? null;
  } catch {
    return null;
  }
}

function core(): Promise<GintrackCore | null> {
  corePromise ??= loadCore();
  return corePromise;
}

function respond(response: CoreResponse): void {
  ctx.postMessage(response);
}

function fail(id: number, code: CoreErrorCode, message: string, path?: string): void {
  const error: CoreErrorPayload = path === undefined ? { code, message } : { code, message, path };
  respond({ id, ok: false, error });
}

/**
 * Reads the build version out of `gintrackCore.version()`, which answers with
 * its own `{ ok, data }` envelope rather than a bare string.
 */
function coreVersion(loaded: GintrackCore | null): string | null {
  const raw = loaded?.version?.();
  if (typeof raw !== 'string') return null;
  try {
    const parsed = JSON.parse(raw) as { ok?: boolean; data?: { version?: unknown } };
    const value = parsed.ok === true ? parsed.data?.version : undefined;
    return typeof value === 'string' ? value : raw;
  } catch {
    return raw;
  }
}

/** Forwards one method to the Go core and unwraps its envelope. */
function forward(request: CoreRequest, call: (method: string, params: string) => string): void {
  let raw: string;
  try {
    raw = call(request.method, JSON.stringify(request.params ?? null));
  } catch (error) {
    fail(request.id, 'internal', error instanceof Error ? error.message : String(error));
    return;
  }

  let envelope: unknown;
  try {
    envelope = JSON.parse(raw);
  } catch {
    fail(request.id, 'internal', `the core returned invalid JSON for "${request.method}"`);
    return;
  }
  if (!isCoreEnvelope(envelope)) {
    fail(request.id, 'internal', `the core returned an unknown envelope for "${request.method}"`);
    return;
  }
  if (envelope.ok) {
    respond({ id: request.id, ok: true, result: envelope.result });
    return;
  }
  respond({ id: request.id, ok: false, error: envelope.error });
}

async function handle(request: CoreRequest): Promise<void> {
  const loaded = await core();

  switch (request.method) {
    case 'ping': {
      const result: PingResult = { pong: true, wasm: loaded !== null };
      respond({ id: request.id, ok: true, result });
      return;
    }
    case 'version': {
      const result: VersionResult = {
        protocol: CORE_PROTOCOL_VERSION,
        core: coreVersion(loaded),
      };
      respond({ id: request.id, ok: true, result });
      return;
    }
    default: {
      if (!loaded?.call) {
        fail(
          request.id,
          'core_unavailable',
          `core.wasm is not loaded, cannot handle "${request.method}"`,
        );
        return;
      }
      forward(request, loaded.call);
    }
  }
}

ctx.addEventListener('message', (event: MessageEvent) => {
  const request = event.data as CoreRequest | undefined;
  if (!request || typeof request.id !== 'number' || typeof request.method !== 'string') {
    return;
  }
  void handle(request);
});
