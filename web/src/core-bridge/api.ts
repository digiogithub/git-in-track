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
  /** The handle of the file name, `[a-z0-9-]+`. */
  author: string;
  /** The git `user.name` the comment was written as, when it was known. */
  authorName?: string;
  /** The git `user.email` the comment was written as, when it was known. */
  authorEmail?: string;
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

/**
 * One feedback note on a knowledge-base page. Lines are 1-based lines of
 * `KbPage.body`, the text after the front matter.
 */
export type KbFeedbackNoteDraft = {
  startLine: number;
  endLine: number;
  /** The text the reader selected, as rendered. */
  quote?: string;
  note: string;
};

/** What the core recorded for one note: its id and the anchor it checks. */
export type KbFeedbackNoteRef = {
  id: string;
  anchor: string;
  startLine: number;
  endLine: number;
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

/**
 * Names the team repository a call acts on: the `key:` of a team.yaml, or the
 * id of the repository holding it. It is optional while the workspace holds a
 * single team and required as soon as it holds two (GIT-US-0036, ADR-020).
 */
export type TeamScoped = { team?: string };

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

/** The coarse bucket of a status in a project workflow (docs/03 §6.1). */
export type StatusCategory = 'todo' | 'in_progress' | 'done' | 'cancelled';

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
  /** The coarse bucket of `status` in the card's own project workflow. */
  category?: StatusCategory;
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
  /** The scrum board's sprint lists this card (docs/04 §8.2). */
  inSprint?: boolean;
  /** The sprint committed to this card when it started, as opposed to pulling
   * it in mid-sprint (R-SPR-1). */
  committed?: boolean;
  /** A sprint candidate: the board's filters match it and the sprint does not
   * list it, so it sits in the `backlog_column` (docs/04 §5.5). */
  backlog?: boolean;
  /** One sentence explaining why the card cannot be edited here. */
  reason?: string;
};

/** The lifecycle of a sprint (docs/04 §8.2). */
export type SprintState = 'planned' | 'active' | 'closed';

/** What a sprint header and a closing report count. */
export type SprintMetrics = {
  items: number;
  resolved: number;
  done: number;
  points: number;
  committedPoints: number;
  donePoints: number;
  /** References pulled in after the sprint started. */
  added: number;
  /** References neither a clone nor a snapshot could render. */
  unresolved: number;
};

/** A sprint as the UI reads it: the file plus the numbers it resolves to. */
export type SprintSummary = {
  id: string;
  title: string;
  board: string;
  state: SprintState;
  start?: string;
  end?: string;
  goal?: string;
  capacityHours?: number;
  velocityTarget?: number;
  participants?: string[];
  retro?: string;
  items: string[];
  committed?: string[];
  /** Both ends inclusive; `remainingDays` is 0 once the end date has passed. */
  totalDays: number;
  remainingDays: number;
  metrics: SprintMetrics;
  body?: string;
  path?: string;
  rev?: string;
};

/** The planning view of one sprint: its scope and the candidates for it. */
export type SprintView = {
  sprint: SprintSummary;
  /** The scope, in the order the sprint file lists it. */
  cards: BoardCard[];
  /** What the board would show that the sprint does not list. */
  backlog: BoardCard[];
  diagnostics: Diagnostic[];
};

/**
 * Where the observations behind a metric came from (docs/04 §12, ADR-017).
 *
 * `git` is the real thing: every revision of every item file, reconstructed
 * from the commits. `updated` is the approximation a host without git falls
 * back to — each item is assumed to have held its current status since its
 * `updated` stamp, and nothing is claimed about the time before that. `none`
 * is no history at all.
 */
export type MetricsSource = 'git' | 'updated' | 'none';

/** The honesty half of a metric: where it came from and what it may not be asked. */
export type MetricsProvenance = {
  source: MetricsSource;
  /** True for anything but a complete git reconstruction. */
  approximate: boolean;
  /** The earliest day the history can speak for, `YYYY-MM-DD`. */
  from?: string | null;
  commits?: number;
  truncated?: boolean;
  /** The size of the scope, and how many of it the history covers. */
  items: number;
  covered: number;
  /** One sentence for the UI. Always present, always shown. */
  note: string;
};

