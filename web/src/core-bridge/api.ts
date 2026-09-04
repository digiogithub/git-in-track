/**
 * Contract between the web app and the Go core running inside the WASM worker.
 *
 * Design (docs/05-web-app.md §6, docs/02-architecture.md):
 * - The core cannot call asynchronous browser APIs. The main thread therefore
 *   pushes file contents INTO the worker (`vault.load`, `vault.apply`) and the
 *   core keeps them in its in-memory FS. Every mutating call returns the list
 *   of files the core wrote or removed (`WriteSet`); the main thread persists
 *   them through the File System Access API and acknowledges nothing back —
 *   the in-memory copy is already up to date.
 * - All item shapes mirror the JSON tags of `internal/core/model.go`.
 * - Every method name below is a `CoreRequest.method`; params/results are the
 *   types listed in `CoreApi`.
 */

export type ItemType = 'epic' | 'story' | 'task' | 'milestone' | 'comment';
export type Priority = 'critical' | 'high' | 'medium' | 'low';
export type LinkKind = 'blocks' | 'blocked_by' | 'relates_to' | 'duplicates';
export type Severity = 'error' | 'warning' | 'info';

export type Link = { kind: LinkKind; target: string; note?: string };

export type Item = {
  id: string;
  type: ItemType;
  title: string;
  status?: string;
  priority?: Priority;
  parent?: string;
  epic?: string;
  milestone?: string;
  sprint?: string;
  assignees?: string[];
  author?: string;
  owner?: string;
  labels?: string[];
  estimate?: number;
  effort?: number;
  spent?: number;
  created?: string;
  updated?: string;
  started?: string;
  closed?: string;
  start?: string;
  due?: string;
  links?: Link[];
  attachments?: string[];
  custom?: Record<string, unknown>;
  deleted?: boolean;
  extra?: Record<string, unknown>;
  /** Markdown body after the front matter. Omitted by list calls unless `fields` asks for it. */
  body: string;
  /** Vault-relative path, forward slashes. */
  path: string;
  /** Content hash used for optimistic concurrency (`sha256:` + 16 hex). */
  rev: string;
};

export type Comment = {
  item: string;
  author: string;
  created?: string;
  updated?: string;
  inReplyTo?: string;
  kind?: string;
  body: string;
  path: string;
  rev: string;
};

export type Diagnostic = {
  code: string;
  severity: Severity;
  message: string;
  path?: string;
  field?: string;
};

export type VaultFile = { path: string; text: string };

export type FileEvent = {
  op: 'create' | 'write' | 'remove' | 'rename';
  path: string;
  /** Present for `create`/`write`; the new content. */
  text?: string;
  /** Present for `rename`; the previous path. */
  from?: string;
};

/** Files the core changed during a mutating call; the host must persist them. */
export type WriteSet = {
  written: VaultFile[];
  removed: string[];
};

export type ProjectSummary = {
  key: string;
  name: string;
  /** Vault-relative path of the folder that contains `.pmngr/`. */
  docsPath: string;
  statuses: { id: string; name: string; category: string; terminal?: boolean; wip?: number }[];
  labels: { name: string; color?: string; description?: string }[];
  priorities: Priority[];
  itemCounts: Record<ItemType, number>;
  /** Repository the project was discovered in; set by a workspace-wide answer. */
  vaultId?: string;
  /** Initial status and transition map (`from -> [to...]`); absent transitions mean any. */
  workflow?: { initial?: string; transitions?: Record<string, string[]> };
  /** `project.yaml` estimation settings. */
  estimation?: { scale?: string; values?: number[]; trackHours?: boolean };
  /** Declared custom fields (`project.yaml` `custom_fields`). */
  customFields?: {
    key: string;
    type: string;
    values?: string[];
    items?: string;
    appliesTo?: ItemType[];
    default?: unknown;
    description?: string;
  }[];
};

