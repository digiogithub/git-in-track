/**
 * The data provider boundary (see docs/05-web-app.md §4).
 *
 * Everything above this boundary is identical in both runtime modes. No feature
 * code may import `isomorphic-git`, the WASM bridge or `fetch('/api/...')`
 * directly: features talk to this interface only.
 *
 * Item, page and query shapes are shared with the WASM core contract in
 * `@/core-bridge/api` so that the browser provider can pass them through
 * unchanged and the companion provider maps them 1:1 onto the REST API.
 *
 * Board, sprint, retro and git members land with their phases (3 and 4) and
 * are intentionally absent here.
 */

import type {
  Comment,
  Diagnostic,
  IndexStats,
  Item,
  ItemDraft,
  ItemFilter,
  ItemPage,
  ItemPatch,
  ItemType,
  KbNode,
  KbPage,
  Priority,
  ProjectSummary,
  RefResolution,
  SearchHit,
  TeamMember,
  TeamProjectSummary,
  TeamSummary,
  WorkspaceSummary,
  WorkspaceVault,
} from '@/core-bridge/api';

export type {
  Comment,
  Diagnostic,
  IndexStats,
  Item,
  ItemDraft,
  ItemFilter,
  ItemPage,
  ItemPatch,
  ItemType,
  KbNode,
  KbPage,
  Priority,
  ProjectSummary,
  RefResolution,
  SearchHit,
  TeamMember,
  TeamProjectSummary,
  TeamSummary,
  WorkspaceSummary,
  WorkspaceVault,
};

export type ProviderKind = 'browser' | 'companion';

/** Statuses are configured per project in `project.yaml`; the UI never hardcodes them. */
export type ItemStatus = string;

export type Capabilities = {
  /** false for the `webkitdirectory` read-only fallback. */
  write: boolean;
  git: boolean;
  /** Companion only. */
  ssh: boolean;
  /** fsnotify push events. */
  watch: boolean;
  fullTextSearch: 'core' | 'bleve';
  mcp: boolean;
  openInEditor: boolean;
  maxBatchWrite: number;
};

export type RepoKind = 'project' | 'team';

export type RepoInfo = {
  id: string;
  kind: RepoKind;
  name: string;
  /** Absolute path in companion mode; the handle name in browser-only mode. */
  location: string;
  docsFolder: string;
  branch?: string;
  ahead?: number;
  behind?: number;
  dirtyFiles?: number;
  lastIndexedAt?: string;
  state: 'ready' | 'needs-permission' | 'indexing' | 'error';
  error?: string;
  /** Project keys discovered inside this repository. */
  projects: string[];
};

export type MountInput = {
  kind: RepoKind;
  /** Companion mode: an absolute path. Browser mode: a picked directory handle id. */
  location: string;
  docsFolder?: string;
};

/**
 * A knowledge base scope: a project's docs folder, or the `knowledge/` folder
 * of the team repository. `teamId` is the team key of `team.yaml`.
 */
export type KbScope = { kind: 'project'; projectKey: string } | { kind: 'team'; teamId: string };

export type SearchQuery = {
  text: string;
  projectKey?: string;
  limit?: number;
};

export type UpdateOp = {
  id: string;
  patch: ItemPatch;
  rev: string;
};

export type BatchResult = {
  applied: number;
  failed: { id: string; code: ProviderErrorCode; message: string }[];
};

export type ProviderErrorCode =
  | 'stale_revision'
  | 'validation_failed'
  | 'not_found'
  | 'read_only'
  | 'permission_denied'
  | 'git_conflict'
  | 'git_auth_failed'
  | 'repo_not_cloned'
  | 'internal';

export type ChangeEvent =
  | { kind: 'items'; repoId: string; ids: string[] }
  | { kind: 'kb'; repoId: string; paths: string[] }
  | { kind: 'repo'; repoId: string }
  | { kind: 'index'; repoId: string; stats: IndexStats };

export type Unsubscribe = () => void;

/**
 * One interface, two implementations (`BrowserProvider`, `CompanionProvider`)
 * plus the in-memory `FakeProvider` used by component tests. The UI branches on
 * `capabilities`, never on `kind`.
 */
export interface DataProvider {
  readonly kind: ProviderKind;
  readonly capabilities: Capabilities;

  // workspace
  listRepos(): Promise<RepoInfo[]>;
  listProjects(): Promise<ProjectSummary[]>;
  /**
   * The team repository of the workspace, or `null` when none is open. Every
   * project it declares is listed, whether or not a clone of it is open: a
   * project with `cloned: false` is remote, not missing (docs/04 §7).
   */
  getTeam(): Promise<TeamSummary | null>;
  /**
   * Resolves a `<projectKey>/<itemId>` reference across every open repository.
   * A reference into a project nobody cloned resolves to `cloned: false` with a
   * reason, never to a failure.
   */
  resolveRef(ref: string): Promise<RefResolution>;
  mountRepo(input: MountInput): Promise<RepoInfo>;
  unmountRepo(repoId: string): Promise<void>;
  reindex(repoId: string, opts?: { full?: boolean }): Promise<IndexStats>;

  // read
  listItems(query: ItemFilter): Promise<ItemPage>;
  getItem(id: string): Promise<Item>;
  getChildren(id: string): Promise<Item[]>;
  listComments(id: string): Promise<Comment[]>;
  listKbTree(scope: KbScope): Promise<KbNode[]>;
  getPage(scope: KbScope, path: string): Promise<KbPage>;
  readAsset(scope: KbScope, path: string): Promise<Blob>;
  search(query: SearchQuery): Promise<SearchHit[]>;
  validateItem(input: { id?: string; text?: string; path?: string }): Promise<Diagnostic[]>;

  // write (all rev-checked)
  createItem(input: ItemDraft): Promise<Item>;
  updateItem(id: string, patch: ItemPatch, rev: string): Promise<Item>;
  moveItem(id: string, status: ItemStatus, rev: string): Promise<Item>;
  updateMany(ops: UpdateOp[]): Promise<BatchResult>;
  deleteItem(id: string, rev: string): Promise<void>;
  addComment(id: string, body: string, author?: string): Promise<Comment>;
  writePage(scope: KbScope, path: string, content: string, rev?: string): Promise<KbPage>;

  // events
  subscribe(handler: (event: ChangeEvent) => void): Unsubscribe;
}

/** A typed provider failure. Callers switch on `code`, never on an HTTP status. */
export class ProviderError extends Error {
  readonly code: ProviderErrorCode;
  readonly path: string | undefined;

  constructor(code: ProviderErrorCode, message: string, path?: string) {
    super(message);
    this.name = 'ProviderError';
    this.code = code;
    this.path = path;
  }
}

/** Capabilities of the read-only browser fallback; a safe default before detection. */
export const readOnlyCapabilities: Capabilities = {
  write: false,
  git: false,
  ssh: false,
  watch: false,
  fullTextSearch: 'core',
  mcp: false,
  openInEditor: false,
  maxBatchWrite: 0,
};