/** One band of a cumulative flow diagram, in stacking order, bottom first. */
export type FlowBand = 'done' | 'cancelled' | 'in_progress' | 'todo' | 'unknown';

/** One day of a burndown. Only an observed day carries measurements. */
export type BurndownPoint = {
  date: string;
  /** 1-based: day 1 is the first day of the sprint. */
  day: number;
  /** The straight line from the commitment to zero; it exists for every day. */
  ideal: number;
  /** A day on or before today: the only kind a chart may plot. */
  observed: boolean;
  remaining: number;
  scope: number;
  done: number;
  items: number;
  completed: number;
  /** References whose state that day the history cannot state. */
  unknown: number;
};

/** The remaining work of a sprint per day against the ideal line. */
export type Burndown = {
  sprint: string;
  start?: string | null;
  end?: string | null;
  committedPoints: number;
  points: BurndownPoint[];
};

/** One day of a cumulative flow diagram. */
export type FlowPoint = {
  date: string;
  day: number;
  observed: boolean;
  counts: Record<FlowBand, number>;
  total: number;
};

/** Item counts by status band over the sprint window. */
export type CumulativeFlow = { bands: FlowBand[]; days: FlowPoint[] };

/** A sample of durations in days. */
export type MetricStat = {
  count: number;
  mean: number;
  median: number;
  p85: number;
  min: number;
  max: number;
};

/** Cycle time, lead time and throughput, each with the sample behind it. */
export type FlowStats = {
  throughput: number;
  throughputPerWeek: number;
  cycleTime: MetricStat;
  leadTime: MetricStat;
  /** Finished references no duration could be measured for. */
  excluded: number;
};

/** One sprint's metrics: both charts, the flow numbers and their provenance. */
export type SprintMetricsView = {
  sprint: SprintSummary;
  burndown: Burndown;
  flow: CumulativeFlow;
  stats: FlowStats;
  provenance: MetricsProvenance;
  /** The scope, so every chart has its data table without a second call. */
  items: BoardCard[];
};

/** What happens to one unfinished item when a sprint closes (R-SPR-3). */
export type SprintCarryAction = 'leave' | 'next' | 'backlog';

/** One closing decision. */
export type SprintCarry = {
  ref: string;
  action: SprintCarryAction;
  /** The sprint to carry into; empty picks the next planned one. */
  sprint?: string;
  /** Overrides the status a `backlog` decision writes. */
  status?: string;
};

/** The outcome of one closing decision. */
export type SprintCarryResult = {
  ref: string;
  action: SprintCarryAction;
  sprint?: string;
  status?: string;
  /** A decision that could not be applied; the rest still went through. */
  error?: string;
};

/** What closing a sprint summarised. */
export type SprintCloseReport = {
  sprint: string;
  board: string;
  completed: BoardCard[];
  incomplete: BoardCard[];
  /** References neither a clone nor a snapshot could grade. */
  unresolved: BoardCard[];
  completedPoints: number;
  incompletePoints: number;
  metrics: SprintMetrics;
  carried: SprintCarryResult[];
};

/** The answer of every sprint call that writes. */
export type SprintResult = {
  sprint: SprintView;
  /** Present when the write touched the board as well. */
  board?: BoardView;
  /** Present when the sprint was closed. */
  report?: SprintCloseReport;
  writes: VaultWriteSet[];
};

/** The facilitation stage of a retro (docs/04 §9.2). */
export type RetroState = 'collecting' | 'voting' | 'discussing' | 'closed';

/** The column a retro note and its theme belong to (docs/04 §9.1). */
export type RetroCategory = 'went_well' | 'to_improve' | 'puzzle';

/** The retro-local bookkeeping of an improvement action (docs/04 §9.2). */
export type RetroActionStatus = 'proposed' | 'promoted' | 'done' | 'dropped';

