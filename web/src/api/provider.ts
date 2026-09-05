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
  BoardDraft,
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
  RetroAction,
  RetroActionDraft,
  RetroActionEdit,
  RetroActionStatus,
  RetroActionView,
  RetroCategory,
  RetroDraft,
  RetroMetrics,
  RetroNote,
  RetroNoteDraft,
  RetroNoteEdit,
  RetroPatch,
  RetroResult,
  RetroState,
  RetroSummary,
  RetroTheme,
  RetroThemeView,
  RetroView,
  SearchHit,
  SnapshotInfo,
  SnapshotItemSummary,
  SnapshotResult,
  Burndown,
  BurndownPoint,
  CumulativeFlow,
  FlowBand,
  FlowPoint,
  FlowStats,
  MetricsProvenance,
  MetricsSource,
  MetricStat,
  SprintCarry,
  SprintCarryAction,
  SprintCarryResult,
  SprintCloseReport,
  SprintMetrics,
  SprintMetricsView,
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
  BoardDraft,
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
  RetroAction,
  RetroActionDraft,
  RetroActionEdit,
  RetroActionStatus,
  RetroActionView,
  RetroCategory,
  RetroDraft,
  RetroMetrics,
  RetroNote,
  RetroNoteDraft,
  RetroNoteEdit,
  RetroPatch,
  RetroResult,
  RetroState,
  RetroSummary,
  RetroTheme,
  RetroThemeView,
  RetroView,
  SearchHit,
  SnapshotInfo,
  SnapshotItemSummary,
  SnapshotResult,
  Burndown,
  BurndownPoint,
  CumulativeFlow,
  FlowBand,
  FlowPoint,
  FlowStats,
  MetricsProvenance,
  MetricsSource,
  MetricStat,
  SprintCarry,
  SprintCarryAction,
  SprintCarryResult,
  SprintCloseReport,
  SprintMetrics,
  SprintMetricsView,
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

/**
 * Commit-on-save settings (docs/06-git-sync.md §3.3, story GIT-US-0020).
 *
 * The same shape in both modes: the companion reads and writes the `git:`
 * section of its configuration file, the browser keeps it per workspace. What
 * differs is `supported` — browser-only mode cannot commit until isomorphic-git
 * arrives with GIT-US-0021 — and the UI branches on that, never on the mode.
 */
export type GitSettings = {
  /** Off by default. */
  commitOnSave: boolean;
  /** How long rapid saves of one item are coalesced for. */
  commitDebounceMs: number;
  /**
   * Go `text/template` source. Both the documented field form
   * (`{{.ItemID}}`) and the short form (`{{action}} {{id}}: {{title}}`) work.
   */
  messageTemplate: string;
  /** What the user configured: `auto`, `go-git` or `system`. */
  backend: 'auto' | 'go-git' | 'system' | 'isomorphic-git';
  /** What `auto` actually resolved to. */
  resolvedBackend: string;
  /** System git version, when the system backend was resolved. */
  gitVersion?: string;
  authorName?: string;
  authorEmail?: string;
  /** Signed commits; the system backend only. */
  signCommits: boolean;
  /** Batched edits waiting to be committed. */
  pending: number;
  /** Whether the last change reached durable storage. */
  persisted?: boolean;
  /**
   * False when this runtime cannot commit at all. `reason` says why, so the UI
   * explains instead of offering a switch that does nothing.
   */
  supported: boolean;
  reason?: string;
};

/** The fields a settings change may carry; an absent one is left alone. */
export type GitSettingsPatch = {
  commitOnSave?: boolean;
  commitDebounceMs?: number;
  messageTemplate?: string;
  authorName?: string;
  authorEmail?: string;
  signCommits?: boolean;
};

/** One repository's git state (`GET /api/v1/git/status`). */
export type GitRepoStatus = {
  repo: string;
  path: string;
  /** False when the folder is not a git working tree; `reason` says so. */
  git: boolean;
  reason?: string;
  backend?: string;
  /** `Name <email>` the commits are attributed to. */
  identity?: string;
  /** Set when no identity resolves, which blocks committing entirely. */
  identityError?: string;
  status?: {
    branch: string;
    detached: boolean;
    clean: boolean;
    staged: string[];
    modified: string[];
    untracked: string[];
  };
  capabilities: {
    backend: string;
    version?: string;
    hooks: boolean;
    signing: boolean;
    credentialHelpers: boolean;
    pathspecCommit: boolean;
  };
};

/** One commit made by commit-on-save or by an explicit commit. */
export type GitCommit = {
  repo: string;
  sha?: string;
  subject?: string;
  /** True when nothing had changed, so no commit was made. */
  empty: boolean;
  paths?: string[];
  /** Machine code of a failure, for example `git_hook_failed`. */
  code?: string;
  message?: string;
};

