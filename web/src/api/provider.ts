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
 * The board members arrived with GIT-US-0017; sprint, retro and git members
 * land with their stories.
 */

import type {
  BoardCard,
  BoardColumnPatch,
  BoardColumnView,
  BoardPatch,
  BoardMovePlan,
  BoardMoveResult,
  BoardSummary,
  BoardView,
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
  SnapshotInfo,
  SnapshotItemSummary,
  SnapshotResult,
  SprintCarry,
  SprintCarryAction,
  SprintCarryResult,
  SprintCloseReport,
  SprintMetrics,
  SprintResult,
  SprintState,
  SprintSummary,
  SprintView,
  StatusCategory,
  TeamMember,
  TeamProjectSummary,
  TeamSummary,
  WorkspaceSummary,
  WorkspaceVault,
} from '@/core-bridge/api';

export type {
  BoardCard,
  BoardColumnPatch,
  BoardColumnView,
  BoardPatch,
  BoardMovePlan,
  BoardMoveResult,
  BoardSummary,
  BoardView,
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
  SnapshotInfo,
  SnapshotItemSummary,
  SnapshotResult,
  SprintCarry,
  SprintCarryAction,
  SprintCarryResult,
  SprintCloseReport,
  SprintMetrics,
  SprintResult,
  SprintState,
  SprintSummary,
  SprintView,
  StatusCategory,
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
  /** A move would put a column over its WIP limit; confirm it to go through. */
  | 'wip_limit_exceeded'
  /** Two sprints of one board would share a day (docs/04 §8.4). */
  | 'sprint_overlap'
  /** The board already runs a sprint; confirm to run two at once. */
  | 'sprint_already_active'
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

  // boards (docs/04-team-repository.md §5)
  /** Every board of the team repository; empty when none is open. */
  listBoards(): Promise<BoardSummary[]>;
  /**
   * One board, rendered over every open repository. A card whose project
   * nobody cloned comes back `remote: true` with a reason, never missing.
   */
  getBoard(slug: string): Promise<BoardView>;
  /**
   * Moves one card. It writes the item's status in its own project repository
   * and the board's `order:` list in the team repository, and nothing else.
   * A move that would exceed a WIP limit fails with `wip_limit_exceeded`
   * unless `force` confirms it.
   */
  moveCard(move: CardMove): Promise<BoardMoveResult>;
  /**
   * Edits the board file itself: columns, WIP limits, filters, and the sprint
   * a scrum board is scoped to. The card order is never patched here — it
   * moves one card at a time through `moveCard`.
   */
  updateBoard(slug: string, patch: BoardPatch, rev?: string): Promise<BoardView>;

  // sprints (docs/04-team-repository.md §8)
  /** The sprints of the team repository, newest ids last; empty when none. */
  listSprints(filter?: SprintFilter): Promise<SprintSummary[]>;
  /** One sprint: its scope, the candidates for it and its metrics. */
  getSprint(id: string): Promise<SprintView>;
  /** Creates a sprint; the core allocates the id from the team key. */
  createSprint(input: SprintDraft): Promise<SprintResult>;
  /**
   * Changes the goal, the dates or the scope. Every change is one write to the
   * sprint file in the team repository, so moving an item in or out of a
   * sprint stays legal for a project nobody cloned (docs/04 R-SPR-2).
   */
  updateSprint(id: string, patch: SprintPatch, rev?: string): Promise<SprintResult>;
  /**
   * Makes a sprint active: its scope becomes its commitment and its board is
   * pointed at it. A board already running a sprint is refused once with
   * `sprint_already_active`; repeat with `force` to run two at once.
   */
  startSprint(id: string, rev?: string, force?: boolean): Promise<SprintResult>;
  /**
   * Closes a sprint and reports completed against incomplete work. Closing
   * modifies no item by itself: `carry` carries one explicit decision per
   * unfinished item (R-SPR-3).
   */
  closeSprint(id: string, carry?: SprintCarry[], rev?: string): Promise<SprintResult>;

  // index snapshots (docs/04-team-repository.md §6)
  /**
   * The committed `.pmngr/index/<projectKey>.json` of every project the team
   * declares: whether there is one, when it was generated and how stale it is.
   */
  listSnapshots(): Promise<SnapshotResult[]>;
  /**
   * Regenerates the snapshots of the projects an open repository serves and
   * writes the ones whose content changed into the team repository. A project
   * nobody cloned comes back `skipped` with a reason.
   */
  refreshSnapshots(input?: SnapshotRefresh): Promise<SnapshotResult[]>;

  // events
  subscribe(handler: (event: ChangeEvent) => void): Unsubscribe;
}

/** How a sprint listing is narrowed; both filters are ANDed. */
export type SprintFilter = { board?: string; state?: SprintState };

/** A new sprint. Dates are required; the id is allocated by the core. */
export type SprintDraft = {
  board: string;
  start: string;
  end: string;
  title?: string;
  goal?: string;
  state?: SprintState;
  items?: string[];
  capacityHours?: number;
  velocityTarget?: number;
  participants?: string[];
  author?: string;
};

/** The sprint fields an update may change; an absent one is left alone. */
export type SprintPatch = {
  title?: string;
  goal?: string;
  start?: string;
  end?: string;
  state?: SprintState;
  capacityHours?: number;
  velocityTarget?: number;
  participants?: string[];
  items?: string[];
  /** Adds and removes edit the scope without resending it. */
  addItems?: string[];
  removeItems?: string[];
};

/** What to regenerate on a snapshot refresh. */
export type SnapshotRefresh = {
  /** Project keys to limit the run to; empty means every cloned project. */
  projects?: string[];
  /** Handle recorded in the file. */
  generatedBy?: string;
  /** Overrides the team's `snapshots.include_closed` for this run. */
  includeClosed?: boolean;
  /** Reports what would change without writing anything. */
  dryRun?: boolean;
};

/** One card move: where the card goes and which locks the caller holds. */
export type CardMove = {
  board: string;
  /** `<projectKey>/<itemId>`. */
  ref: string;
  toColumn: string;
  /** 0-based index in the target column; -1 appends. */
  position: number;
  /** Overrides the status the column mapping would pick. */
  status?: string;
  /** Board revision the user was looking at. */
  rev?: string;
  /** Item revision the user was looking at. */
  itemRev?: string;
  /** Confirms a move over a WIP limit, and an undeclared transition. */
  force?: boolean;
};

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