/** One sticky note: a body bullet of a collection section. */
export type RetroNote = {
  /** The `(n1)` prefix; absent on a bullet somebody typed by hand. */
  id?: string;
  category: RetroCategory;
  text: string;
  /** Absent on an anonymous retro. */
  author?: string;
};

/** A group of notes the room merged into one topic. */
export type RetroTheme = {
  id: string;
  title: string;
  category?: RetroCategory;
  /** Ids of the notes this theme absorbed. */
  notes?: string[];
};

/** One improvement action as the retro file stores it. */
export type RetroAction = {
  id: string;
  title: string;
  /** The single accountable handle; an action without one is a warning. */
  owner?: string;
  due?: string;
  theme?: string;
  /** `<projectKey>/<itemId>`, written by a promotion and by nothing else. */
  task?: string;
  status?: RetroActionStatus;
  note?: string;
};

/** One theme plus what the room decided about it. */
export type RetroThemeView = RetroTheme & {
  votes: number;
  voters?: string[];
  /** The notes this theme grouped, resolved from the body. */
  noteTexts?: RetroNote[];
  /** Ids of the improvement actions that came out of it. */
  actions?: string[];
};

/**
 * One improvement action plus its live state. Once the action carries a `task`
 * that task's status in the project repository is the truth, and `done` is
 * graded from the card rather than from `status` (docs/04 R-RETRO-1).
 */
export type RetroActionView = RetroAction & {
  /** The retro the action belongs to. */
  retro: string;
  retroTitle?: string;
  /** The promoted task, live, from a snapshot, or unresolved with a reason. */
  card?: BoardCard;
  done: boolean;
  /** Neither done nor dropped: exactly what the next retro has to review. */
  open: boolean;
  reason?: string;
};

/** How well one retro was followed through. */
export type RetroMetrics = {
  actions: number;
  promoted: number;
  done: number;
  open: number;
  dropped: number;
  /** Actions nobody is accountable for (docs/04 R-RETRO-4). */
  noOwner: number;
};

/** A retro as an index reads it. */
export type RetroSummary = {
  id: string;
  title: string;
  sprint?: string;
  board?: string;
  date?: string;
  facilitator?: string;
  participants?: string[];
  state: RetroState;
  anonymous?: boolean;
  voteBudget: number;
  carriedFrom?: string;
  notes: number;
  themes: number;
  metrics: RetroMetrics;
  actions: RetroAction[];
  path?: string;
  rev?: string;
};

/** One retro as the UI runs it. */
export type RetroView = {
  retro: RetroSummary;
  /** The body bullets, in document order. */
  notes: RetroNote[];
  /** Ranked by votes descending, then by id. */
  themes: RetroThemeView[];
  actions: RetroActionView[];
  /** The discussion remarks, each naming the card it hangs off. */
  comments: RetroComment[];
  /** The still-open actions of the retros before this one. */
  carried: RetroActionView[];
  /** The sprint under review, when the team repository holds it. */
  sprint?: SprintSummary;
  body?: string;
  diagnostics: Diagnostic[];
};

/** One remark left on a note or a theme while the room discusses it. */
export type RetroComment = {
  id: string;
  /** The sticky note this comment discusses. */
  note?: string;
  /** The theme this comment discusses. */
  theme?: string;
  author?: string;
  text: string;
  created?: string;
};

/** A new comment. Exactly one of `note` and `theme` is set. */
export type RetroCommentDraft = {
  id?: string;
  note?: string;
  theme?: string;
  author?: string;
  text: string;
};

/** One comment edited during the session; its target is never moved. */
export type RetroCommentEdit = { id: string; text?: string };

/** The answer of every retro call that writes. */
export type RetroResult = {
  retro: RetroView;
  /** The task a promotion created, in the project repository. */
  task?: Item;
  writes: VaultWriteSet[];
};

/** One sticky note added during the session. */
export type RetroNoteDraft = { category: RetroCategory; text: string; author?: string };