/**
 * The headline state of one repository, in the precedence the companion
 * resolves it: a blocked repository reads as blocked before it reads as behind
 * (docs/06-git-sync.md §4, story GIT-US-0021).
 */
export type SyncState =
  | 'conflicted'
  | 'in_progress'
  | 'detached'
  | 'no_remote'
  | 'no_upstream'
  | 'diverged'
  | 'behind'
  | 'ahead'
  | 'dirty'
  | 'up_to_date';

/** One path an integration could not merge on its own. */
export type SyncConflict = {
  path: string;
  /** `content`, `delete-modify`, `add-add` or `unknown`. */
  kind: string;
};

/** One commit in a preview or a sync report. */
export type SyncCommit = {
  sha: string;
  subject: string;
  author?: string;
  date?: string;
};

/** One repository's sync state, the row the sync panel renders. */
export type SyncStatus = {
  branch: string;
  detached: boolean;
  clean: boolean;
  /** Uncommitted paths: staged, modified and untracked. */
  dirty?: string[];
  /** True when any dirty path is tracked, which is what blocks an integration. */
  trackedChanges: boolean;
  remote?: string;
  /** The remote URL with any credential removed; never a token. */
  remoteUrl?: string;
  /** The remote-tracking branch, for example `origin/main`. */
  upstream?: string;
  ahead: number;
  behind: number;
  conflicted?: SyncConflict[];
  /** `rebase` or `merge` when one is half-finished, else absent. */
  operation?: string;
  state: SyncState;
};

/** One repository in a sync status listing. */
export type SyncRepoStatus = {
  repo: string;
  path: string;
  /** False when the folder is not a git working tree; `reason` says so. */
  git: boolean;
  reason?: string;
  backend?: string;
  status?: SyncStatus;
  /** Edits commit-on-save has batched; a sync commits them before it fetches. */
  pending: number;
};

/** How a sync runs. Every field is optional; the defaults come from settings. */
export type SyncOptions = {
  /** Preview only: it fetches, which is read-only, and changes nothing else. */
  dryRun?: boolean;
  /** Overrides `pushOnSync` for this run. */
  push?: boolean;
  /** Overrides `pullStrategy`; browser-only mode is always `merge`. */
  strategy?: 'rebase' | 'merge';
};

/** The phase a run ended in. */
export type SyncPhase =
  'preflight' | 'fetch' | 'integrate' | 'push' | 'done' | 'conflicts' | 'failed';

/**
 * One repository's sync report. It is filled on failure too, so the UI can say
 * what happened without inspecting an exception: every failure of the pipeline
 * leaves a recoverable working tree, and `message` says what to do next.
 */
export type SyncResult = {
  repo: string;
  dryRun: boolean;
  strategy: 'rebase' | 'merge';
  phase: SyncPhase;
  before: SyncStatus;
  after: SyncStatus;
  pulled: number;
  pushed: number;
  incoming?: SyncCommit[];
  outgoing?: SyncCommit[];
  conflicts?: SyncConflict[];
  retries: number;
  warnings?: string[];
  durationMs: number;
  /** Machine code of a failure, for example `git_push_rejected`. */
  code?: string;
  message?: string;
};

/**
 * The sync half of the git settings. `supported` is false when this runtime
 * cannot sync at all — browser-only mode without a CORS proxy — and `reason`
 * says why, so the UI explains instead of offering a button that fails
 * (docs/06 §6.3).
 */
export type SyncSettings = {
  pullStrategy: 'rebase' | 'merge';
  pushOnSync: boolean;
  maxPushRetries: number;
  supported: boolean;
  reason?: string;
  /** The configured CORS proxy; browser-only mode, never a credential. */
  corsProxy?: string;
};

/**
 * The conflict resolver (docs/06-git-sync.md §5, story GIT-US-0022).
 *
 * A conflicted file is never handed over as raw conflict markers: the front
 * matter is merged field by field on parsed values and the body hunk by hunk,
 * and every decision the merge made is reported so the user can flip it.
 */
export type ConflictFieldDecision = {
  field: string;
  /** `immutable`, `set`, `ordered`, `order-map`, `scalar`, `timestamp` or `unknown`. */
  kind: string;
  base?: unknown;
  ours?: unknown;
  theirs?: unknown;
  merged?: unknown;
  /** The side the merged value came from: `base`, `ours`, `theirs` or `merged`. */
  choice: string;
  /** True when both sides changed the field, so the decision deserves a look. */
  review: boolean;
  note?: string;
};

