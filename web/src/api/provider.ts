/**
 * The data provider boundary (see docs/05-web-app.md §4).
 *
 * Everything above this boundary is identical in both runtime modes. No feature
 * code may import `isomorphic-git`, the WASM bridge or `fetch('/api/...')`
 * directly: features talk to this interface only.
 *
 * Phase 0 declares the interface and the vocabulary. Board, sprint, retro and
 * git members land with their phases (3 and 4) and are intentionally absent
 * here so the scaffold does not promise behaviour that is not designed yet.
 */

export type ProviderKind = 'browser' | 'companion';

export type ItemType = 'epic' | 'story' | 'task' | 'milestone';

export type Priority = 'critical' | 'high' | 'medium' | 'low';

/** Statuses are configured per project in `project.yaml`; the UI never hardcodes them. */
export type ItemStatus = string;

/** `ACME/ACME-US-0042` when serialised. */
export type ItemRef = {
  projectKey: string;
  id: string;
};

export type ItemLinkType = 'blocks' | 'blocked_by' | 'relates_to' | 'duplicates';

export type ItemLink = {
  type: ItemLinkType;
  ref: string;
};

/**
 * Front matter plus an optional body. `rev` is a content hash computed at read
 * time, never stored in the file, and is required by every write.
 */
export type Item = {
  ref: ItemRef;
  type: ItemType;
  title: string;
  status: ItemStatus;
  created: string;
  updated: string;
  author?: string;
  assignees: string[];
  labels: string[];
  priority?: Priority;
  parent?: string;
  milestone?: string;
  estimate?: number;
  effort?: number;
  due?: string;
  links: ItemLink[];
  path: string;
  rev: string;
  body?: string;
};

export type Comment = {
  item: ItemRef;
  author: string;
  created: string;
  body: string;
  path: string;
};

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
};

export type ProjectInfo = {
  key: string;
  name: string;
  repoId: string;
  statuses: string[];
  itemCount: number;
};

export type MountInput = {
  kind: RepoKind;
  /** Companion mode: an absolute path. Browser mode: a picked directory handle id. */
  location: string;
  docsFolder?: string;
};

export type IndexStats = {
  repoId: string;
  filesScanned: number;
  itemsFound: number;
  errors: number;
  durationMs: number;
};

export type SortDirection = 'asc' | 'desc';

export type ItemQuery = {
  projectKey?: string;
  text?: string;
  type?: ItemType[];
  status?: ItemStatus[];
  labels?: string[];
  assignees?: string[];
  priority?: Priority[];
  milestone?: string;
  parent?: string;
  updatedAfter?: string;
  sort?: { field: keyof Item; direction: SortDirection }[];
  limit?: number;
  cursor?: string;
};

export type Page<T> = {
  rows: T[];
  total: number;
  nextCursor?: string;
};

/** A knowledge base scope: a project's docs folder or a team's `knowledge/`. */
export type KbScope = { kind: 'project'; projectKey: string } | { kind: 'team'; teamId: string };

export type KbNode = {
  path: string;
  name: string;
  kind: 'file' | 'directory';
  children?: KbNode[];
};

export type KbPage = {
  scope: KbScope;
  path: string;
  title: string;
  content: string;
  rev: string;
  updated: string;
};

export type SearchQuery = {
  text: string;
  scopes?: KbScope[];
  limit?: number;
};

export type SearchResult = {
  kind: 'item' | 'kb';
  title: string;
  snippet: string;
  path: string;
  ref?: ItemRef;
  score: number;
};

export type CreateItemInput = {
  projectKey: string;
  type: ItemType;
  title: string;
  status?: ItemStatus;
  parent?: string;
  milestone?: string;
  labels?: string[];
  assignees?: string[];
  priority?: Priority;
  body?: string;
};

export type ItemPatch = Partial<
  Pick<
    Item,
    | 'title'
    | 'status'
    | 'assignees'
    | 'labels'
    | 'priority'
    | 'parent'
    | 'milestone'
    | 'estimate'
    | 'effort'
    | 'due'
    | 'links'
    | 'body'
  >
>;

export type UpdateOp = {
  ref: ItemRef;
  patch: ItemPatch;
  rev: string;
};

export type BatchResult = {
  applied: number;
  failed: { ref: ItemRef; code: ProviderErrorCode; message: string }[];
};

export type ProviderErrorCode =
  | 'stale_revision'
  | 'validation_failed'
  | 'not_found'
  | 'read_only'
  | 'git_conflict'
  | 'git_auth_failed'
  | 'repo_not_cloned'
  | 'internal';

export type ChangeEvent =
  | { kind: 'items'; repoId: string; refs: ItemRef[] }
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
  listProjects(): Promise<ProjectInfo[]>;
  mountRepo(input: MountInput): Promise<RepoInfo>;
  unmountRepo(repoId: string): Promise<void>;
  reindex(repoId: string, opts?: { full?: boolean }): Promise<IndexStats>;

  // read
  listItems(query: ItemQuery): Promise<Page<Item>>;
  getItem(ref: ItemRef, opts?: { body?: boolean }): Promise<Item>;
  getChildren(ref: ItemRef): Promise<Item[]>;
  listComments(ref: ItemRef): Promise<Comment[]>;
  listKbTree(scope: KbScope): Promise<KbNode[]>;
  getPage(scope: KbScope, path: string): Promise<KbPage>;
  readAsset(scope: KbScope, path: string): Promise<Blob>;
  search(query: SearchQuery): Promise<SearchResult[]>;

  // write (all rev-checked)
  createItem(input: CreateItemInput): Promise<Item>;
  updateItem(ref: ItemRef, patch: ItemPatch, rev: string): Promise<Item>;
  updateMany(ops: UpdateOp[]): Promise<BatchResult>;
  deleteItem(ref: ItemRef, rev: string): Promise<void>;
  addComment(ref: ItemRef, body: string): Promise<Comment>;
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
