import {
  isCoreResponse,
  type CoreErrorCode,
  type CoreRequest,
  type CoreResponse,
  type PingResult,
  type VersionResult,
} from './protocol';

/** Anything that behaves like a `Worker` for the client's purposes. */
export type WorkerLike = Pick<Worker, 'postMessage' | 'terminate' | 'addEventListener'>;

export type CoreClientOptions = {
  /** Injected in tests; defaults to the bundled module worker. */
  createWorker?: () => WorkerLike;
  /** Per-call timeout. */
  timeoutMs?: number;
};

export class CoreError extends Error {
  readonly code: CoreErrorCode;

  constructor(code: CoreErrorCode, message: string) {
    super(message);
    this.name = 'CoreError';
    this.code = code;
  }
}

type Pending = {
  resolve: (value: unknown) => void;
  reject: (reason: CoreError) => void;
  timer: ReturnType<typeof setTimeout>;
};

const DEFAULT_TIMEOUT_MS = 30_000;

function defaultWorker(): WorkerLike {
  return new Worker(new URL('./worker.ts', import.meta.url), {
    type: 'module',
    name: 'gintrack-core',
  });
}

/**
 * Typed RPC client for the WASM core worker.
 *
 * The worker is spawned lazily on the first call, every request gets an id and
 * a timeout, and a worker crash rejects everything in flight so callers never
 * hang.
 */
export class CoreClient {
  readonly #createWorker: () => WorkerLike;
  readonly #timeoutMs: number;
  readonly #pending = new Map<number, Pending>();
  #worker: WorkerLike | null = null;
  #nextId = 1;

  constructor(options: CoreClientOptions = {}) {
    this.#createWorker = options.createWorker ?? defaultWorker;
    this.#timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  }

  /** Sends one request and resolves with its typed result. */
  call<T>(method: string, params?: unknown): Promise<T> {
    const worker = this.#ensureWorker();
    const id = this.#nextId++;

    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#pending.delete(id);
        reject(
          new CoreError('timeout', `core call "${method}" timed out after ${this.#timeoutMs}ms`),
        );
      }, this.#timeoutMs);

      this.#pending.set(id, {
        resolve: resolve as (value: unknown) => void,
        reject,
        timer,
      });

      const request: CoreRequest = params === undefined ? { id, method } : { id, method, params };
      worker.postMessage(request);
    });
  }

  ping(): Promise<PingResult> {
    return this.call<PingResult>('ping');
  }

  version(): Promise<VersionResult> {
    return this.call<VersionResult>('version');
  }

  /** Terminates the worker and rejects everything still in flight. */
  dispose(): void {
    this.#rejectAll(new CoreError('worker_crashed', 'core worker was disposed'));
    this.#worker?.terminate();
    this.#worker = null;
  }

  #ensureWorker(): WorkerLike {
    if (this.#worker) return this.#worker;

    const worker = this.#createWorker();
    worker.addEventListener('message', (event) => {
      this.#onMessage(event.data);
    });
    worker.addEventListener('error', () => {
      this.#rejectAll(new CoreError('worker_crashed', 'core worker crashed'));
      this.#worker = null;
    });
    this.#worker = worker;
    return worker;
  }

  #onMessage(data: unknown): void {
    if (!isCoreResponse(data)) return;
    const response: CoreResponse = data;

    const pending = this.#pending.get(response.id);
    if (!pending) return;
    this.#pending.delete(response.id);
    clearTimeout(pending.timer);

    if (response.ok) {
      pending.resolve(response.result);
    } else {
      pending.reject(new CoreError(response.error.code, response.error.message));
    }
  }

  #rejectAll(error: CoreError): void {
    for (const pending of this.#pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.#pending.clear();
  }
}

/** The app-wide client. Feature code never touches it: providers do. */
export const coreClient = new CoreClient();
