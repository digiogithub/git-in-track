/**
 * Typed RPC client for the WASM core worker.
 *
 * `call` is the whole surface: it is typed against the `CoreApi` method map of
 * `./api.ts`, so a wrong method name, wrong params or a mistyped result is a
 * compile error rather than a runtime surprise. The convenience methods below
 * are thin, named wrappers over it.
 */
import type {
  Comment,
  CoreApi,
  CoreMethodName,
  CoreParams,
  CoreResult,
  Diagnostic,
  FileEvent,
  IndexStats,
  Item,
  ItemDraft,
  ItemFilter,
  ItemPage,
  ItemPatch,
  KbNode,
  KbPage,
  ProjectSummary,
  SearchHit,
  SnapshotBlob,
  VaultFile,
} from './api';
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
  /**
   * Maximum number of bytes of file text pushed in a single message. A vault
   * bigger than this is loaded in batches so that one structured clone never
   * has to carry the whole repository (docs/05 §6.4, "Batching").
   */
  chunkBytes?: number;
  /** Maximum number of files pushed in a single message. */
  chunkFiles?: number;
};

export class CoreError extends Error {
  readonly code: CoreErrorCode;
  /** Vault-relative path the failure is about, when it is about one file. */
  readonly path: string | undefined;

  constructor(code: CoreErrorCode, message: string, path?: string) {
    super(message);
    this.name = 'CoreError';
    this.code = code;
    this.path = path;
  }
}

type Pending = {
  resolve: (value: unknown) => void;
  reject: (reason: CoreError) => void;
  timer: ReturnType<typeof setTimeout>;
};

const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_CHUNK_BYTES = 4 * 1024 * 1024;
const DEFAULT_CHUNK_FILES = 256;

/**
 * Methods that can be called without arguments: their params are `undefined`,
 * or optional because the only field they carry names a repository of the
 * workspace and omitting it means "the default one".
 */
type NoParamMethod = {
  [M in CoreMethodName]: undefined extends CoreApi[M]['params'] ? M : never;
}[CoreMethodName];

function defaultWorker(): WorkerLike {
  return new Worker(new URL('./worker.ts', import.meta.url), {
    type: 'module',
    name: 'gintrack-core',
  });
}

/** Progress of a batched `loadVault`, reported after every message. */
export type LoadProgress = {
  files: number;
  totalFiles: number;
  bytes: number;
  totalBytes: number;
};

export type LoadVaultOptions = {
  rootLabel?: string;
  onProgress?: (progress: LoadProgress) => void;
  /** Repository inside the workspace; omit for the default one. */
  vaultId?: string;
  /**
   * Documentation folders this repository declares. Discovery probes the root
   * and its first-level directories on its own; a folder deeper than that is
   * found only because it is listed here (ADR-018).
   */
  docsFolders?: string[];
};

/**
 * Client for the WASM core worker.
 *
 * The worker is spawned lazily on the first call, every request gets an id and
 * a timeout, and a worker crash rejects everything in flight so callers never
 * hang.
 */
export class CoreClient {
  readonly #createWorker: () => WorkerLike;
  readonly #timeoutMs: number;
  readonly #chunkBytes: number;
  readonly #chunkFiles: number;
  readonly #pending = new Map<number, Pending>();
  #worker: WorkerLike | null = null;
  #nextId = 1;

  constructor(options: CoreClientOptions = {}) {
    this.#createWorker = options.createWorker ?? defaultWorker;
    this.#timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.#chunkBytes = options.chunkBytes ?? DEFAULT_CHUNK_BYTES;
    this.#chunkFiles = options.chunkFiles ?? DEFAULT_CHUNK_FILES;
  }