/** One region of the body the two sides did not both leave alone. */
export type ConflictHunk = {
  index: number;
  /** The Markdown heading the hunk falls under. */
  section?: string;
  base: string;
  ours: string;
  theirs: string;
  merged: string;
  /** `ours`, `theirs`, `both`, `base`, `merged` or `edited`. */
  choice: string;
  /** True when no rule could pick, so the user has to. */
  conflicted: boolean;
  suggestion?: string;
  note?: string;
};

/** What the core proposes for one conflicted file. */
export type ConflictMerge = {
  path: string;
  /** True when the file has front matter, so the field-level merge applied. */
  structured: boolean;
  fields?: ConflictFieldDecision[];
  hunks?: ConflictHunk[];
  /** The merged file, canonically serialised. */
  content: string;
  conflicted: number;
  review: number;
  clean: boolean;
  warnings?: string[];
};

/** The three versions of a conflicted path, as the index holds them. */
export type ConflictVersions = {
  path: string;
  kind: string;
  base?: string;
  ours?: string;
  theirs?: string;
  hasBase: boolean;
  hasOurs: boolean;
  hasTheirs: boolean;
  /** True when the sides were swapped back into the user's frame (a rebase). */
  rebased?: boolean;
  /** The working copy, conflict markers included: the manual edit starts here. */
  working?: string;
  /** Binary conflicts have no structured resolution: keep mine or keep theirs. */
  binary: boolean;
};

/** Everything the resolver needs for one conflicted path. */
export type ConflictAnalysis = {
  repo: string;
  path: string;
  kind: string;
  operation?: string;
  strategy?: 'rebase' | 'merge';
  versions: ConflictVersions;
  /** Absent for a binary conflict. */
  merge?: ConflictMerge;
};

/** What the user decided for one conflicted path. */
export type ConflictResolution = {
  /** `ours` and `theirs` keep one whole side; `manual` writes `content`. */
  resolution: 'ours' | 'theirs' | 'merged' | 'manual';
  content?: string;
  body?: string;
  /** Field name to `ours`, `theirs` or `base`. */
  fields?: Record<string, string>;
  /** Hunk index, as a string, to `ours`, `theirs`, `both`, `base` or `edited`. */
  hunks?: Record<string, string>;
  /** The text of an `edited` hunk, keyed by the same index. */
  hunkText?: Record<string, string>;
  /** Defaults to true: finish the rebase or merge once nothing is left. */
  continue?: boolean;
};

/** What a resolution did. */
export type ConflictResolveResult = {
  repo: string;
  path: string;
  merge: ConflictMerge;
  result: {
    staged: boolean;
    continued: boolean;
    remaining?: SyncConflict[];
    status?: SyncStatus;
  };
  /** The repository row after the resolution. */
  status?: SyncRepoStatus;
};