/** One note edited during the session; an absent field is left alone. */
export type RetroNoteEdit = {
  id: string;
  text?: string;
  author?: string;
  /** Moves the note to another column. */
  category?: RetroCategory;
};

/** One improvement action selected during the session. */
export type RetroActionDraft = {
  id?: string;
  title: string;
  owner?: string;
  due?: string;
  theme?: string;
  note?: string;
};

/** One action edited during the session; `task` is written by a promotion. */
export type RetroActionEdit = {
  id: string;
  title?: string;
  owner?: string;
  due?: string;
  theme?: string;
  status?: RetroActionStatus;
  note?: string;
};

/**
 * The retro fields an update may change. Notes and actions are edited entry by
 * entry so that one participant's write is one line of diff; themes and votes
 * are replaced wholesale because grouping is one decision about the whole wall.
 */
export type RetroPatch = {
  title?: string;
  date?: string;
  state?: RetroState;
  facilitator?: string;
  participants?: string[];
  anonymous?: boolean;
  votesPerPerson?: number;
  carriedFrom?: string;
  addNotes?: RetroNoteDraft[];
  updateNotes?: RetroNoteEdit[];
  removeNotes?: string[];
  themes?: RetroTheme[];
  votes?: Record<string, string[]>;
  addComments?: RetroCommentDraft[];
  updateComments?: RetroCommentEdit[];
  removeComments?: string[];
  addActions?: RetroActionDraft[];
  updateActions?: RetroActionEdit[];
  removeActions?: string[];
};

/** A new retro. Everything but the sprint has a sensible default. */
export type RetroDraft = {
  sprint?: string;
  board?: string;
  title?: string;
  date?: string;
  facilitator?: string;
  participants?: string[];
  anonymous?: boolean;
  votesPerPerson?: number;
  carriedFrom?: string;
  state?: RetroState;
  author?: string;
};

/** The fields `board.update` may change; the card order is never patched. */
export type BoardPatch = {
  title?: string;
  description?: string;
  projects?: string[];
  columns?: BoardColumnPatch[];
  filters?: BoardFilters;
  swimlanes?: { by?: string; order?: string[]; collapseEmpty?: boolean };
  card?: { show?: string[] };
  sprint?: string;
  backlogColumn?: string;
};

/**
 * A new board as `board.create` receives it (docs/04 §5.1). Only the title is
 * required; the core allocates the slug, the default columns and — on a scrum
 * board — the backlog column.
 */
export type BoardDraft = {
  title: string;
  kind?: BoardKind;
  /** The slug, when the caller does not want the one derived from the title. */
  id?: string;
  description?: string;
  projects?: string[];
  columns?: BoardColumnPatch[];
  filters?: BoardFilters;
  swimlanes?: { by?: string; order?: string[]; collapseEmpty?: boolean };
  card?: { show?: string[] };
  backlogColumn?: string;
  author?: string;
};