export type ItemFilter = {
  project?: string;
  type?: ItemType | ItemType[];
  status?: string | string[];
  category?: string | string[];
  priority?: Priority | Priority[];
  assignee?: string;
  label?: string | string[];
  parent?: string;
  milestone?: string;
  updatedSince?: string;
  text?: string;
  includeDeleted?: boolean;
  sort?: 'updated' | 'created' | 'priority' | 'id' | 'title';
  order?: 'asc' | 'desc';
  limit?: number;
  cursor?: string;
  /** Front-matter fields to include; omit for all front matter without body. */
  fields?: string[];
};

export type ItemPage = { items: Item[]; nextCursor?: string; total: number };

export type ItemDraft = {
  project: string;
  type: Exclude<ItemType, 'comment'>;
  title: string;
  status?: string;
  priority?: Priority;
  parent?: string;
  milestone?: string;
  assignees?: string[];
  author?: string;
  labels?: string[];
  estimate?: number;
  due?: string;
  links?: Link[];
  custom?: Record<string, unknown>;
  body?: string;
};

export type ItemPatch = {
  set?: Partial<Omit<Item, 'id' | 'type' | 'path' | 'rev' | 'body'>>;
  unset?: string[];
  body?: string;
};

export type KbNode = {
  path: string;
  name: string;
  kind: 'dir' | 'page' | 'asset';
  title?: string;
  children?: KbNode[];
};

export type KbPage = {
  path: string;
  title: string;
  frontMatter: Record<string, unknown>;
  body: string;
  rev: string;
  outgoing: string[];
  backlinks: string[];
};

export type SearchHit = {
  kind: 'item' | 'page';
  id?: string;
  path: string;
  title: string;
  snippet: string;
  score: number;
  /** Project key the hit belongs to; the team key for a team knowledge-base page. */
  project?: string;
  /** Repository the hit came from, set by a workspace-wide search. */
  vaultId?: string;
};

/** One member of `team.yaml` (docs/04-team-repository.md §3.2). */
export type TeamMember = {
  handle: string;
  name?: string;
  role?: string;
  emails?: string[];
  gitNames?: string[];
  handles?: Record<string, string>;
  capacity?: number;
  active: boolean;
};

/**
 * One project declared in `team.yaml`, plus what the workspace knows about it
 * locally. `cloned: false` is the normal state of a project nobody on this
 * machine has checked out (docs/04 §7).
 */
export type TeamProjectSummary = {
  key: string;
  name: string;
  repo: string;
  defaultBranch?: string;
  docsPath: string;
  host?: string;
  webUrl?: string;
  color?: string;
  archived?: boolean;
  localHints?: string[];
  cloned: boolean;
  vaultId?: string;
  localDocsPath?: string;
  /** The committed index snapshot of this project, present or not. */
  snapshot: SnapshotInfo;
  /** Where the project can be browsed; empty disables the host links. */
  browseUrl?: string;
  diagnostics?: Diagnostic[];
};

/** The team repository of the workspace (docs/04 §3). */
export type TeamSummary = {
  key: string;
  name: string;
  description?: string;
  timezone?: string;
  root: string;
  knowledgePath: string;
  vaultId?: string;
  members: TeamMember[];
  projects: TeamProjectSummary[];
  policies?: Record<string, string>;
  cadence: { sprintLengthDays?: number; sprintStartWeekday?: string; retroAfterSprint?: boolean };
  defaults: { board?: string; sprintLengthDays?: number; capacityHoursPerDay?: number };
  snapshots: { enabled: boolean; maxAgeDays?: number; includeClosed?: boolean };
  diagnostics: Diagnostic[];
};

/** Where a `<projectKey>/<itemId>` reference points, and whether it can be read. */
export type RefResolution = {
  ref: string;
  project: string;
  item: string;
  /** `team.yaml` lists the project. */
  declared: boolean;
  /** A repository exposing the project is open. */
  cloned: boolean;
  vaultId?: string;
  /** The item itself, without its body; absent when the reference is remote. */
  found?: Item;
  /** The read-only summary a committed snapshot carries for a remote item. */
  snapshot?: SnapshotItemSummary;
  /** The file that summary came from. */
  snapshotInfo?: SnapshotInfo;
  /** The item's file on the git host, empty when no link can be built. */
  url?: string;
  /** One sentence explaining an unresolved reference. */
  reason?: string;
};

