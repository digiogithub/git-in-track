/**
 * `DataProvider` for browser-only mode (docs/05-web-app.md §4.1).
 *
 * It composes two things and owns the plumbing between them:
 *
 * - a `VaultFS` (File System Access handles, or the read-only
 *   `webkitdirectory` fallback), which reads the folder and persists writes;
 * - the WASM core worker (`CoreClient`), which parses, indexes, validates and
 *   serialises every item.
 *
 * The core keeps the vault in memory, so a write is a round trip:
 * `provider → core (validate + serialise) → WriteSet → vault → ChangeEvent`.
 * The core's answer is authoritative for content; the vault is authoritative
 * for storage. This file is the only place in the app allowed to import the
 * core bridge and the filesystem layer.
 */

import type {
  BatchResult,
  Capabilities,
  ChangeEvent,
  Comment,
  DataProvider,
  Diagnostic,
  IndexStats,
  Item,
  ItemDraft,
  ItemFilter,
  ItemPage,
  ItemPatch,
  ItemStatus,
  KbNode,
  KbPage,
  KbScope,
  MountInput,
  ProjectSummary,
  RefResolution,
  RepoInfo,
  SearchHit,
  SearchQuery,
  TeamSummary,
  Unsubscribe,
  UpdateOp,
} from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { hydrateOrBuild } from '@/cache/index-cache';
import type { CoreMethodName, CoreParams, CoreResult, WriteSet } from '@/core-bridge/api';
import { coreClient, type CoreClient } from '@/core-bridge/client';
import {
  FsaVault,
  getHandleRecord,
  getVault,
  listHandleRecords,
  normalizeDocsFolder,
  queryPermission,
  removeHandleRecord,
  requestPersistentStorage,
  saveHandleRecord,
  supportsFileSystemAccess,
  VaultError,
  type RepoHandleRecord,
  type VaultFS,
} from '@/fs';

type MountedRepo = {
  id: string;
  kind: RepoInfo['kind'];
  name: string;
  vault: VaultFS;
  docsFolder: string;
  projects: string[];
  lastIndexedAt?: string;
};

export type BrowserProviderOptions = {
  /** Injected by tests; production shares the app-wide worker client. */
  client?: CoreClient;
};

/** Core error codes that map onto a provider code; everything else is internal. */
const CORE_ERROR_CODES: Record<string, ProviderError['code']> = {
  stale_revision: 'stale_revision',
  rev_mismatch: 'stale_revision',
  conflict: 'stale_revision',
  validation_failed: 'validation_failed',
  invalid_item: 'validation_failed',
  invalid_request: 'validation_failed',
  not_found: 'not_found',
  unknown_id: 'not_found',
  unknown_method: 'internal',
  read_only: 'read_only',
  permission_denied: 'permission_denied',
};

function errorCode(error: unknown): string | null {
  if (typeof error !== 'object' || error === null) return null;
  const code = (error as { code?: unknown }).code;
  return typeof code === 'string' ? code : null;
}

/** Normalises a core or vault failure into the typed error the UI switches on. */
export function toProviderError(error: unknown): ProviderError {
  if (error instanceof ProviderError) return error;

  if (error instanceof VaultError) {
    const code = error.code === 'io' ? 'internal' : error.code;
    return new ProviderError(code, error.message, error.path);
  }

  const message = error instanceof Error ? error.message : String(error);
  const name = error instanceof Error ? error.name : '';
  if (name === 'NotAllowedError' || name === 'SecurityError') {
    return new ProviderError('permission_denied', message);
  }

  const code = errorCode(error);
  if (code) return new ProviderError(CORE_ERROR_CODES[code] ?? 'internal', message);
  return new ProviderError('internal', message);
}

export class BrowserProvider implements DataProvider {
  readonly kind = 'browser' as const;

  readonly #client: CoreClient;
  readonly #mounts = new Map<string, MountedRepo>();
  readonly #handlers = new Set<(event: ChangeEvent) => void>();
  #activeRepoId: string | null = null;

  constructor(options: BrowserProviderOptions = {}) {
    this.#client = options.client ?? coreClient;
  }

  #capabilities: Capabilities | null = null;

