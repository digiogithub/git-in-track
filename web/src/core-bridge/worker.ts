/**
 * WASM core worker.
 *
 * Runs the Go core (`web/public/core.wasm`, built by `make wasm`) off the main
 * thread. Both `wasm_exec.js` and `core.wasm` are build artifacts and are not
 * present in a clean checkout, so loading is lazy and failure is not fatal:
 * `ping` simply reports `wasm: false` and core-backed methods answer with
 * `core_unavailable`.
 */
import {
  CORE_PROTOCOL_VERSION,
  type CoreErrorCode,
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

function fail(id: number, code: CoreErrorCode, message: string): void {
  respond({ id, ok: false, error: { code, message } });
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
        core: loaded?.version?.() ?? null,
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
      try {
        const raw = loaded.call(request.method, JSON.stringify(request.params ?? null));
        respond({ id: request.id, ok: true, result: JSON.parse(raw) as unknown });
      } catch (error) {
        fail(request.id, 'internal', error instanceof Error ? error.message : String(error));
      }
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