  /** Sends one request and resolves with the result the contract declares. */
  call<M extends NoParamMethod>(method: M): Promise<CoreResult<M>>;
  call<M extends CoreMethodName>(method: M, params: CoreParams<M>): Promise<CoreResult<M>>;
  call<M extends CoreMethodName>(method: M, params?: CoreParams<M>): Promise<CoreResult<M>> {
    const worker = this.#ensureWorker();
    const id = this.#nextId++;

    return new Promise<CoreResult<M>>((resolve, reject) => {
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

  // ------------------------------------------------------------ lifecycle --

  ping(): Promise<PingResult> {
    return this.call('ping');
  }

  version(): Promise<VersionResult> {
    return this.call('version');
  }

  // ----------------------------------------------------------------- vault --

  /**
   * Replaces the in-memory vault with these files.
   *
   * The texts cross as one structured clone per message. A vault larger than
   * `chunkBytes` is split: the first message carries every `project.yaml` — the
   * core cannot classify a file before it knows where the projects are — and the
   * rest arrive as `vault.apply` batches of `create` events.
   */
  async loadVault(files: VaultFile[], options: LoadVaultOptions = {}): Promise<IndexStats> {
    const batches = chunkFiles(files, this.#chunkBytes, this.#chunkFiles);
    const totalFiles = files.length;
    const totalBytes = files.reduce((sum, file) => sum + file.text.length, 0);
    let sentFiles = 0;
    let sentBytes = 0;

    const report = (batch: VaultFile[]): void => {
      sentFiles += batch.length;
      sentBytes += batch.reduce((sum, file) => sum + file.text.length, 0);
      options.onProgress?.({ files: sentFiles, totalFiles, bytes: sentBytes, totalBytes });
    };

    const target = options.vaultId === undefined ? {} : { vaultId: options.vaultId };
    const declared =
      options.docsFolders === undefined || options.docsFolders.length === 0
        ? {}
        : { docsFolders: options.docsFolders };
    const head = batches[0] ?? [];
    const loadParams =
      options.rootLabel === undefined
        ? { files: head, ...declared, ...target }
        : { files: head, rootLabel: options.rootLabel, ...declared, ...target };
    let stats = await this.call('vault.load', loadParams);
    report(head);

    for (const batch of batches.slice(1)) {
      const events: FileEvent[] = batch.map((file) => ({
        op: 'create',
        path: file.path,
        text: file.text,
      }));
      stats = await this.call('vault.apply', { events, ...target });
      report(batch);
    }
    return stats;
  }

  applyEvents(events: FileEvent[], vaultId?: string): Promise<IndexStats> {
    return this.call('vault.apply', { events, ...(vaultId === undefined ? {} : { vaultId }) });
  }

  stats(vaultId?: string): Promise<IndexStats> {
    return this.call('vault.stats', vaultId === undefined ? undefined : { vaultId });
  }

  exportSnapshot(vaultId?: string): Promise<SnapshotBlob> {
    return this.call('snapshot.export', vaultId === undefined ? undefined : { vaultId });
  }

  loadSnapshot(blob: SnapshotBlob, vaultId?: string): Promise<IndexStats> {
    return this.call('snapshot.load', vaultId === undefined ? blob : { ...blob, vaultId });
  }

  // -------------------------------------------------------------- queries --

  listProjects(): Promise<ProjectSummary[]> {
    return this.call('project.list');
  }

  listItems(filter: ItemFilter = {}): Promise<ItemPage> {
    return this.call('item.list', filter);
  }

  getItem(id: string): Promise<Item> {
    return this.call('item.get', { id });
  }

  listChildren(id: string): Promise<Item[]> {
    return this.call('item.children', { id });
  }

  listComments(id: string): Promise<Comment[]> {
    return this.call('comment.list', { id });
  }

  kbTree(project?: string): Promise<KbNode[]> {
    return this.call('kb.tree', project === undefined ? {} : { project });
  }

  kbPage(path: string): Promise<KbPage> {
    return this.call('kb.page', { path });
  }

  search(q: string, options: { limit?: number; project?: string } = {}): Promise<SearchHit[]> {
    return this.call('search', { q, ...options });
  }

  validateItem(params: CoreParams<'item.validate'>): Promise<Diagnostic[]> {
    return this.call('item.validate', params);
  }

  parseItem(path: string, text: string): Promise<Item> {
    return this.call('item.parse', { path, text });
  }

  serializeItem(item: Item): Promise<{ text: string }> {
    return this.call('item.serialize', { item });
  }

  // ------------------------------------------------------------- mutations --

  createItem(draft: ItemDraft): Promise<CoreResult<'item.create'>> {
    return this.call('item.create', draft);
  }

  updateItem(id: string, patch: ItemPatch, rev: string): Promise<CoreResult<'item.update'>> {
    return this.call('item.update', { id, patch, rev });
  }

  moveItem(id: string, status: string, rev: string): Promise<CoreResult<'item.move'>> {
    return this.call('item.move', { id, status, rev });
  }

  deleteItem(id: string, rev: string, hard = false): Promise<CoreResult<'item.delete'>> {
    return this.call('item.delete', { id, rev, hard });
  }

  addComment(params: CoreParams<'comment.add'>): Promise<CoreResult<'comment.add'>> {
    return this.call('comment.add', params);
  }

  writeKbPage(path: string, text: string, rev?: string): Promise<CoreResult<'kb.write'>> {
    return this.call('kb.write', rev === undefined ? { path, text } : { path, text, rev });
  }

  // ------------------------------------------------------------- lifecycle --

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
      pending.reject(
        new CoreError(response.error.code, response.error.message, response.error.path),
      );
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

/**
 * Splits a vault into messages of at most `maxBytes` of text and `maxFiles`
 * files.
 *
 * Project descriptors go into the first batch whatever their size: project
 * discovery runs on the first message, and a file that belongs to no known
 * project would otherwise be indexed as a stray.
 */
export function chunkFiles(
  files: VaultFile[],
  maxBytes = DEFAULT_CHUNK_BYTES,
  maxFiles = DEFAULT_CHUNK_FILES,
): VaultFile[][] {
  if (files.length === 0) return [[]];

  const descriptors = files.filter((file) => isProjectDescriptor(file.path));
  const rest = files.filter((file) => !isProjectDescriptor(file.path));

  let current: VaultFile[] = descriptors;
  const batches: VaultFile[][] = [current];
  let bytes = descriptors.reduce((sum, file) => sum + file.text.length, 0);

  for (const file of rest) {
    const size = file.text.length;
    if (current.length > 0 && (bytes + size > maxBytes || current.length >= maxFiles)) {
      current = [];
      batches.push(current);
      bytes = 0;
    }
    current.push(file);
    bytes += size;
  }
  return batches;
}

/** Reports whether a path is a `.pmngr/project.yaml`. */
function isProjectDescriptor(path: string): boolean {
  return path.endsWith('/.pmngr/project.yaml') || path === '.pmngr/project.yaml';
}

/** The app-wide client. Feature code never touches it: providers do. */
export const coreClient = new CoreClient();