/** One item of a committed snapshot: front-matter-derived fields only. */
export type SnapshotItemSummary = {
  id: string;
  type: ItemType;
  title: string;
  status?: string;
  category?: string;
  priority?: Priority;
  parent?: string;
  milestone?: string;
  sprint?: string;
  assignees?: string[];
  labels?: string[];
  estimate?: number;
  due?: string;
  updated?: string;
  path: string;
  rev: string;
  ac?: { total: number; done: number };
};

/** What happened to one project's snapshot during a refresh. */
export type SnapshotResult = {
  project: string;
  path: string;
  status: 'written' | 'unchanged' | 'skipped';
  items: number;
  reason?: string;
  info: SnapshotInfo;
};

export type BoardKind = 'kanban' | 'scrum';

/** One card of a rendered board (docs/04-team-repository.md §5). */
export type BoardCard = {
  /** `<projectKey>/<itemId>`. */
  ref: string;
  project: string;
  item: string;
  /** `team.yaml` declares the project; an undeclared ref renders as inert text. */
  declared: boolean;
  /** No open repository serves the project: the card is read-only (docs/04 §7). */
  remote: boolean;
  vaultId?: string;
  title?: string;
  type?: ItemType;
  status?: string;
  priority?: Priority;
  assignees?: string[];
  labels?: string[];
  estimate?: number;
  milestone?: string;
  parent?: string;
  due?: string;
  updated?: string;
  path?: string;
  rev?: string;
  /**
   * Where the card was read from: `live` for a local clone, `snapshot` for the
   * committed `.pmngr/index/<projectKey>.json` of the team repository. Absent
   * on a remote card no snapshot could resolve (docs/04 §6).
   */
  source?: CardSource;
  /** When the snapshot the card came from was generated. */
  snapshotAt?: string;
  /** The snapshot is older than the team's `snapshots.max_age_days`. */
  stale?: boolean;
  /** The item's file on the git host, absent when no link can be built. */
  remoteUrl?: string;
  /** One sentence explaining why the card cannot be edited here. */
  reason?: string;
};

/** Where a card's fields came from. */
export type CardSource = 'live' | 'snapshot';

/** How old a committed snapshot is, graded against the team policy (R-SNAP-9). */
export type SnapshotFreshness = 'unknown' | 'fresh' | 'ageing' | 'stale';

/** What is known about one project's committed index snapshot (docs/04 §6). */
export type SnapshotInfo = {
  project: string;
  /** Where the file lives, or would live, in the team repository. */
  path: string;
  present: boolean;
  /** The team publishes snapshots at all (`snapshots.enabled`). */
  enabled: boolean;
  generated?: string;
  generatedBy?: string;
  generator?: string;
  commit?: string;
  /** The snapshot was generated from a dirty working tree (R-SNAP-4). */
  dirty?: boolean;
  items: number;
  ageSeconds?: number;
  freshness: SnapshotFreshness;
  stale: boolean;
  /** Why a file that exists could not be used. */
  error?: string;
};

/** One rendered column, with the live WIP condition recomputed on every read. */
export type BoardColumnView = {
  id: string;
  name: string;
  /** 0 or absent means unlimited. */
  wip?: number;
  color?: string;
  collapsed?: boolean;
  cards: BoardCard[];
  /** The column holds more cards than its limit allows. */
  exceeded: boolean;
};

export type BoardFilters = {
  projects?: string[];
  types?: ItemType[];
  labelsAny?: string[];
  labelsAll?: string[];
  labelsNone?: string[];
  assignees?: string[];
  priorities?: Priority[];
  milestone?: string;
  sprint?: string;
  dueBefore?: string;
  updatedSince?: string;
  includeClosed?: boolean;
  query?: string;
};