  /**
   * Capabilities follow the mounted vault: the `webkitdirectory` fallback is
   * read-only (story GIT-US-0011). Before anything is mounted we report what
   * the browser could do, so a Chromium user never sees the read-only banner.
   */
  get capabilities(): Capabilities {
    const vault = this.#activeMount()?.vault ?? [...this.#mounts.values()][0]?.vault;
    const write = vault ? vault.capabilities.write : supportsFileSystemAccess();
    // Return a stable reference while nothing changed so React effects keyed on
    // this object do not re-run (and re-render) on every access.
    if (this.#capabilities?.write === write) return this.#capabilities;
    this.#capabilities = {
      write,
      git: false,
      ssh: false,
      watch: false,
      fullTextSearch: 'core',
      mcp: false,
      openInEditor: false,
      maxBatchWrite: write ? 50 : 0,
    };
    return this.#capabilities;
  }

  // ---------------------------------------------------------------- workspace

  async listRepos(): Promise<RepoInfo[]> {
    const records = await listHandleRecords();
    const repos: RepoInfo[] = [];

    for (const record of records) {
      const mount = this.#mounts.get(record.id);
      const state = mount ? 'ready' : await this.#recordState(record);
      repos.push({
        id: record.id,
        kind: record.kind,
        name: record.name,
        location: record.name,
        docsFolder: mount?.docsFolder ?? record.docsFolder,
        state,
        projects: mount?.projects ?? record.projects ?? [],
        ...((mount?.lastIndexedAt ?? record.lastIndexedAt)
          ? { lastIndexedAt: mount?.lastIndexedAt ?? record.lastIndexedAt }
          : {}),
      });
    }

    // Fallback mounts have no handle to persist: they live for this session only.
    for (const mount of this.#mounts.values()) {
      if (records.some((record) => record.id === mount.id)) continue;
      repos.push({
        id: mount.id,
        kind: mount.kind,
        name: mount.name,
        location: mount.name,
        docsFolder: mount.docsFolder,
        state: 'ready',
        projects: mount.projects,
        ...(mount.lastIndexedAt ? { lastIndexedAt: mount.lastIndexedAt } : {}),
      });
    }

    return repos.sort((a, b) => a.name.localeCompare(b.name));
  }

  async listProjects(): Promise<ProjectSummary[]> {
    await this.#ensureActive();
    return this.#call('project.list', undefined);
  }

  /**
   * The team repository among the open folders. `team.get` fails with
   * `not_found` when none is open, which is a normal state, not an error the
   * UI has to show.
   */
  async getTeam(): Promise<TeamSummary | null> {
    await this.#ensureActive();
    try {
      return await this.#call('team.get', undefined);
    } catch (error) {
      if (error instanceof ProviderError && error.code === 'not_found') return null;
      throw error;
    }
  }

  async resolveRef(ref: string): Promise<RefResolution> {
    await this.#ensureActive();
    return this.#call('ref.resolve', { ref });
  }