/** The sync settings a change may carry; an absent one is left alone. */
export type SyncSettingsPatch = {
  pullStrategy?: 'rebase' | 'merge';
  pushOnSync?: boolean;
  maxPushRetries?: number;
  /** Browser-only mode: the proxy that makes git over HTTPS possible at all. */
  corsProxy?: string;
};

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
  /** The improvement action already became a task (docs/04 R-RETRO-2). */
  | 'retro_action_promoted'
  /** The board slug is already taken; a board file is named after its id. */
  | 'duplicate_id'
  /** A sprint still names the board that was to be deleted. */
  | 'board_in_use'
  /** The documentation folder already holds a `project.yaml` (GIT-US-0031). */
  | 'project_exists'
  /** A write lost a race, or a sprint already has a retro. */
  | 'conflict'
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
  /**
   * Creates a board in the team repository. A board is a view, so creating one
   * adds no item anywhere: the cards it shows are the ones its project scope
   * and its filters select. A slug already taken fails with `duplicate_id`.
   */
  createBoard(draft: BoardDraft): Promise<BoardView>;
  /**
   * Deletes a board file, and nothing else — every item its cards referenced
   * stays where it is. A board a sprint still names fails with `board_in_use`,
   * or `sprint_already_active` when that sprint is running.
   */
  deleteBoard(slug: string, rev?: string): Promise<void>;

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
  /**
   * One sprint's burndown, cumulative flow diagram and flow statistics, with
   * the provenance of the history behind them (docs/04 §12). The provenance is
   * part of the answer, not decoration: the companion reconstructs the series
   * from git, and a host without git says so and shows the approximation it
   * can draw from the `updated` stamps instead of inventing a curve.
   */
  getSprintMetrics(id: string): Promise<SprintMetricsView>;

  // retrospectives (docs/04-team-repository.md §9)
  /**
   * The retros of the team repository, newest first, with the improvement
   * actions they left open. The open actions come back with the listing
   * because a team starting a new retro has to see them first (§9.1, step 7).
   */
  listRetros(filter?: RetroFilter): Promise<RetroListing>;
  /** One retro: its notes, its themes by votes, its actions and what it carried. */
  getRetro(id: string): Promise<RetroView>;
  /** Creates a retro; the core allocates the id from the team key. */
  createRetro(input: RetroDraft): Promise<RetroResult>;
  /**
   * Applies one session's edits. Notes and actions are added, changed and
   * removed one entry at a time, so two participants writing at once produce
   * diffs that merge rather than an entry that disappears.
   */
  updateRetro(id: string, patch: RetroPatch, rev?: string): Promise<RetroResult>;
  /**
   * Turns one improvement action into a task in a project repository, and
   * writes the produced reference back into the retro. A project no open
   * repository serves is refused with `repo_not_cloned` rather than half
   * written, and the UI then offers the action as Markdown to paste
   * (docs/04 R-RETRO-2).
   */
  promoteRetroAction(input: RetroPromotion): Promise<RetroResult>;

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

  // git (docs/06-git-sync.md §3.3)
  /** The effective commit-on-save settings of this runtime. */
  getGitSettings(): Promise<GitSettings>;
  /**
   * Changes them. An invalid message template is refused with
   * `validation_failed` before anything is applied, so a broken template can
   * never reach a commit.
   */
  updateGitSettings(patch: GitSettingsPatch): Promise<GitSettings>;
  /** Per-repository git state: backend, identity and dirty set. */
  getGitStatus(repoId?: string): Promise<GitRepoStatus[]>;
  /**
   * Commits now. With no `paths` it flushes what commit-on-save has batched,
   * which is the "Commit N changes" action of the sync panel.
   */
  commitNow(input?: { repoId?: string; paths?: string[]; message?: string }): Promise<GitCommit[]>;

  // git — sync (docs/06-git-sync.md §4, story GIT-US-0021)
  /**
   * Per-repository sync state: branch, ahead/behind, dirty set, conflicted
   * paths and any half-finished rebase. It never throws for a folder that is
   * not a git working tree: that repository comes back `git: false`.
   */
  getSyncStatus(repoId?: string): Promise<SyncRepoStatus[]>;
  /** The strategy and the push policy this runtime syncs with. */
  getSyncSettings(): Promise<SyncSettings>;
  /**
   * Changes them. Browser-only mode accepts only `corsProxy` — its strategy is
   * forced to `merge` because isomorphic-git has no rebase (docs/06 §6.2) —
   * and the companion accepts the strategy and the push policy.
   */
  updateSyncSettings(patch: SyncSettingsPatch): Promise<SyncSettings>;
  /**
   * Fetch, then rebase or merge, then push. With no `repoId` every repository
   * is synced. A dry run previews the incoming and outgoing commits and
   * changes nothing. A failure is reported in the result's `code` and
   * `message`, not thrown, because the tree is always recoverable.
   */
  sync(repoId: string | undefined, opts?: SyncOptions): Promise<SyncResult[]>;
  /** Undo a half-finished rebase or merge, restoring the tree. */
  abortSync(repoId: string): Promise<SyncRepoStatus>;
  /** The conflicted paths of every repository whose integration stopped. */
  listSyncConflicts(
    repoId?: string,
  ): Promise<{ repo: string; paths: string[]; operation?: string }[]>;

  // git — conflict resolution (docs/06 §5, story GIT-US-0022)
  /**
   * The three versions of one conflicted path plus the merge the core proposes
   * for them: the field decisions, the body hunks and the canonical merged
   * file. It is what the ConflictResolver renders.
   */
  readConflict(repoId: string, path: string): Promise<ConflictAnalysis>;
  /**
   * Writes a resolution, stages it and — unless `continue` is false — finishes
   * the rebase or merge. Keep-mine, keep-theirs and a manual edit are always
   * available, whatever the shape of the conflict.
   */
  resolveConflict(
    repoId: string,
    path: string,
    resolution: ConflictResolution,
  ): Promise<ConflictResolveResult>;

  subscribe(handler: (event: ChangeEvent) => void): Unsubscribe;
}

/** How a retro listing is narrowed; the filters are ANDed. */
export type RetroFilter = { sprint?: string; board?: string; state?: RetroState };

/** A retro listing: the retros and every action they left open. */
export type RetroListing = {
  retros: RetroSummary[];
  carried: RetroActionView[];
  diagnostics: Diagnostic[];
};

/** Promoting one improvement action into a task in a project repository. */
export type RetroPromotion = {
  retro: string;
  action: string;
  project: string;
  /** Overrides the `[retro]` label the task carries (docs/04 R-RETRO-3). */
  labels?: string[];
  rev?: string;
};

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