/** A board plus the cards it currently shows. */
export type BoardView = {
  id: string;
  kind: BoardKind;
  title: string;
  description?: string;
  path: string;
  rev: string;
  teamVaultId?: string;
  projects: string[];
  filters: BoardFilters;
  swimlanes: { by?: string; order?: string[]; collapseEmpty?: boolean };
  card: { show?: string[] };
  sprint?: string;
  backlogColumn?: string;
  columns: BoardColumnView[];
  /** Items whose status maps to no column: surfaced, never hidden (R-COL-4). */
  unmapped: BoardCard[];
  body?: string;
  diagnostics: Diagnostic[];
};

/** One entry of the board index. */
export type BoardSummary = {
  id: string;
  kind: BoardKind;
  title: string;
  description?: string;
  path: string;
  rev: string;
  vaultId?: string;
  projects: string[];
  columns: number;
  sprint?: string;
  diagnostics: Diagnostic[];
};

/** What a card move implied, echoed back so the UI can explain it. */
export type BoardMovePlan = {
  ref: string;
  fromColumn?: string;
  toColumn: string;
  status?: string;
  statusChanged: boolean;
  /** Every status the target column maps for this project. */
  choices?: string[];
  wip: { column: string; used: number; limit: number; exceeded: boolean };
};

/** A `WriteSet` plus the repository it belongs to. */
export type VaultWriteSet = { vaultId: string } & WriteSet;

export type BoardMoveResult = {
  board: BoardView;
  /** Present only when the move changed a status. */
  item?: Item;
  move: BoardMovePlan;
  /** One entry per repository written: the item's clone and the team repo. */
  writes: VaultWriteSet[];
};

/** One repository of the workspace. */
export type WorkspaceVault = {
  id: string;
  role: 'project' | 'team';
  label: string;
  projects: string[];
  team: boolean;
  teamKey?: string;
  stats: IndexStats;
};

/** Every open repository, the team among them, and the cross-repository findings. */
export type WorkspaceSummary = {
  vaults: WorkspaceVault[];
  team?: TeamSummary;
  diagnostics: Diagnostic[];
};

export type IndexStats = {
  projects: number;
  items: number;
  pages: number;
  comments: number;
  durationMs: number;
  fingerprint: string;
  diagnostics: Diagnostic[];
};

export type SnapshotBlob = { fingerprint: string; json: string };