/** One column as `board.update` sends it back. */
export type BoardColumnPatch = {
  id: string;
  name?: string;
  statuses?: Record<string, string[]>;
  categories?: StatusCategory[];
  wip?: number;
  collapsed?: boolean;
  color?: string;
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
  /** The column's mapping, echoed so the board editor can patch it back. */
  statuses?: Record<string, string[]>;
  categories?: StatusCategory[];
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
  /** The goal, the dates and the metrics of the sprint a scrum board runs. */
  sprintInfo?: SprintSummary;
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
  /** The sprint a scrum board is scoped to, and whether the move joined it. */
  sprint?: string;
  sprintAdd?: boolean;
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
  /** The first open team, repeated from `teams` for single-team clients. */
  team?: TeamSummary;
  /** Every open team repository, in mount order (GIT-US-0036). */
  teams: TeamSummary[];
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

/**
 * One front-matter field the conflict merge decided (GIT-US-0022). The shapes
 * below mirror `internal/core/merge.go`, which is the one implementation both
 * runtimes call.
 */
export type ConflictFieldDecision = {
  field: string;
  kind: string;
  base?: unknown;
  ours?: unknown;
  theirs?: unknown;
  merged?: unknown;
  choice: string;
  review: boolean;
  note?: string;
};

/** One body region the two sides did not both leave alone. */
export type ConflictHunk = {
  index: number;
  section?: string;
  base: string;
  ours: string;
  theirs: string;
  merged: string;
  choice: string;
  conflicted: boolean;
  suggestion?: string;
  note?: string;
};

/** What the core proposes for one conflicted file. */
export type ConflictMergeResult = {
  path: string;
  structured: boolean;
  fields?: ConflictFieldDecision[];
  hunks?: ConflictHunk[];
  content: string;
  conflicted: number;
  review: number;
  clean: boolean;
  warnings?: string[];
};

/** What the user decided; every field is optional. */
export type ConflictResolutionParams = {
  take?: string;
  content?: string;
  body?: string;
  fields?: Record<string, string>;
  hunks?: Record<string, string>;
  hunkText?: Record<string, string>;
};

/** What `project.create` needs: where the backlog goes and its identity. */
export type NewProjectParams = {
  /** Vault-relative documentation folder; `''` and `'.'` both mean the root. */
  docsFolder?: string;
  /** ID prefix, matching `[A-Z][A-Z0-9]{1,9}`. */
  key: string;
  /** Human name; defaults to the key. */
  name?: string;
  description?: string;
  /** IANA timezone; defaults to `UTC`. */
  timezone?: string;
  vaultId?: string;
};

/** What `project.create` answers with: the project, and the files to persist. */
export type ProjectCreated = {
  project: ProjectSummary;
  writes: WriteSet;
};

/** What `team.create` needs: where `team.yaml` goes and its identity. */
export type NewTeamParams = {
  /** Vault-relative folder; `''` and `'.'` both mean the repository root. */
  root?: string;
  /** ID prefix of sprints and retros, matching `[A-Z][A-Z0-9-]{1,15}`. */
  key: string;
  /** Display name; defaults to the key. */
  name?: string;
  description?: string;
  /** IANA timezone; defaults to `UTC`. */
  timezone?: string;
  /** Knowledge-base folder; defaults to `knowledge`. */
  knowledgePath?: string;
  /** The people the team starts with; it may be empty (ADR-020). */
  members?: TeamMember[];
  vaultId?: string;
};

/** What `team.create` answers with: the team, and the files to persist. */
export type TeamCreated = {
  team: TeamSummary;
  writes: WriteSet;
};

/**
 * One entry of the `projects:` list of `team.yaml` (docs/04 §3.3). It is the
 * routing declaration that decides which project a board may show and whether
 * a card renders live from a clone or read-only from a committed snapshot;
 * `key`, `name` and `repo` plus `docsPath` are the required half.
 */
export type TeamProjectDraft = {
  /** Must equal the `key:` of that repository's own `project.yaml`. */
  key: string;
  /** Display name; defaults to the key. */
  name?: string;
  /** Canonical remote URL. */
  repo: string;
  /** Branch snapshot links and blob URLs are built against; defaults to `main`. */
  defaultBranch?: string;
  /** Folder holding `.pmngr/` inside that repository. */
  docsPath: string;
  host?: string;
  webUrl?: string;
  color?: string;
  archived?: boolean;
};

/**
 * One place a team artifact points at a project: the scope of a board, a
 * column order, the `items` or `committed` list of a sprint, or the task a
 * retro action was promoted into. It is what removing a project would orphan.
 */
export type TeamProjectReference = {
  /** `board`, `sprint` or `retro`. */
  kind: string;
  id: string;
  path: string;
  /** The front-matter field the reference sits in. */
  field: string;
  /** `<projectKey>/<itemId>`, or the bare key when the artifact names the project. */
  ref: string;
};

/**
 * One place something points at an item: a child's `parent`, a story's
 * `milestone`, a typed link, a card in a board column, a sprint's scope, or the
 * task a retro action was promoted into. It is what a delete would orphan.
 */
export type ItemReference = {
  /** `item`, `board`, `sprint` or `retro`. */
  kind: string;
  /** The referring item or artifact. */
  id: string;
  path: string;
  title?: string;
  /** The item type of a referring item; empty for a team artifact. */
  type?: string;
  /** The front-matter field the reference sits in, e.g. `links.blocks`. */
  field: string;
  /** The reference as written: a bare id, or `<projectKey>/<itemId>`. */
  ref: string;
};

/** What `item.references` answers. */
export type ItemReferencesResult = {
  id: string;
  references: ItemReference[];
  /**
   * The direct children among the references, repeated: a child is the one
   * reference a delete would leave with a dangling parent.
   */
  children: ItemReference[];
};

/** What `team.project.add` and `team.project.remove` answer with. */
export type TeamProjectResult = {
  team: TeamSummary;
  /** The entry that was added or removed. */
  project: TeamProjectSummary;
  /** The references a forced removal broke; an add never produces any. */
  references?: TeamProjectReference[];
  /** One write set per repository; a project change touches `team.yaml` only. */
  writes: VaultWriteSet[];
};

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
    params: {
      files: VaultFile[];
      rootLabel?: string;
      vaultId?: string;
      /**
       * Documentation folders this repository declares. Discovery probes the
       * repository root and its first-level directories on its own; a folder
       * deeper than that is found only because it is listed here (ADR-018).
       */
      docsFolders?: string[];
    };
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
    params: {
      vaultId: string;
      role?: 'project' | 'team';
      rootLabel?: string;
      /** Documentation folders declared for this repository (ADR-018). */
      docsFolders?: string[];
    };
    result: WorkspaceVault;
  };
  /** Drop a repository from the workspace; it never touches files. */
  'workspace.unmount': { params: { vaultId: string }; result: { unmounted: string } };

  /**
   * One team repository of the workspace. `team` names it; it may be omitted
   * while a single team is open. It fails with `not_found` when none is open,
   * and with `invalid_request` when several are and none was named.
   */
  'team.get': { params: TeamScoped | undefined; result: TeamSummary };
  /** Every open team repository, in mount order (GIT-US-0036). */
  'team.list': { params: undefined; result: { teams: TeamSummary[]; total: number } };
  /**
   * Turn a mounted folder into a team repository: it writes `team.yaml` plus
   * the `.pmngr/` artifact folders and the knowledge base of docs/04 §2.
   *
   * It refuses a key outside `[A-Z][A-Z0-9-]{1,15}` with `validation_failed`,
   * and a folder that already holds a `team.yaml` with `team_exists`.
   */
  'team.create': { params: NewTeamParams; result: TeamCreated };
  /**
   * Declare a project repository in a team's `team.yaml`, which is what makes
   * its items reachable from that team's boards and sprints (docs/04 §3.9).
   *
   * The entry lands in key order rather than at the end of the list, so two
   * people connecting two different projects write two hunks git can merge. A
   * key the team already declares is refused with `team_project_exists`, and an
   * entry missing `repo` or `docsPath` with `validation_failed`.
   */
  'team.project.add': {
    params: { project: TeamProjectDraft } & TeamScoped;
    result: TeamProjectResult;
  };
  /**
   * Disconnect a project from a team. Nothing in the project repository is
   * touched: the entry is a routing declaration, not the backlog.
   *
   * A project a board, a sprint or a retro action still references is refused
   * with `team_project_referenced` and the list of references; `force` accepts
   * leaving them pointing at a project the team no longer declares.
   */
  'team.project.remove': {
    params: { key: string; force?: boolean } & TeamScoped;
    result: TeamProjectResult;
  };
  /** Resolve `<projectKey>/<itemId>` across every open repository. */
  'ref.resolve': { params: { ref: string }; result: RefResolution };

  'project.list': { params: undefined; result: ProjectSummary[] };
  /**
   * Scaffold a backlog in a repository that has none: it writes
   * `<docsFolder>/.pmngr/project.yaml` plus the folder layout of
   * docs/03-data-model.md §2, and declares the folder so that discovery keeps
   * finding it however deep it is.
   *
   * It refuses a key outside `[A-Z][A-Z0-9]{1,9}` with `validation_failed`,
   * and a folder that already holds a project with `project_exists`.
   */
  'project.create': { params: NewProjectParams; result: ProjectCreated };

  /** Every board of the team repository. */
  'board.list': {
    params: TeamScoped | undefined;
    result: { boards: BoardSummary[]; diagnostics: Diagnostic[] };
  };
  /** One board, rendered over every open repository. */
  'board.get': { params: { board: string } & TeamScoped; result: BoardView };
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
    } & TeamScoped;
    result: BoardMoveResult;
  };
  /** Edit a board's columns, WIP limits, filters or sprint; never its order. */
  'board.update': {
    params: { board: string; rev?: string; patch: BoardPatch } & TeamScoped;
    result: { board: BoardView; writes: VaultWriteSet[] };
  };
  /**
   * Create a board file in the team repository. Only the title is required:
   * an absent kind is kanban, an absent id is the slug of the title, and
   * absent columns are the default category-mapped set (docs/04 R-COL-2). A
   * slug that is already a board fails with `duplicate_id`.
   */
  'board.create': {
    params: BoardDraft & TeamScoped;
    result: { board: BoardView; writes: VaultWriteSet[] };
  };
  /**
   * Delete a board file. It removes a view and nothing else: the items its
   * cards referenced live in their own repositories. A board a sprint still
   * names fails with `board_in_use`, or `sprint_already_active` when that
   * sprint is running.
   */
  'board.delete': {
    params: { board: string; rev?: string } & TeamScoped;
    result: { board: string; writes: VaultWriteSet[] };
  };

  /** The sprints of the team repository, filtered by board and by state. */
  'sprint.list': {
    params: ({ board?: string; state?: SprintState } & TeamScoped) | undefined;
    result: { sprints: SprintSummary[]; diagnostics: Diagnostic[] };
  };
  /** One sprint: its scope, the candidates for it and its metrics. */
  'sprint.get': { params: { id: string } & TeamScoped; result: SprintView };
  /** Create a sprint; the id is allocated by the core from the team key. */
  'sprint.create': {
    params: {
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
    } & TeamScoped;
    result: SprintResult;
  };
  /**
   * Change the goal, the dates or the scope. Every change is one write to the
   * sprint file in the team repository, never a write to an item.
   */
  'sprint.update': {
    params: {
      id: string;
      rev?: string;
      patch: {
        title?: string;
        goal?: string;
        start?: string;
        end?: string;
        state?: SprintState;
        capacityHours?: number;
        velocityTarget?: number;
        participants?: string[];
        items?: string[];
        addItems?: string[];
        removeItems?: string[];
      };
    } & TeamScoped;
    result: SprintResult;
  };
  /** Make a sprint active, snapshot its commitment and point its board at it. */
  'sprint.start': {
    params: { id: string; rev?: string; force?: boolean } & TeamScoped;
    result: SprintResult;
  };
  /** Close a sprint and apply one explicit decision per unfinished item. */
  'sprint.close': {
    params: { id: string; rev?: string; carry?: SprintCarry[] } & TeamScoped;
    result: SprintResult;
  };
  /**
   * The burndown, the cumulative flow diagram and the flow statistics of one
   * sprint, with the provenance of the history behind them. The three come
   * back together because they are one reconstruction of one window.
   */
  'sprint.metrics': { params: { id: string } & TeamScoped; result: SprintMetricsView };

  /**
   * The retros of the team repository, newest first, with every improvement
   * action they left open. The open actions are the point: a team starting a
   * new retro sees what it promised last time (docs/04 §9.1, step 7).
   */
  'retro.list': {
    params: ({ sprint?: string; board?: string; state?: RetroState } & TeamScoped) | undefined;
    result: { retros: RetroSummary[]; carried: RetroActionView[]; diagnostics: Diagnostic[] };
  };
  /** One retro: its notes, its themes by votes, its actions and what it carried. */
  'retro.get': { params: { id: string } & TeamScoped; result: RetroView };
  /** Create a retro; the id is allocated by the core from the team key. */
  'retro.create': { params: RetroDraft & TeamScoped; result: RetroResult };
  /** Apply one session's edits: notes, grouping, votes and actions. */
  'retro.update': {
    params: { id: string; rev?: string; patch: RetroPatch } & TeamScoped;
    result: RetroResult;
  };
  /**
   * Turn one improvement action into a task in a project repository and write
   * the reference back into the retro, so neither end of the link is lost. A
   * project no open repository serves is refused with `repo_not_cloned` rather
   * than half written (docs/04 R-RETRO-2).
   */
  'retro.promote': {
    params: {
      id: string;
      action: string;
      project: string;
      labels?: string[];
      rev?: string;
    } & TeamScoped;
    result: RetroResult;
  };

  /**
   * The committed index snapshot of every project the team declares, with its
   * age and its staleness (docs/04 §6).
   */
  'snapshot.list': {
    params: TeamScoped | undefined;
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
    } & TeamScoped;
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
  /**
   * Everything that points at an item, across the project repository and every
   * open team repository. The web app shows it before a delete.
   */
  'item.references': {
    params: { id: string; team?: string };
    result: ItemReferencesResult;
  };
  /**
   * Flip one task-list checkbox in the body. `line` is the 1-based line of the
   * marker; the core rewrites that line and nothing else, under `rev`.
   */
  'item.task.set': {
    params: { id: string; line: number; checked: boolean; rev: string };
    result: { item: Item; writes: WriteSet };
  };
  'item.validate': { params: { id?: string; text?: string; path?: string }; result: Diagnostic[] };
  'item.parse': { params: { path: string; text: string }; result: Item };
  'item.serialize': { params: { item: Item }; result: { text: string } };

  'comment.list': { params: { id: string }; result: Comment[] };
  'comment.add': {
    params: {
      id: string;
      author?: string;
      authorName?: string;
      authorEmail?: string;
      body: string;
      inReplyTo?: string;
    };
    result: { comment: Comment; writes: WriteSet };
  };

  'kb.tree': { params: { project?: string; vaultId?: string }; result: KbNode[] };
  'kb.page': { params: { path: string; vaultId?: string }; result: KbPage };
  'kb.write': {
    params: { path: string; text: string; rev?: string; vaultId?: string };
    result: { page: KbPage; writes: WriteSet };
  };
  /**
   * Appends feedback notes to the feedback block at the end of a page. Each
   * note is anchored to a hash of the body lines it quotes; a later write that
   * changes those lines drops the note (docs/03-data-model.md, ADR-030).
   */
  'kb.feedback.add': {
    params: {
      path: string;
      notes: KbFeedbackNoteDraft[];
      rev?: string;
      vaultId?: string;
      author?: string;
      authorName?: string;
      authorEmail?: string;
    };
    result: { page: KbPage; writes: WriteSet; notes: KbFeedbackNoteRef[] };
  };

  search: { params: { q: string; limit?: number; project?: string }; result: SearchHit[] };

  /**
   * Merge the three versions of one conflicted file, applying the user's
   * resolution when there is one. It needs no vault: it is a pure function of
   * the three blobs, which is what lets browser-only mode resolve a conflict
   * with the same rules as the companion (docs/06 §5).
   */
  'conflict.merge': {
    params: {
      path: string;
      base?: string;
      ours?: string;
      theirs?: string;
      resolution?: ConflictResolutionParams;
    };
    result: ConflictMergeResult;
  };
};

export type CoreMethodName = keyof CoreApi;
export type CoreParams<M extends CoreMethodName> = CoreApi[M]['params'];
export type CoreResult<M extends CoreMethodName> = CoreApi[M]['result'];
