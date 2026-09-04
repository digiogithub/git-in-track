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
  BoardMoveResult,
  BoardPatch,
  BoardSummary,
  BoardView,
  Capabilities,
  CardMove,
  ChangeEvent,
  Comment,
  ConflictAnalysis,
  ConflictMerge,
  ConflictResolution,
  ConflictResolveResult,
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
  SnapshotRefresh,
  SnapshotResult,
  RetroDraft,
  RetroListing,
  RetroPatch,
  RetroPromotion,
  RetroResult,
  RetroFilter,
  RetroView,
  SprintCarry,
  SprintDraft,
  SprintFilter,
  SprintPatch,
  SprintResult,
  SprintSummary,
  SprintView,
  TeamSummary,
  Unsubscribe,
  UpdateOp,
  GitCommit,
  GitRepoStatus,
  GitSettings,
  GitSettingsPatch,
  SyncOptions,
  SyncRepoStatus,
  SyncResult,
  SyncSettings,
  SyncSettingsPatch,
  SyncStatus,
} from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { hydrateOrBuild } from '@/cache/index-cache';
import type {
  ConflictResolutionParams,
  CoreMethodName,
  CoreParams,
  CoreResult,
  VaultWriteSet,
  WriteSet,
} from '@/core-bridge/api';
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
import type { DirectoryHandleLike } from '@/fs/types';
import { readSyncStatus, runSync, type BrowserConflict } from '@/git/browser-sync';
import {
  createAuthCallback,
  createAuthFailureCallback,
  forgetCredentials,
} from '@/git/credentials';
import {
  BROWSER_GIT_REASON,
  readGitSettings,
  readSyncSettings,
  writeGitSettings,
  writeSyncSettings,
} from '@/git/settings-store';

/** One conflicted path browser mode is holding for the resolver. */
type PendingConflict = BrowserConflict & { resolved?: string };

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
  /**
   * Namespaces the per-workspace commit-on-save settings in browser storage
   * (docs/06-git-sync.md §3.3). Defaults to `default`.
   */
  workspace?: string;
};