/** Method map: request method name → { params, result }. */
export type CoreApi = {
  ping: { params: undefined; result: { pong: true; wasm: boolean } };
  version: { params: undefined; result: { protocol: number; core: string | null } };

  /**
   * Replace the in-memory vault with these files (full load). `vaultId` names
   * the repository inside the workspace; a call that omits it goes to the
   * default one, and a `vault.load` for an unknown id creates it.
   */
  'vault.load': {
    params: { files: VaultFile[]; rootLabel?: string; vaultId?: string };
    result: IndexStats;
  };
  /** Apply incremental file events (from a rescan diff or a watcher). */
  'vault.apply': { params: { events: FileEvent[]; vaultId?: string }; result: IndexStats };
  'vault.stats': { params: { vaultId?: string } | undefined; result: IndexStats };
  /** Serialised index for the IndexedDB cache; `snapshot.load` hydrates without files. */
  'snapshot.export': { params: { vaultId?: string } | undefined; result: SnapshotBlob };
  'snapshot.load': { params: SnapshotBlob & { vaultId?: string }; result: IndexStats };

  /** Every open repository, plus the team repository among them. */
  'workspace.list': { params: undefined; result: WorkspaceSummary };
  /** Open an empty repository the host then fills with `vault.load`. */
  'workspace.mount': {
    params: { vaultId: string; role?: 'project' | 'team'; rootLabel?: string };
    result: WorkspaceVault;
  };
  /** Drop a repository from the workspace; it never touches files. */
  'workspace.unmount': { params: { vaultId: string }; result: { unmounted: string } };

  /** The team repository of the workspace; fails with `not_found` when none is open. */
  'team.get': { params: undefined; result: TeamSummary };
  /** Resolve `<projectKey>/<itemId>` across every open repository. */
  'ref.resolve': { params: { ref: string }; result: RefResolution };

  'project.list': { params: undefined; result: ProjectSummary[] };

  /** Every board of the team repository. */
  'board.list': { params: undefined; result: { boards: BoardSummary[]; diagnostics: Diagnostic[] } };
  /** One board, rendered over every open repository. */
  'board.get': { params: { board: string }; result: BoardView };
  /**
   * Move one card. It writes the item's status in its own project repository
   * and the board's `order:` list in the team repository, and nothing else
   * (docs/04 R-MOVE-1). A move that would exceed a WIP limit fails once with
   * `wip_limit_exceeded`; repeat it with `force` to confirm.
   */
  'board.move': {
    params: {
      board: string;
      ref: string;
      toColumn: string;
      /** 0-based index in the target column; -1 appends. */
      position: number;
      /** Overrides the status the column mapping would pick. */
      status?: string;
      /** Board revision the caller read. */
      rev?: string;
      /** Item revision the caller read. */
      itemRev?: string;
      force?: boolean;
    };
    result: BoardMoveResult;
  };

  /**
   * The committed index snapshot of every project the team declares, with its
   * age and its staleness (docs/04 §6).
   */
  'snapshot.list': {
    params: undefined;
    result: { snapshots: SnapshotResult[]; writes: VaultWriteSet[]; dryRun?: boolean };
  };
  /**
   * Regenerate the snapshots of the projects an open repository serves and
   * write the ones whose content changed into the team repository. A file that
   * did not change is not rewritten, so a refresh that finds nothing new
   * produces no commit.
   */
  'snapshot.refresh': {
    params: {
      projects?: string[];
      generatedBy?: string;
      includeClosed?: boolean;
      dryRun?: boolean;
    };
    result: { snapshots: SnapshotResult[]; writes: VaultWriteSet[]; dryRun?: boolean };
  };

  'item.list': { params: ItemFilter; result: ItemPage };
  'item.get': { params: { id: string }; result: Item };
  'item.children': { params: { id: string }; result: Item[] };
  'item.create': { params: ItemDraft; result: { item: Item; writes: WriteSet } };
  'item.update': {
    params: { id: string; patch: ItemPatch; rev: string };
    result: { item: Item; writes: WriteSet };
  };
  'item.move': {
    params: { id: string; status: string; rev: string };
    result: { item: Item; writes: WriteSet };
  };
  'item.delete': {
    params: { id: string; rev: string; hard?: boolean };
    result: { writes: WriteSet };
  };
  'item.validate': { params: { id?: string; text?: string; path?: string }; result: Diagnostic[] };
  'item.parse': { params: { path: string; text: string }; result: Item };
  'item.serialize': { params: { item: Item }; result: { text: string } };

  'comment.list': { params: { id: string }; result: Comment[] };
  'comment.add': {
    params: { id: string; author: string; body: string; inReplyTo?: string };
    result: { comment: Comment; writes: WriteSet };
  };

  'kb.tree': { params: { project?: string; vaultId?: string }; result: KbNode[] };
  'kb.page': { params: { path: string; vaultId?: string }; result: KbPage };
  'kb.write': {
    params: { path: string; text: string; rev?: string; vaultId?: string };
    result: { page: KbPage; writes: WriteSet };
  };

  search: { params: { q: string; limit?: number; project?: string }; result: SearchHit[] };
};

export type CoreMethodName = keyof CoreApi;
export type CoreParams<M extends CoreMethodName> = CoreApi[M]['params'];
export type CoreResult<M extends CoreMethodName> = CoreApi[M]['result'];