  /**
   * Mounts a folder the user already picked. `MountInput.location` is the id
   * returned by `registerVault` (see `fs/vault-registry.ts`), which keeps
   * browser handles out of the provider interface.
   */
  async mountRepo(input: MountInput): Promise<RepoInfo> {
    const vault = getVault(input.location);
    if (!vault) {
      throw new ProviderError(
        'not_found',
        'That folder is no longer available. Choose it again to mount it.',
      );
    }

    const docsFolder = normalizeDocsFolder(input.docsFolder ?? '');
    const stats = await this.#load(input.location, vault, {
      kind: input.kind,
      docsFolder,
    });
    const mount = this.#mounts.get(input.location);
    const projects = mount?.projects ?? [];

    if (vault instanceof FsaVault) {
      const record: RepoHandleRecord = {
        id: input.location,
        name: vault.name,
        handle: vault.handle,
        docsFolder,
        kind: input.kind,
        projects,
        mountedAt: new Date().toISOString(),
        lastIndexedAt: mount?.lastIndexedAt ?? new Date().toISOString(),
      };
      await saveHandleRecord(record);
      void requestPersistentStorage();
    }

    this.#emit({ kind: 'repo', repoId: input.location });
    this.#emit({ kind: 'index', repoId: input.location, stats });

    return {
      id: input.location,
      kind: input.kind,
      name: vault.name,
      location: vault.name,
      docsFolder,
      state: 'ready',
      projects,
      ...(mount?.lastIndexedAt ? { lastIndexedAt: mount.lastIndexedAt } : {}),
    };
  }

  async unmountRepo(repoId: string): Promise<void> {
    this.#mounts.delete(repoId);
    if (this.#activeRepoId === repoId) {
      this.#activeRepoId = [...this.#mounts.keys()][0] ?? null;
    }
    try {
      await this.#call('workspace.unmount', { vaultId: repoId });
    } catch (error) {
      // A folder the core never loaded is already unmounted as far as it knows.
      if (!(error instanceof ProviderError && error.code === 'not_found')) throw error;
    }
    await removeHandleRecord(repoId);
    this.#emit({ kind: 'repo', repoId });
  }

  /** Full reload, or a rescan diff applied incrementally to the core index. */
  async reindex(repoId: string, opts?: { full?: boolean }): Promise<IndexStats> {
    const mount = await this.#ensureMounted(repoId);

    if (opts?.full || this.#activeRepoId !== repoId) {
      return this.#load(repoId, mount.vault, { kind: mount.kind, docsFolder: mount.docsFolder });
    }

    const events = await this.#vaultCall(() => mount.vault.rescan());
    const stats = events.length
      ? await this.#call('vault.apply', { events, vaultId: repoId })
      : await this.#call('vault.stats', { vaultId: repoId });

    mount.lastIndexedAt = new Date().toISOString();
    mount.projects = await this.#projectsOf(repoId);
    await this.#touchRecord(mount);

    this.#emit({ kind: 'index', repoId, stats });
    if (events.length) {
      this.#emit({ kind: 'kb', repoId, paths: events.map((event) => event.path) });
    }
    return stats;
  }

  // --------------------------------------------------------------------- read

  async listItems(query: ItemFilter): Promise<ItemPage> {
    await this.#ensureActive();
    return this.#call('item.list', query);
  }

  async getItem(id: string): Promise<Item> {
    await this.#ensureActive();
    return this.#call('item.get', { id });
  }

  async getChildren(id: string): Promise<Item[]> {
    await this.#ensureActive();
    return this.#call('item.children', { id });
  }

  async listComments(id: string): Promise<Comment[]> {
    await this.#ensureActive();
    return this.#call('comment.list', { id });
  }

  /**
   * A team scope reads the `knowledge/` folder of the team repository. Both
   * scopes are addressed by key: the core indexes the team knowledge base under
   * the team key exactly as it indexes a project's docs folder under its
   * project key (docs/04 §4).
   */
  async listKbTree(scope: KbScope): Promise<KbNode[]> {
    await this.#ensureActive();
    return this.#call('kb.tree', {
      project: scope.kind === 'project' ? scope.projectKey : scope.teamId,
    });
  }

  async getPage(scope: KbScope, path: string): Promise<KbPage> {
    await this.#ensureActive();
    return this.#call('kb.page', { path, ...this.#scopeVault(scope) });
  }

  /** Binary reads never go through the core: the bytes come from the vault. */
  async readAsset(scope: KbScope, path: string): Promise<Blob> {
    const mount = (await this.#mountForScope(scope)) ?? (await this.#ensureActive());
    return this.#vaultCall(() => mount.vault.readBinary(path));
  }

  async search(query: SearchQuery): Promise<SearchHit[]> {
    await this.#ensureActive();
    return this.#call('search', {
      q: query.text,
      ...(query.limit === undefined ? {} : { limit: query.limit }),
      ...(query.projectKey === undefined ? {} : { project: query.projectKey }),
    });
  }

  async validateItem(input: { id?: string; text?: string; path?: string }): Promise<Diagnostic[]> {
    await this.#ensureActive();
    return this.#call('item.validate', input);
  }

  // -------------------------------------------------------------------- write

  async createItem(input: ItemDraft): Promise<Item> {
    const active = await this.#ensureWritable();
    const { item, writes } = await this.#call('item.create', input);
    const mount = this.#mountForItem(item.id, active);
    await this.#persist(mount, writes);
    this.#emit({ kind: 'items', repoId: mount.id, ids: [item.id] });
    return item;
  }

  async updateItem(id: string, patch: ItemPatch, rev: string): Promise<Item> {
    const mount = this.#mountForItem(id, await this.#ensureWritable());
    const { item, writes } = await this.#call('item.update', { id, patch, rev });
    await this.#persist(mount, writes);
    this.#emit({ kind: 'items', repoId: mount.id, ids: [item.id] });
    return item;
  }

  async moveItem(id: string, status: ItemStatus, rev: string): Promise<Item> {
    const mount = this.#mountForItem(id, await this.#ensureWritable());
    const { item, writes } = await this.#call('item.move', { id, status, rev });
    await this.#persist(mount, writes);
    this.#emit({ kind: 'items', repoId: mount.id, ids: [item.id] });
    return item;
  }

  async updateMany(ops: UpdateOp[]): Promise<BatchResult> {
    const result: BatchResult = { applied: 0, failed: [] };
    for (const op of ops) {
      try {
        await this.updateItem(op.id, op.patch, op.rev);
        result.applied += 1;
      } catch (error) {
        const provider = toProviderError(error);
        result.failed.push({ id: op.id, code: provider.code, message: provider.message });
      }
    }
    return result;
  }

  async deleteItem(id: string, rev: string): Promise<void> {
    const mount = this.#mountForItem(id, await this.#ensureWritable());
    const { writes } = await this.#call('item.delete', { id, rev });
    await this.#persist(mount, writes);
    this.#emit({ kind: 'items', repoId: mount.id, ids: [id] });
  }

  async addComment(id: string, body: string, author = 'me'): Promise<Comment> {
    const mount = this.#mountForItem(id, await this.#ensureWritable());
    const { comment, writes } = await this.#call('comment.add', { id, author, body });
    await this.#persist(mount, writes);
    this.#emit({ kind: 'items', repoId: mount.id, ids: [id] });
    return comment;
  }

  async writePage(scope: KbScope, path: string, content: string, rev?: string): Promise<KbPage> {
    const active = await this.#ensureWritable();
    const mount = (await this.#mountForScope(scope)) ?? active;
    const { page, writes } = await this.#call('kb.write', {
      path,
      text: content,
      vaultId: mount.id,
      ...(rev === undefined ? {} : { rev }),
    });
    await this.#persist(mount, writes);
    this.#emit({ kind: 'kb', repoId: mount.id, paths: [page.path] });
    return page;
  }

  // ------------------------------------------------------------------- events

  subscribe(handler: (event: ChangeEvent) => void): Unsubscribe {
    this.#handlers.add(handler);
    return () => {
      this.#handlers.delete(handler);
    };
  }

  // ---------------------------------------------------------------- internals

  #emit(event: ChangeEvent): void {
    for (const handler of this.#handlers) handler(event);
  }

  #activeMount(): MountedRepo | undefined {
    return this.#activeRepoId ? this.#mounts.get(this.#activeRepoId) : undefined;
  }

  async #call<M extends CoreMethodName>(method: M, params: CoreParams<M>): Promise<CoreResult<M>> {
    try {
      return await this.#client.call(method, params);
    } catch (error) {
      throw toProviderError(error);
    }
  }

  async #vaultCall<T>(run: () => Promise<T>): Promise<T> {
    try {
      return await run();
    } catch (error) {
      throw toProviderError(error);
    }
  }

  /**
   * Pushes the whole folder into the core and refreshes the mount record.
   *
   * Every folder gets its own repository inside the core workspace, keyed by
   * the same id the handle store uses, so that a team repository and several
   * project clones are open at the same time (GIT-US-0016). Loading one folder
   * no longer evicts the others.
   */
  async #load(
    repoId: string,
    vault: VaultFS,
    meta: { kind: RepoInfo['kind']; docsFolder: string },
  ): Promise<IndexStats> {
    const files = await this.#vaultCall(async () => vault.cachedFiles() ?? vault.readTextFiles());
    await this.#call('workspace.mount', {
      vaultId: repoId,
      role: meta.kind,
      rootLabel: vault.name,
    });
    // The IndexedDB snapshot cache (GIT-US-0007) paints the vault structure
    // before the files are parsed again, then the real index replaces it.
    let stats: IndexStats;
    try {
      ({ stats } = await hydrateOrBuild(this.#client, repoId, files, {
        rootLabel: vault.name,
        vaultId: repoId,
      }));
    } catch (error) {
      throw toProviderError(error);
    }
    const projects = await this.#projectsOf(repoId);

    const mount: MountedRepo = {
      id: repoId,
      kind: meta.kind,
      name: vault.name,
      vault,
      docsFolder: meta.docsFolder,
      projects,
      lastIndexedAt: new Date().toISOString(),
    };
    this.#mounts.set(repoId, mount);
    this.#activeRepoId = repoId;
    await this.#touchRecord(mount);
    return stats;
  }

  /** Project keys one repository of the workspace exposes. */
  async #projectsOf(repoId: string): Promise<string[]> {
    const summary = await this.#call('workspace.list', undefined);
    const entry = summary.vaults.find((v) => v.id === repoId);
    return entry?.projects ?? [];
  }

  /** The repository a knowledge-base scope reads from, when it is known. */
  async #mountForScope(scope: KbScope): Promise<MountedRepo | undefined> {
    if (scope.kind === 'project') return this.#mountForProject(scope.projectKey);
    const summary = await this.#call('workspace.list', undefined);
    const teamVault = summary.vaults.find((v) => v.team);
    return teamVault ? this.#mounts.get(teamVault.id) : undefined;
  }

  /** `vaultId` for a knowledge-base scope, so a page read hits the right folder. */
  #scopeVault(scope: KbScope): { vaultId?: string } {
    if (scope.kind !== 'project') return {};
    const mount = this.#mountForProject(scope.projectKey);
    return mount ? { vaultId: mount.id } : {};
  }

  /** The folder holding a project, by the key its items are prefixed with. */
  #mountForProject(key: string): MountedRepo | undefined {
    for (const mount of this.#mounts.values()) {
      if (mount.projects.includes(key)) return mount;
    }
    return undefined;
  }

  /**
   * The folder that owns an item id. The project key is the id's own prefix,
   * which is what lets a write land in the right repository without the caller
   * having to say which one it meant.
   */
  #mountForItem(id: string, fallback: MountedRepo): MountedRepo {
    const key = id.split('-')[0] ?? '';
    return this.#mountForProject(key) ?? fallback;
  }

  /** Keeps the persisted record in step with what the last index found. */
  async #touchRecord(mount: MountedRepo): Promise<void> {
    if (!(mount.vault instanceof FsaVault)) return;
    const record = await getHandleRecord(mount.id);
    if (!record) return;
    await saveHandleRecord({
      ...record,
      docsFolder: mount.docsFolder,
      projects: mount.projects,
      ...(mount.lastIndexedAt ? { lastIndexedAt: mount.lastIndexedAt } : {}),
    });
  }

  async #recordState(record: RepoHandleRecord): Promise<RepoInfo['state']> {
    try {
      const permission = await queryPermission(record.handle, 'readwrite');
      return permission === 'granted' ? 'ready' : 'needs-permission';
    } catch {
      return 'needs-permission';
    }
  }

  /** Restores a mount after a reload, when permission is still granted. */
  async #ensureMounted(repoId: string): Promise<MountedRepo> {
    const mounted = this.#mounts.get(repoId);
    if (mounted) return mounted;

    const registered = getVault(repoId);
    if (registered) {
      await this.#load(repoId, registered, { kind: 'project', docsFolder: '' });
      const mount = this.#mounts.get(repoId);
      if (mount) return mount;
    }

    const record = await getHandleRecord(repoId);
    if (!record) {
      throw new ProviderError('not_found', `Repository ${repoId} is not mounted`);
    }
    const permission = await queryPermission(record.handle, 'readwrite');
    if (permission !== 'granted') {
      throw new ProviderError(
        'permission_denied',
        `Permission for "${record.name}" has expired. Reconnect the folder to continue.`,
      );
    }

    const vault = new FsaVault(record.handle);
    await this.#load(repoId, vault, { kind: record.kind, docsFolder: record.docsFolder });
    const mount = this.#mounts.get(repoId);
    if (!mount) throw new ProviderError('internal', `Repository ${repoId} failed to mount`);
    return mount;
  }

  /** The repository whose index the core currently holds. */
  async #ensureActive(): Promise<MountedRepo> {
    const active = this.#activeMount();
    if (active) return active;

    const [first] = this.#mounts.values();
    if (first) {
      await this.#load(first.id, first.vault, { kind: first.kind, docsFolder: first.docsFolder });
      return this.#mounts.get(first.id) ?? first;
    }

    const records = await listHandleRecords();
    for (const record of records) {
      if ((await this.#recordState(record)) !== 'ready') continue;
      return this.#ensureMounted(record.id);
    }

    throw new ProviderError(
      'not_found',
      'No folder is open. Open a project folder from the workspace to continue.',
    );
  }

  async #ensureWritable(): Promise<MountedRepo> {
    const mount = await this.#ensureActive();
    if (!mount.vault.capabilities.write) {
      throw new ProviderError(
        'read_only',
        'This folder was opened read-only. Use a Chromium browser or install the companion to make changes.',
      );
    }
    return mount;
  }

  /** Persists everything the core wrote, then drops the removed files. */
  async #persist(mount: MountedRepo, writes: WriteSet): Promise<void> {
    await this.#vaultCall(async () => {
      for (const file of writes.written) {
        await mount.vault.writeFile(file.path, file.text);
      }
      for (const path of writes.removed) {
        await mount.vault.removeFile(path);
      }
    });
    mount.lastIndexedAt = new Date().toISOString();
  }
}