/** Core error codes that map onto a provider code; everything else is internal. */
const CORE_ERROR_CODES: Record<string, ProviderError['code']> = {
  stale_revision: 'stale_revision',
  wip_limit_exceeded: 'wip_limit_exceeded',
  repo_not_cloned: 'repo_not_cloned',
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
  /**
   * The conflicts of the last stopped merge, per repository and path, with the
   * three versions the merge driver saw and any resolution the user accepted.
   * `isomorphic-git` rolls a conflicting merge back, so this in-memory record
   * is the whole conflicted state: a reload simply loses it and leaves the
   * working tree exactly as it was, which is what abort would have done
   * (docs/06 §6.2).
   */
  readonly #conflicts = new Map<string, Map<string, PendingConflict>>();
  /** Namespaces the per-workspace git settings in browser storage. */
  readonly #workspaceName: string;

  constructor(options: BrowserProviderOptions = {}) {
    this.#client = options.client ?? coreClient;
    this.#workspaceName = options.workspace ?? 'default';
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
    // Unmounting is one of the "forget credentials" triggers of docs/06 §8.2.
    forgetCredentials();
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

  // -------------------------------------------------------------------- boards

  /**
   * The boards of the team repository. Without one open there is nothing to
   * list, which is a state rather than an error.
   */
  async listBoards(): Promise<BoardSummary[]> {
    await this.#ensureActive();
    try {
      const result = await this.#call('board.list', undefined);
      return result.boards;
    } catch (error) {
      if (error instanceof ProviderError && error.code === 'not_found') return [];
      throw error;
    }
  }

  async getBoard(slug: string): Promise<BoardView> {
    await this.#ensureActive();
    return this.#call('board.get', { board: slug });
  }

  /**
   * A move writes two repositories, so the core answers with one `WriteSet`
   * per repository and each is persisted into the folder it belongs to.
   */
  async moveCard(move: CardMove): Promise<BoardMoveResult> {
    await this.#ensureWritable();
    const result = await this.#call('board.move', {
      board: move.board,
      ref: move.ref,
      toColumn: move.toColumn,
      position: move.position,
      ...(move.status === undefined ? {} : { status: move.status }),
      ...(move.rev === undefined ? {} : { rev: move.rev }),
      ...(move.itemRev === undefined ? {} : { itemRev: move.itemRev }),
      ...(move.force === undefined ? {} : { force: move.force }),
    });
    await this.#persistSets(result.writes);
    if (result.item) {
      const mount = this.#mountForItem(result.item.id, await this.#ensureActive());
      this.#emit({ kind: 'items', repoId: mount.id, ids: [result.item.id] });
    }
    return result;
  }

  /**
   * Edits the board file of the team repository and nothing else: one write,
   * persisted through the team repository's directory handle.
   */
  async updateBoard(slug: string, patch: BoardPatch, rev?: string): Promise<BoardView> {
    await this.#ensureWritable();
    const result = await this.#call('board.update', {
      board: slug,
      patch,
      ...(rev === undefined ? {} : { rev }),
    });
    await this.#persistSets(result.writes);
    return result.board;
  }

  // ------------------------------------------------------------------- sprints

  /**
   * The sprints of the team repository. Without one open there is nothing to
   * list, which is a state rather than an error.
   */
  async listSprints(filter: SprintFilter = {}): Promise<SprintSummary[]> {
    await this.#ensureActive();
    try {
      const result = await this.#call('sprint.list', {
        ...(filter.board === undefined ? {} : { board: filter.board }),
        ...(filter.state === undefined ? {} : { state: filter.state }),
      });
      return result.sprints;
    } catch (error) {
      if (error instanceof ProviderError && error.code === 'not_found') return [];
      throw error;
    }
  }

  async getSprint(id: string): Promise<SprintView> {
    await this.#ensureActive();
    return this.#call('sprint.get', { id });
  }

  async createSprint(input: SprintDraft): Promise<SprintResult> {
    await this.#ensureWritable();
    return this.#persistSprint(await this.#call('sprint.create', input));
  }

  async updateSprint(id: string, patch: SprintPatch, rev?: string): Promise<SprintResult> {
    await this.#ensureWritable();
    return this.#persistSprint(
      await this.#call('sprint.update', { id, patch, ...(rev === undefined ? {} : { rev }) }),
    );
  }

  async startSprint(id: string, rev?: string, force?: boolean): Promise<SprintResult> {
    await this.#ensureWritable();
    return this.#persistSprint(
      await this.#call('sprint.start', {
        id,
        ...(rev === undefined ? {} : { rev }),
        ...(force === undefined ? {} : { force }),
      }),
    );
  }

  async closeSprint(id: string, carry?: SprintCarry[], rev?: string): Promise<SprintResult> {
    await this.#ensureWritable();
    return this.#persistSprint(
      await this.#call('sprint.close', {
        id,
        ...(carry === undefined ? {} : { carry }),
        ...(rev === undefined ? {} : { rev }),
      }),
    );
  }

  // ------------------------------------------------------------------- retros

  /**
   * The retros of the team repository. Without one open there is nothing to
   * list, which is a state rather than an error.
   */
  async listRetros(filter: RetroFilter = {}): Promise<RetroListing> {
    await this.#ensureActive();
    try {
      return await this.#call('retro.list', {
        ...(filter.sprint === undefined ? {} : { sprint: filter.sprint }),
        ...(filter.board === undefined ? {} : { board: filter.board }),
        ...(filter.state === undefined ? {} : { state: filter.state }),
      });
    } catch (error) {
      if (error instanceof ProviderError && error.code === 'not_found') {
        return { retros: [], carried: [], diagnostics: [] };
      }
      throw error;
    }
  }

  async getRetro(id: string): Promise<RetroView> {
    await this.#ensureActive();
    return this.#call('retro.get', { id });
  }

  async createRetro(input: RetroDraft): Promise<RetroResult> {
    await this.#ensureWritable();
    return this.#persistRetro(await this.#call('retro.create', input));
  }

  async updateRetro(id: string, patch: RetroPatch, rev?: string): Promise<RetroResult> {
    await this.#ensureWritable();
    return this.#persistRetro(
      await this.#call('retro.update', { id, patch, ...(rev === undefined ? {} : { rev }) }),
    );
  }

  async promoteRetroAction(input: RetroPromotion): Promise<RetroResult> {
    await this.#ensureWritable();
    return this.#persistRetro(
      await this.#call('retro.promote', {
        id: input.retro,
        action: input.action,
        project: input.project,
        ...(input.labels === undefined ? {} : { labels: input.labels }),
        ...(input.rev === undefined ? {} : { rev: input.rev }),
      }),
    );
  }

  /** Persists what a retro call wrote: the team repository, and the project
   * repository a promoted task landed in. */
  async #persistRetro(result: RetroResult): Promise<RetroResult> {
    await this.#persistSets(result.writes);
    return result;
  }

  /** Persists what a sprint call wrote: the team repository, and the project
   * repository of an item a closing decision sent back to the backlog. */
  async #persistSprint(result: SprintResult): Promise<SprintResult> {
    await this.#persistSets(result.writes);
    return result;
  }

  /** Writes one `WriteSet` per repository into the folder it belongs to. */
  async #persistSets(sets: VaultWriteSet[]): Promise<void> {
    for (const set of sets) {
      const mount = this.#mounts.get(set.vaultId);
      if (!mount) continue;
      await this.#persist(mount, { written: set.written, removed: set.removed });
      this.#emit({ kind: 'repo', repoId: mount.id });
    }
  }

  // ----------------------------------------------------------------- snapshots

  async listSnapshots(): Promise<SnapshotResult[]> {
    const result = await this.#call('snapshot.list', undefined);
    return result.snapshots;
  }

  /**
   * Regenerating a snapshot writes one file into the team repository, so the
   * write set comes back the way a card move's does and is persisted through
   * the same directory handle.
   */
  async refreshSnapshots(input: SnapshotRefresh = {}): Promise<SnapshotResult[]> {
    if (!input.dryRun) await this.#ensureWritable();
    const result = await this.#call('snapshot.refresh', {
      ...(input.projects === undefined ? {} : { projects: input.projects }),
      ...(input.generatedBy === undefined ? {} : { generatedBy: input.generatedBy }),
      ...(input.includeClosed === undefined ? {} : { includeClosed: input.includeClosed }),
      ...(input.dryRun === undefined ? {} : { dryRun: input.dryRun }),
    });
    for (const set of result.writes) {
      const mount = this.#mounts.get(set.vaultId);
      if (!mount) continue;
      await this.#persist(mount, { written: set.written, removed: set.removed });
      this.#emit({ kind: 'repo', repoId: mount.id });
    }
    return result.snapshots;
  }

  // ---------------------------------------------------------------------- git

  /**
   * The commit-on-save settings of this workspace. Browser-only mode stores
   * them and renders the message with them, but cannot commit until
   * isomorphic-git lands with GIT-US-0021, which is what `supported` reports.
   */
  async getGitSettings(): Promise<GitSettings> {
    return Promise.resolve(readGitSettings(this.#workspaceName));
  }

  async updateGitSettings(patch: GitSettingsPatch): Promise<GitSettings> {
    try {
      return await Promise.resolve(writeGitSettings(patch, this.#workspaceName));
    } catch (error) {
      throw new ProviderError(
        'validation_failed',
        error instanceof Error ? error.message : String(error),
      );
    }
  }

  /**
   * Browser-only mode has no git backend to inspect yet, so every mounted
   * repository reports why rather than pretending to be a working tree.
   */
  async getGitStatus(repoId?: string): Promise<GitRepoStatus[]> {
    const mounts = [...this.#mounts.values()].filter(
      (mount) => repoId === undefined || mount.id === repoId,
    );
    return Promise.resolve(
      mounts.map((mount) => ({
        repo: mount.id,
        path: mount.name,
        git: false,
        reason: BROWSER_GIT_REASON,
        capabilities: {
          backend: 'isomorphic-git',
          hooks: false,
          signing: false,
          credentialHelpers: false,
          pathspecCommit: false,
        },
      })),
    );
  }

  commitNow(): Promise<GitCommit[]> {
    return Promise.reject(new ProviderError('read_only', BROWSER_GIT_REASON));
  }

  // --------------------------------------------------------------- git sync

  /**
   * Reads each mounted folder's git state with isomorphic-git (docs/06 §6.1).
   * A folder that is not a working tree, or a vault that has no File System
   * Access handle (the `webkitdirectory` fallback), reports why instead of
   * pretending.
   */
  async getSyncStatus(repoId?: string): Promise<SyncRepoStatus[]> {
    const mounts = [...this.#mounts.values()].filter(
      (mount) => repoId === undefined || mount.id === repoId,
    );
    return Promise.all(
      mounts.map(async (mount) => {
        const row: SyncRepoStatus = {
          repo: mount.id,
          path: mount.name,
          git: false,
          backend: 'isomorphic-git',
          pending: 0,
        };
        const handle = handleOf(mount.vault);
        if (!handle) {
          row.reason = NO_HANDLE_REASON;
          return row;
        }
        try {
          row.status = await readSyncStatus(handle);
          row.git = true;
        } catch (error) {
          row.reason = error instanceof Error ? error.message : String(error);
        }
        return row;
      }),
    );
  }

  getSyncSettings(): Promise<SyncSettings> {
    return Promise.resolve(readSyncSettings(this.#workspaceName));
  }

  updateSyncSettings(patch: SyncSettingsPatch): Promise<SyncSettings> {
    try {
      return Promise.resolve(writeSyncSettings(patch, this.#workspaceName));
    } catch (error) {
      return Promise.reject(
        new ProviderError(
          'validation_failed',
          error instanceof Error ? error.message : String(error),
        ),
      );
    }
  }

  /**
   * Fetch, merge and push over isomorphic-git. The strategy is always `merge`
   * and a run without a configured CORS proxy reports `git_cors_proxy_required`
   * rather than failing obscurely against a host that sends no CORS headers
   * (docs/06 §6.2, §6.3).
   */
  async sync(repoId: string | undefined, opts: SyncOptions = {}): Promise<SyncResult[]> {
    const settings = readSyncSettings(this.#workspaceName);
    const git = await this.getGitSettings();
    const author =
      git.authorName && git.authorEmail
        ? { name: git.authorName, email: git.authorEmail }
        : undefined;
    const mounts = [...this.#mounts.values()].filter(
      (mount) => repoId === undefined || mount.id === repoId,
    );
    const results: SyncResult[] = [];
    for (const mount of mounts) {
      const handle = handleOf(mount.vault);
      if (!handle) {
        results.push(unsupportedSync(mount.id, NO_HANDLE_REASON));
        continue;
      }
      const resolutions = resolutionsOf(this.#conflicts.get(mount.id));
      const result = await runSync(handle, mount.id, {
        ...opts,
        ...(resolutions ? { resolutions } : {}),
        push: opts.push ?? settings.pushOnSync,
        ...(settings.corsProxy ? { corsProxy: settings.corsProxy } : {}),
        ...(author ? { author } : {}),
        // The token, when a host asks for one, is prompted for and held in
        // memory for this tab only (GIT-US-0023, docs/06 §8.2).
        onAuth: createAuthCallback(settings.corsProxy ? { corsProxy: settings.corsProxy } : {}),
        onAuthFailure: createAuthFailureCallback(),
        onConflict: (conflicts) => this.#rememberConflicts(mount.id, conflicts),
      });
      if (result.phase === 'done' && !result.dryRun && result.pulled > 0) {
        // Incoming work changed files under our feet: reload the vault so the
        // core and every open view see them.
        await this.reindex(mount.id);
      }
      results.push(result);
    }
    return results;
  }

  /**
   * Browser git rolls a conflicting merge back by itself, so aborting is only
   * ever about forgetting the conflict this session is holding: the working
   * tree was never touched, which is exactly the pre-sync state.
   */
  async abortSync(repoId: string): Promise<SyncRepoStatus> {
    this.#conflicts.delete(repoId);
    const [row] = await this.getSyncStatus(repoId);
    if (!row) {
      throw new ProviderError('not_found', `No repository is mounted as ${repoId}.`);
    }
    return row;
  }

  listSyncConflicts(
    repoId?: string,
  ): Promise<{ repo: string; paths: string[]; operation?: string }[]> {
    const out: { repo: string; paths: string[]; operation?: string }[] = [];
    for (const [repo, paths] of this.#conflicts) {
      if (repoId !== undefined && repo !== repoId) continue;
      if (paths.size === 0) continue;
      out.push({ repo, paths: [...paths.keys()].sort(), operation: 'merge' });
    }
    return Promise.resolve(out);
  }

  /**
   * The three versions of one conflicted path and the merge the core proposes
   * for them. The merge runs in the same Go code the companion calls, so the
   * two runtimes never drift (docs/06 §5, §6.2).
   */
  async readConflict(repoId: string, path: string): Promise<ConflictAnalysis> {
    const pending = this.#pendingConflict(repoId, path);
    const merge = await this.#mergeConflict(pending);
    return {
      repo: repoId,
      path,
      kind: pending.kind,
      operation: 'merge',
      strategy: 'merge',
      versions: {
        path,
        kind: pending.kind,
        base: pending.base,
        ours: pending.ours,
        theirs: pending.theirs,
        hasBase: pending.base !== '',
        hasOurs: true,
        hasTheirs: true,
        binary: false,
      },
      merge,
    };
  }

  /**
   * Writes a resolution and, once every conflicted path has one, replays the
   * merge with those resolutions so that it completes: the merge driver is
   * handed the resolved text and reports a clean merge, which is how browser
   * mode finishes an integration it could not finish on its own.
   */
  async resolveConflict(
    repoId: string,
    path: string,
    resolution: ConflictResolution,
  ): Promise<ConflictResolveResult> {
    const pending = this.#pendingConflict(repoId, path);
    const merge = await this.#mergeConflict(pending, resolution);
    if (!merge.clean) {
      throw new ProviderError(
        'validation_failed',
        `The resolution of ${path} still leaves conflicted hunks: decide every hunk, or use ` +
          'keep mine, keep theirs or a manual edit.',
      );
    }
    pending.resolved = merge.content;

    const repoConflicts = this.#conflicts.get(repoId);
    const remaining = [...(repoConflicts?.values() ?? [])].filter((c) => c.resolved === undefined);
    const proceed = resolution.continue !== false && remaining.length === 0;
    const out: ConflictResolveResult = {
      repo: repoId,
      path,
      merge,
      result: {
        staged: true,
        continued: false,
        remaining: remaining.map((c) => ({ path: c.path, kind: c.kind })),
      },
    };
    if (!proceed) return out;

    const results = await this.sync(repoId, { push: true });
    const [result] = results;
    if (result && (result.phase === 'failed' || result.phase === 'conflicts')) {
      throw new ProviderError(
        'git_conflict',
        result.message ?? `The merge of ${repoId} could not be completed.`,
      );
    }
    this.#conflicts.delete(repoId);
    out.result.continued = true;
    if (result) out.result.status = result.after;
    const [row] = await this.getSyncStatus(repoId);
    if (row) out.status = row;
    return out;
  }

  /** Records the conflicts a stopped merge reported, keeping any resolution. */
  #rememberConflicts(repoId: string, conflicts: BrowserConflict[]): void {
    const existing = this.#conflicts.get(repoId);
    const next = new Map<string, PendingConflict>();
    for (const conflict of conflicts) {
      const previous = existing?.get(conflict.path);
      next.set(conflict.path, {
        ...conflict,
        ...(previous?.resolved === undefined ? {} : { resolved: previous.resolved }),
      });
    }
    this.#conflicts.set(repoId, next);
  }

  /** Looks up one held conflict, or explains that the list is stale. */
  #pendingConflict(repoId: string, path: string): PendingConflict {
    const pending = this.#conflicts.get(repoId)?.get(path);
    if (!pending) {
      throw new ProviderError(
        'not_found',
        `${path} is not conflicted in ${repoId}: run the sync again to see the current conflicts.`,
      );
    }
    return pending;
  }

  /** Runs the core's three-way merge over one held conflict. */
  async #mergeConflict(
    pending: PendingConflict,
    resolution?: ConflictResolution,
  ): Promise<ConflictMerge> {
    const merged = await this.#call('conflict.merge', {
      path: pending.path,
      base: pending.base,
      ours: pending.ours,
      theirs: pending.theirs,
      ...(resolution ? { resolution: coreResolution(resolution) } : {}),
    });
    return merged satisfies ConflictMerge;
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

/** Why a vault with no File System Access handle cannot be driven by git. */
const NO_HANDLE_REASON =
  'This folder was opened read-only through the directory-upload fallback, which gives no handle ' +
  'git can write through. Reopen it with "Open folder" in a Chromium browser, or run the companion.';

/** The directory handle of a vault, when it has one. */
function handleOf(vault: VaultFS): DirectoryHandleLike | undefined {
  return vault instanceof FsaVault ? vault.handle : undefined;
}

/** A report for a repository this runtime cannot sync at all. */
function unsupportedSync(repo: string, reason: string): SyncResult {
  const status: SyncStatus = {
    branch: '',
    detached: false,
    clean: true,
    trackedChanges: false,
    ahead: 0,
    behind: 0,
    state: 'no_remote',
  };
  return {
    repo,
    dryRun: false,
    strategy: 'merge',
    phase: 'failed',
    before: status,
    after: status,
    pulled: 0,
    pushed: 0,
    retries: 0,
    durationMs: 0,
    code: 'git_unsupported',
    message: reason,
  };
}

/** Maps the provider's resolution onto the core's, which is side-shaped. */
function coreResolution(resolution: ConflictResolution): ConflictResolutionParams {
  return {
    ...(resolution.resolution === 'ours' || resolution.resolution === 'theirs'
      ? { take: resolution.resolution }
      : {}),
    ...(resolution.resolution === 'manual' && resolution.content !== undefined
      ? { content: resolution.content }
      : {}),
    ...(resolution.body === undefined ? {} : { body: resolution.body }),
    ...(resolution.fields === undefined ? {} : { fields: resolution.fields }),
    ...(resolution.hunks === undefined ? {} : { hunks: resolution.hunks }),
    ...(resolution.hunkText === undefined ? {} : { hunkText: resolution.hunkText }),
  };
}

/** The resolved texts a replayed merge applies, keyed by path. */
function resolutionsOf(
  conflicts: Map<string, PendingConflict> | undefined,
): Record<string, string> | undefined {
  if (!conflicts || conflicts.size === 0) return undefined;
  const out: Record<string, string> = {};
  for (const [path, conflict] of conflicts) {
    if (conflict.resolved !== undefined) out[path] = conflict.resolved;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}
