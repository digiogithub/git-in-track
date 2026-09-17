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

/**
 * AG-UI protocol types (GIT-US-0053). They are imported from the browser-safe
 * entry `@pando-ai/sdk/agui/client`, never from the package root or the
 * `/agui` index, which pull Node-only code and CopilotKit. Every one of them
 * is a type, so the import is erased at build time and the provider boundary
 * stays free of a runtime dependency on the SDK.
 */
import type { AguiEvent, AguiInfo, AguiMessage, RunAgentInput } from '@pando-ai/sdk/agui/client';

import type {
  CommentPushEntry,
  CommentPushInput,
  CommentPushResult,
  External,
  InboxDraftOptions,
  InboxFilter,
  InboxPage,
  InboxStatus,
  InboxTriageAction,
  InboxTriageInput,
  InboxTriageResult,
  ItemInbox,
  KbPageSyncStatus,
  KbSyncJobResult,
  KbSyncSelector,
  KbSyncState,
  KbSyncStatusResult,
  KbUnlinkResult,
  SprintSnapshot,
  SprintSnapshotBucket,
  SprintSnapshotPoint,
  SprintSnapshotTotals,
  SprintStatus,
  SprintTransfer,
  SprintTransferMode,
  WriteSet,
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
  ItemReference,
  ItemReferencesResult,
  ItemType,
  KbFeedbackNoteDraft,
  KbFeedbackNoteRef,
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
  RetroComment,
  RetroCommentDraft,
  RetroCommentEdit,
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
  TeamProjectDraft,
  TeamProjectReference,
  TeamProjectResult,
  TeamProjectSummary,
  TeamSummary,
  WorkspaceSummary,
  WorkspaceVault,
} from '@/core-bridge/api';

export type {
  CommentPushEntry,
  CommentPushInput,
  CommentPushResult,
  External,
  InboxDraftOptions,
  InboxFilter,
  InboxPage,
  InboxStatus,
  InboxTriageAction,
  InboxTriageInput,
  InboxTriageResult,
  ItemInbox,
  KbPageSyncStatus,
  KbSyncJobResult,
  KbSyncSelector,
  KbSyncState,
  KbSyncStatusResult,
  KbUnlinkResult,
  SprintSnapshot,
  SprintSnapshotBucket,
  SprintSnapshotPoint,
  SprintSnapshotTotals,
  SprintStatus,
  SprintTransfer,
  SprintTransferMode,
  WriteSet,
  KbFeedbackNoteDraft,
  KbFeedbackNoteRef,
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
  ItemReference,
  ItemReferencesResult,
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
  RetroComment,
  RetroCommentDraft,
  RetroCommentEdit,
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
  TeamProjectDraft,
  TeamProjectReference,
  TeamProjectResult,
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

/**
 * The lifecycle of the public tunnel (`/api/v1/tunnel`).
 *
 * `starting` already carries the URL: the hostname is handed out before the
 * edge answers on it, and DNS takes a few seconds to propagate. The UI has to
 * keep those two apart, because a link shared during `starting` fails for the
 * person who opens it.
 */
export type TunnelState = 'off' | 'starting' | 'connected' | 'reconnecting' | 'error';

/**
 * The public tunnel of the companion (story: `gintrack serve --tunnel`).
 *
 * A tunnel puts a server that otherwise listens on `127.0.0.1`, with read and
 * write access to the user's repositories, on the public internet. The bearer
 * token is the only thing guarding it, so the shape carries `tokenConfigured`:
 * the server refuses to open a tunnel when authentication is disabled, and the
 * UI explains that rather than reporting a generic failure.
 */
export type TunnelStatus = {
  /** False when this build or runtime cannot tunnel at all; hide the card. */
  supported: boolean;
  /** The tunnelling service in use, for example `cloudflare`. */
  provider: string;
  state: TunnelState;
  /**
   * `https://<random-words>.trycloudflare.com`, or empty when there is none.
   * A new hostname is minted on every enable: never cache or persist it.
   */
  url: string;
  /** Edge connections the tunnel holds. */
  connections: number;
  /** When the current tunnel came up; `null` while there is none. */
  since: string | null;
  /** Why the tunnel failed, when `state` is `error`. Empty otherwise. */
  error: string;
  /** False when the companion was started with authentication disabled. */
  tokenConfigured: boolean;
};

/**
 * The write surface of the MCP server (`GET|PATCH /api/v1/mcp/settings`).
 *
 * The read tools are always there; the write tools — `create_task`,
 * `update_item`, `move_on_board` and the rest — are advertised only when this
 * is on. It is a stored setting rather than only a `--allow-write` flag because
 * an agent's MCP entry is usually a bare `gintrack mcp`: the companion writes
 * the choice to its configuration file, and every stdio server started
 * afterwards reads it (docs/08-mcp-server.md section 7.1).
 */
export type McpSettings = {
  /** False when the runtime can neither persist nor apply it; hide the card. */
  supported: boolean;
  /** Whether the write tools are advertised. */
  allowWrite: boolean;
  /** Whether this companion also serves the endpoint at POST /mcp. */
  http: boolean;
  /** Whether the configuration file took the change. */
  persisted: boolean;
  /** The configuration file the choice lives in, empty when there is none. */
  configPath: string;
  /** The tools the endpoint of this process advertises. */
  tools: string[];
};

/**
 * Where the YouTrack credential of a project came from (`tokenSource`).
 *
 * It decides what the settings card may offer: a token that arrived in the
 * environment or on the command line is owned by whoever started the
 * companion, so this screen can report it but must not pretend to clear it.
 * `''` and `none` both mean there is none.
 */
export type YouTrackTokenSource = '' | 'none' | 'file' | 'config' | 'env' | 'flag';

/** When a comment reaches the linked issue. */
export type YouTrackPushComments = '' | 'manual' | 'auto';

/** When a knowledge-base page is synchronized, and which way. */
export type YouTrackKbSync = '' | 'manual' | 'on_write';
export type YouTrackKbSyncDirection = '' | 'push' | 'pull' | 'both';

/**
 * One project's YouTrack connection (`GET /api/v1/youtrack/settings`).
 *
 * It deliberately carries no token: the credential is write-only, and this
 * shape reports only that one resolves (`hasToken`) and where it came from.
 * Anything that renders a stored token, placeholder included, is a leak the
 * user could save back as a literal value.
 */
export type YouTrackSettings = {
  /** The git-in-track project this connection belongs to. */
  projectKey: string;
  /** Whether `project.yaml` holds an `integrations.youtrack` block at all. */
  configured: boolean;
  /** The instance URL, context path included. */
  url: string;
  /** The YouTrack project short name, the "ACME" of ACME-42. */
  project: string;
  /**
   * That project's internal entity id, the "0-17" form.
   *
   * YouTrack addresses a project by this id on every write — creating an
   * article with the short name is refused outright — so the settings picker
   * records it when the project is chosen. It is empty on a link written by
   * hand, and the companion then resolves it from the short name when a job
   * needs it.
   */
  projectId: string;
  /** git-in-track field → the YouTrack field carrying it and its value map. */
  fieldMap: Record<string, YouTrackFieldMapping>;
  pushComments: YouTrackPushComments;
  kbSync: YouTrackKbSync;
  kbSyncDirection: YouTrackKbSyncDirection;
  /** Whether a credential resolves for this project. Never the credential. */
  hasToken: boolean;
  tokenSource: YouTrackTokenSource;
  /** Whether the last change reached the configuration file. */
  persisted: boolean;
  /** The `project.yaml` the committed half lives in, relative to the repository. */
  projectPath: string;
  /** The mounted repository holding that file. */
  repo: string;
};

/**
 * A sparse change to the connection. An absent key is left alone and a present
 * one is applied, so an empty string clears a value rather than being
 * indistinguishable from "not mentioned" — which is what makes disconnecting a
 * project, or forgetting a token, expressible at all.
 */
export type YouTrackSettingsPatch = {
  url?: string;
  project?: string;
  /**
   * The entity id of the chosen project. A patch that moves `project` without
   * carrying one clears the stored id rather than leaving the previous
   * project's id behind it.
   */
  projectId?: string;
  fieldMap?: Record<string, YouTrackFieldMapping>;
  pushComments?: YouTrackPushComments;
  kbSync?: YouTrackKbSync;
  kbSyncDirection?: YouTrackKbSyncDirection;
  /** Write-only: it is never read back, and `''` forgets the stored one. */
  token?: string;
};

/** What a successful probe reports (`POST /api/v1/youtrack/test`). */
export type YouTrackTestResult = {
  ok: boolean;
  baseUrl: string;
  login: string;
  fullName: string;
  email: string;
  /** The linked YouTrack project, when one is configured and readable. */
  project: string;
};

/** One row of `GET /api/v1/youtrack/projects`. */
export type YouTrackProject = {
  id: string;
  shortName: string;
  name: string;
  archived: boolean;
};

/**
 * One allowed value of a bundle-backed YouTrack field.
 *
 * `name` is the key a mapping is written against — it is stable, where the id
 * is instance-local and the localized name changes with the UI language — and
 * `label` is what to show.
 *
 * The instance also declares a colour per value. It is deliberately not carried
 * here: a colour in a component is a bug (docs/13 §1), and a value table that
 * painted itself in YouTrack's palette would be exactly that.
 */
export type YouTrackFieldValue = {
  id: string;
  /** The stable value name; the key a value mapping is written against. */
  name: string;
  /** What to render: the localized name when the instance declares one. */
  label: string;
  description?: string;
  /** The position the instance lists the value at. */
  ordinal: number;
  /** Still on old issues, no longer offered. Shown, never hidden. */
  archived: boolean;
  /**
   * Whether a state value marks an issue as done. Absent — not `false` — when
   * the instance never said, which is why it must not propose a done status.
   */
  isResolved?: boolean;
};

/** One custom field of the remote project (`GET /api/v1/youtrack/fields`). */
export type YouTrackField = {
  id: string;
  name: string;
  type: string;
  bundleId: string;
  bundleType: string;
  canBeEmpty: boolean;
  /** What the instance shows for an unset value, when it declares one. */
  emptyFieldText?: string;
  /**
   * Whether this field's values are enumerable at all: false for a text, date,
   * integer or period field, and for a bundle kind the companion does not know.
   * `values` is then empty without that being a failure.
   */
  bundled: boolean;
  values: YouTrackFieldValue[];
  /** Why the values could not be read; already redacted by the companion. */
  warnings: string[];
};

/**
 * One entry of the field map: which YouTrack custom field carries a
 * git-in-track field, and what its values mean here.
 *
 * `values` maps a YouTrack value *name* onto the git-in-track value it means —
 * a status id for `status`, a priority for `priority`, an item type for
 * `type` — and the companion accepts it on those three keys only
 * (`valueMappableFields`). An entry with no value map leaves the importer's
 * defaults in force.
 */
export type YouTrackFieldMapping = {
  field: string;
  values?: Record<string, string>;
};

/**
 * The field-mapping vocabulary: the YouTrack half discovered from the instance
 * and the git-in-track half the companion declares, so the UI offers both sides
 * from one call instead of hard-coding a list that would drift.
 */
export type YouTrackFieldList = {
  project: string;
  fields: YouTrackField[];
  total: number;
  gintrackFields: string[];
  /**
   * The subset of `gintrackFields` whose *values* can be mapped one by one. A
   * settings screen renders a value table only for these; the others take a
   * field name and nothing else.
   */
  valueMappableFields: string[];
};

/**
 * Which YouTrack connection a call addresses. The key is optional because a
 * companion serving exactly one project defaults to it; one serving several
 * refuses an unnamed call rather than guessing.
 */
export type YouTrackScope = { projectKey?: string };

// ------------------------------------------------ YouTrack import (GIT-EP-0012)

/**
 * The saved queries the import dialog offers instead of the YouTrack query
 * language. `''` means "whatever the user typed, and nothing else".
 */
export type YouTrackIssuePreset = '' | 'epics' | 'stories' | 'tasks' | 'versions' | 'unresolved';

/**
 * One row of the issue autosuggest (`GET /api/v1/youtrack/issues`, GIT-US-0054).
 *
 * `linked` is resolved by the companion from its own index, by the
 * `(external.system, external.id)` pair, so the picker can say "already
 * imported" and offer the row as an update without a second round trip.
 */
export type YouTrackIssue = {
  id: string;
  idReadable: string;
  summary: string;
  type: string;
  state: string;
  assignee: string;
  updated: string;
  url: string;
  /** The git-in-track item a previous import created for this issue. */
  linked: { itemId: string } | null;
};

/** One page of the autosuggest; `nextCursor` is empty on the last one. */
export type YouTrackIssuePage = {
  items: YouTrackIssue[];
  nextCursor: string;
};

/** What the autosuggest asks for. */
export type YouTrackIssueQuery = {
  q?: string;
  preset?: YouTrackIssuePreset;
  limit?: number;
  cursor?: string;
};

/**
 * The import options, as `internal/vault`'s `YouTrackImportParams` models them
 * (story GIT-US-0047). Preview and run take exactly the same shape, which is
 * what makes "what preview showed me is what run does" true by construction.
 */
export type YouTrackImportOptions = {
  /** The git-in-track project to import into; the only one when omitted. */
  project?: string;
  /** Readable issue ids. Exactly one of `ids` and `query` is given. */
  ids?: string[];
  query?: string;
  /** Subtask recursion: 0 imports the selected issues only. */
  depth: number;
  includeLinks: boolean;
  includeComments: boolean;
  includeAttachments: boolean;
};

/**
 * One finding of the mapper (`internal/youtrack/mapping`.Warning). `reason` is
 * a complete English sentence and is the only part a surface renders as prose —
 * as **plain text**, because every field here is third-party content.
 */
export type YouTrackImportWarning = {
  field: string;
  value?: string;
  fallback?: string;
  reason: string;
};

/** What an import would do to one issue, before anything is written. */
export type YouTrackImportPlanItem = {
  youtrackId: string;
  title: string;
  mappedType: string;
  action: 'create' | 'update';
  /** The item an update would patch; empty for a create. */
  targetId?: string;
  parent?: string;
  milestone?: string;
  depth: number;
  comments: number;
  warnings?: YouTrackImportWarning[];
};

/** The answer of the preview operation: a plan, and nothing written. */
export type YouTrackImportPreviewResult = {
  project: string;
  issues: YouTrackImportPlanItem[];
  /** Findings about the import as a whole rather than about one issue. */
  warnings?: YouTrackImportWarning[];
};

/**
 * What asking for an import answers: the id of the job that runs it.
 *
 * `POST /api/v1/youtrack/import` always queues. An import is a hundred issues
 * and a hundred requests against somebody else's rate limit, so it answers
 * `202` with a job id and nothing that could go stale — the work happens off
 * the request, the browser follows it over the `sync.job.*` events and reads
 * the failure, if any, back from `getSyncJob(jobId)`. There is deliberately no
 * synchronous shape to handle: the preview is the operation that answers
 * inline, and it answers a plan rather than a result.
 */
export type YouTrackImportRun = {
  jobId: string;
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
    /** The current line of work: a branch, or a jj working copy. */
    branch: string;
    /** True when there is no named line of work at all (a detached HEAD). */
    detached: boolean;
    /** What the VCS calls that line: `branch`, `bookmark`, `working-copy`. */
    lineKind?: LineKind;
    /** What a publish updates on the remote: a branch, or a jj bookmark. */
    pushTarget?: string;
    clean: boolean;
    /** Empty in a VCS with no staging area; jj commits the working copy. */
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
  | 'jujutsu'
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

/**
 * What a VCS calls the current line of work (GIT-US-0039). Empty means there is
 * no named one, which is git's detached HEAD.
 */
export type LineKind = 'branch' | 'bookmark' | 'working-copy' | '';

/** How an unfinished integration is taken back (GIT-US-0039). */
export type UndoMethod = 'abort' | 'operation_log' | '';

/** How an unfinished integration is carried forward (GIT-US-0039). */
export type ResumeMethod = 'continue' | '';

/** One repository's sync state, the row the sync panel renders. */
export type SyncStatus = {
  /** The current line of work: a git branch, or a jj working copy. */
  branch: string;
  /** True when there is no named line of work at all (a detached HEAD). */
  detached: boolean;
  /** What the VCS calls that line: `branch`, `bookmark`, `working-copy`. */
  lineKind?: LineKind;
  /** What a publish updates on the remote: a branch, or a jj bookmark. */
  pushTarget?: string;
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
  /** `rebase` or `merge` when one is unfinished, else absent. */
  operation?: string;
  /**
   * True while an integration has not settled, so nothing else may run against
   * the repository. It is the VCS-neutral form of `operation`: jj never leaves
   * a half-finished operation, but a commit can carry an unresolved conflict.
   */
  unfinished?: boolean;
  /** How that integration is taken back: git aborts, jj undoes an operation. */
  undo?: UndoMethod;
  /** How it is carried forward, absent when there is nothing to carry. */
  resume?: ResumeMethod;
  /**
   * True for a repository managed with Jujutsu. Its branch is `@`, its working
   * copy is a commit rather than a checkout, and every git write is refused
   * (GIT-US-0038).
   */
  jujutsu?: boolean;
  state: SyncState;
};

/** What manages a repository on disk (GIT-US-0038). */
export type RepoVCS = {
  kind: 'git' | 'jj' | 'none';
  /** Jujutsu only: `colocated` when the workspace has a git directory of its own. */
  layout?: 'colocated' | 'internal';
  gitDir?: boolean;
};

/** The sentence every surface shows for a Jujutsu repository. */
export const JUJUTSU_SUMMARY = 'Managed by Jujutsu — reads and writes go through jj';

/**
 * The sentence shown for a jj repository the product cannot write to: the jj
 * binary is missing, so the read-only guard of GIT-US-0038 is driving.
 */
export const JUJUTSU_READ_ONLY =
  'Managed by Jujutsu, and no jj binary was found: reads work, and every write is refused ' +
  'rather than made behind jj\u2019s back.';

/** True when a repository is managed with Jujutsu, whatever its layout. */
export function isJujutsu(vcs: RepoVCS | undefined): boolean {
  return vcs?.kind === 'jj';
}

/** One repository in a sync status listing. */
export type SyncRepoStatus = {
  repo: string;
  path: string;
  /** False when the folder is not a git working tree; `reason` says so. */
  git: boolean;
  /** What manages the folder: git, jj, or nothing. */
  vcs?: RepoVCS;
  /**
   * False when the repository cannot be written to at all — today only a jj
   * workspace with no jj binary installed. It is what a surface disables its
   * write actions from, rather than "is it jj" (GIT-US-0041).
   */
  writes?: boolean;
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
  /** The CORS proxy in effect; browser-only mode, never a credential. */
  corsProxy?: string;
  /**
   * Where that proxy came from: `configured` when the user set it, `companion`
   * when it is the one the running companion serves at `/cors-proxy/` — the
   * only proxy this app ever picks on its own — and `none` when there is none
   * (docs/06 §6.3, GIT-US-0042).
   */
  proxySource?: 'configured' | 'companion' | 'none';
  /**
   * The background job engine's half of the same settings (GIT-US-0084).
   * Absent on a runtime that has no engine, which is what the settings card
   * gates on.
   */
  engine?: SyncEngineSettings;
  /**
   * Whether the last change reached the configuration file. It is `false` for
   * any change touching the engine today: the configuration file has no
   * `sync.engine` section yet, so a knob is process-only until a restart.
   */
  persisted?: boolean;
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

/**
 * The three sides of a conflicted path, however the backend produced them:
 * git reads its index stages, a Jujutsu backend reads the conflict recorded
 * inside the commit (GIT-US-0039).
 */
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
  /**
   * The dialect those markers are written in. jj adds `%%%%%%%` and `+++++++`
   * to git's, so a parser must honour what it is told rather than assume.
   */
  markers?: 'git' | 'jj';
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
  /** The engine knobs; they are sent nested, which is the form that wins. */
  engine?: SyncEngineSettingsPatch;
};

// ------------------------------------------- background job engine (GIT-EP-0015)

/** Where a job is in the engine's state machine. */
export type SyncJobState = 'queued' | 'running' | 'done' | 'failed' | 'cancelled';

/**
 * The last failure of a job, as the engine recorded it.
 *
 * `message` is third-party text — a tracker's error body, with the credentials
 * the engine recognized already redacted. It is rendered as **plain text**,
 * never as Markdown and never as HTML.
 */
export type SyncJobError = {
  /** 1-based attempt this error came from. */
  attempt: number;
  /** How the engine classified it: `terminal`, `transient`, `rate_limited`, … */
  class: string;
  message: string;
  at: string;
  /** Nanoseconds the error itself asked to wait, absent when it asked for none. */
  retryAfter?: number;
};

/**
 * One job of the queue (`GET /api/v1/sync/jobs`). The job's *payload* is
 * deliberately absent from this shape: it is the one part the API cannot vouch
 * for, and nothing in the UI reads it.
 */
export type SyncJob = {
  id: string;
  /** `youtrack.import`, `youtrack.comment.push`, … */
  kind: string;
  /** What the job is keyed on — the project, usually. */
  key: string;
  state: SyncJobState;
  attempts: number;
  createdAt: string;
  updatedAt: string;
  /** When the next attempt is due; absent when none is scheduled. */
  nextAttempt?: string;
  lastError?: SyncJobError;
  /** The job gave up and is waiting to be retried or cleared. */
  deadLetter?: boolean;
};

/** How many jobs the whole queue holds in each state — never just the page. */
export type SyncJobCounts = {
  queued: number;
  running: number;
  done: number;
  failed: number;
  cancelled: number;
};

/** The filter `GET /api/v1/sync/jobs` accepts; `state` and `kind` are OR-ed. */
export type SyncJobFilter = {
  state?: SyncJobState[];
  kind?: string[];
  limit?: number;
  cursor?: string;
};

/** One page of the queue, plus the whole queue's summary. */
export type SyncJobPage = {
  jobs: SyncJob[];
  /** The id the next page starts at; empty on the last page. */
  nextCursor: string;
  /** How many jobs matched the filter, before paging. */
  total: number;
  counts: SyncJobCounts;
  /** How many batches are inside a handler right now. */
  running: number;
  deadLetter: number;
  /** Whether the worker pool is up. An idle engine is still `true`. */
  engine: boolean;
};

/** The engine half of `GET|PATCH /api/v1/sync/settings`. */
export type SyncEngineSettings = {
  workers: number;
  batchSize: number;
  rate: number;
  maxAttempts: number;
  retentionHours: number;
  drainSeconds: number;
  running: boolean;
};

/**
 * A sparse change to the knobs. `workers`, `batchSize` and `rate` take effect on
 * the running pool at once; `maxAttempts` is fixed when the engine is built, so
 * it is recorded and applies from the next start.
 */
export type SyncEngineSettingsPatch = {
  workers?: number;
  batchSize?: number;
  rate?: number;
  maxAttempts?: number;
};

/**
 * The ranges the companion enforces (docs/07-cli-and-api.md §4.1). They are
 * checked in the browser too, so a value that cannot work is refused before a
 * request rather than after one.
 */
export const SYNC_ENGINE_RANGES = {
  workers: { min: 1, max: 64 },
  batchSize: { min: 1, max: 500 },
  rate: { min: 0, max: 1000 },
  maxAttempts: { min: 1, max: 20 },
} as const;

/**
 * A `sync.job.*` frame, normalized. `phase` is the topic that carried it; the
 * synthetic `resync` phase is what a reconnect, a `stream.overflow` or a
 * `resume.gap` raises, and means "the stream lost its place, reconcile from
 * `GET /api/v1/sync/jobs`" — it carries no job.
 *
 * `processed` and `total` count the *coalescing group* (the job's kind plus its
 * key), which is the unit the engine batches by and the unit a progress bar
 * renders. Progress is coalesced server-side to one frame per 500 ms per group
 * and terminal frames are never throttled, so a consumer must not add a second
 * layer of throttling and must treat a missing intermediate frame as normal.
 */
export type SyncJobEventPhase = 'queued' | 'started' | 'progress' | 'done' | 'failed' | 'resync';

export type SyncJobEvent = {
  phase: SyncJobEventPhase;
  id: string;
  kind: string;
  key: string;
  /** `done` on a terminal frame is `done` *or* `cancelled`; both end the row. */
  state: SyncJobState | '';
  attempt: number;
  processed: number;
  total: number;
  error: string;
  errorClass: string;
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
  /**
   * Which engine answers `search`. `'pando'` means the companion can also ask
   * a Pando index for semantically related passages, which is what the search
   * UI gates its "Related by meaning" section on (GIT-US-0086).
   */
  fullTextSearch: 'core' | 'bleve' | 'pando';
  mcp: boolean;
  openInEditor: boolean;
  maxBatchWrite: number;
  /**
   * This runtime can talk to YouTrack at all — companion mode. It is what
   * decides whether the settings card is rendered, because a card that only
   * appears once a project is already connected can never connect the first
   * one.
   */
  youtrackSupported: boolean;
  /** At least one mounted project declares an `integrations.youtrack` block. */
  youtrack: boolean;
  /**
   * This runtime exposes `/api/v1/search/settings` — companion mode. It gates
   * the semantic-search settings card the way `youtrackSupported` gates the
   * YouTrack one: the card has to exist before anything is configured, so it
   * asks whether the runtime *has* the surface, not whether Pando is already
   * wired up (GIT-US-0091).
   */
  searchSettings: boolean;
  /**
   * The companion can reach a Pando AG-UI adapter for at least one repository
   * (GIT-EP-0018). Everything the agent feature renders branches on this flag,
   * never on the provider kind: a companion built without the agent routes,
   * or configured with no `agui-serve` behind it, answers `false` and the chat
   * surface simply is not there.
   */
  agent: boolean;
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
  /** What manages the repository on disk; absent in browser-only mode. */
  vcs?: RepoVCS;
};

export type MountInput = {
  kind: RepoKind;
  /** Companion mode: an absolute path. Browser mode: a picked directory handle id. */
  location: string;
  docsFolder?: string;
  /**
   * Every documentation folder this repository declares, `docsFolder`
   * included. Discovery probes the repository root and its first-level
   * directories on its own; a folder deeper than that — a monorepo's
   * `apps/api/docs` — is found only because it is listed here (ADR-018).
   */
  docsFolders?: string[];
};

/**
 * Creating a project in a repository that has none (story GIT-US-0031).
 *
 * It writes `<docsFolder>/.pmngr/project.yaml` and the folder layout
 * docs/03-data-model.md §2 prescribes. A key outside `[A-Z][A-Z0-9]{1,9}` is
 * refused with `validation_failed`, and a folder that already holds a project
 * with `project_exists` — a backlog is never overwritten.
 */
export type CreateProjectInput = {
  /** The repository to write into; the only open one when omitted. */
  repoId?: string;
  /** Repository-relative documentation folder; `''` means the root. */
  docsFolder: string;
  /** ID prefix, matching `[A-Z][A-Z0-9]{1,9}`. */
  key: string;
  /** Human name; defaults to the key. */
  name?: string;
  description?: string;
  /** IANA timezone; defaults to `UTC`. */
  timezone?: string;
};

/**
 * Creating a team repository in a folder that is not one (story GIT-US-0034).
 *
 * It writes `team.yaml` at the folder root plus the `.pmngr/` artifact folders
 * and the knowledge base docs/04-team-repository.md §2 prescribes. A key
 * outside `[A-Z][A-Z0-9-]{1,15}` is refused with `validation_failed`, and a
 * folder that already holds a `team.yaml` with `team_exists` — the routing
 * table of the workspace is never overwritten.
 */
export type CreateTeamInput = {
  /** The repository to write into; the only open one when omitted. */
  repoId?: string;
  /** Repository-relative folder; `''` means the root, which is where it goes. */
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
};

/**
 * A knowledge base scope: a project's docs folder, or the `knowledge/` folder
 * of the team repository. `teamId` is the team key of `team.yaml`.
 */
export type KbScope = { kind: 'project'; projectKey: string } | { kind: 'team'; teamId: string };

/**
 * Spreads `{ team }` into a request only when a team was named, so a
 * single-team workspace keeps sending exactly the payload it sent before the
 * active team existed (GIT-US-0036).
 */
export function teamScope(team?: string): { team?: string } {
  return team === undefined || team === '' ? {} : { team };
}

/**
 * Where a hit came from. `'core'` is an exact match found by the local index;
 * `'pando'` is a passage a semantic index considered related. A hit that
 * carries no `source` predates GIT-US-0086 and is read as `'core'`.
 */
export type SearchHitSource = 'core' | 'pando';

/** Which Pando indexation a semantic hit came from (story GIT-US-0098). */
export type SearchHitIndex = 'kb' | 'code';

/**
 * One search result. `snippet` is the passage that matched — repository
 * content, so it is always rendered as escaped text — and `score` is the
 * engine's own relevance, only ever shown as a subdued indicator.
 */
export type SearchHit = {
  /**
   * `file` is a document a semantic backend found inside the indexed tree that
   * the index owns neither as an item nor as a page. It carries a path and a
   * snippet and nothing else (story GIT-US-0096).
   */
  kind: 'item' | 'page' | 'file';
  id?: string;
  path?: string;
  title: string;
  snippet?: string;
  score?: number;
  /** Project key the hit belongs to; the team key for a team knowledge-base page. */
  project?: string;
  /** Repository the hit came from, set by a workspace-wide search. */
  vaultId?: string;
  source: SearchHitSource;
  /**
   * Which of Pando's two indexations produced the hit: `kb` for the
   * knowledge-base indexation of the documentation directory, which holds the
   * backlog, `code` for the code indexation of the repository root, which holds
   * the source and the Markdown outside the knowledge base. Absent on a core
   * hit, which has one index to come from (story GIT-US-0098).
   *
   * A row is labelled with it so a code hit is never read as a backlog item.
   */
  index?: SearchHitIndex;
  /**
   * Where in the document the fragment was found: absent for the document
   * itself, `comment` for a semantic hit inside an item's comment thread that
   * resolved back to the item.
   */
  match?: 'comment';
  /** Further comments of the same item collapsed into this hit. */
  moreMatches?: number;
};

/**
 * The answer to one search. Hits arrive ordered: exact matches first, then the
 * semantic ones that are not already among them. `degraded` says the semantic
 * half could not be reached, so the panel can say so instead of silently
 * dropping a section the user turned on.
 */
export type SearchResult = {
  hits: SearchHit[];
  degraded?: boolean;
};

export type SearchQuery = {
  text: string;
  projectKey?: string;
  limit?: number;
};

/**
 * What Pando indexes for one mounted repository.
 *
 * There is no exported copy to date any more (epic GIT-EP-0020): Pando's
 * `KBPath` is the repository's own documentation directory and its root is
 * registered as a code project. So the diagnosable thing is where Pando was
 * pointed and how much this companion's index finds there — a documentation
 * directory holding no items is a misconfigured `KBPath`, and the row says so.
 */
export type SearchIndexedRepo = {
  repo: string;
  /** The working tree, registered with Pando as a code project. */
  root: string;
  /** The documentation directories; the backlog lives under them in `.pmngr/`. */
  docs: string[];
  items: number;
  pages: number;
  comments: number;
  /**
   * The repository's code-project registration, which the companion performs
   * in the background when it starts (story GIT-US-0098). It is absent until
   * that pass has run, and `note` is why there is no code search when there is
   * none — a Pando that is down never fails the companion, so the reason has to
   * be readable here.
   */
  code?: SearchCodeIndex;
};

/** What became of one repository's code-project registration. */
export type SearchCodeIndex = {
  /** The Pando project the repository root was registered under. */
  project: string;
  /**
   * `off` — nothing is configured; `registered` — Pando already had it and was
   * not asked to index it again; `indexing` — a job is filling the index;
   * `unavailable` — Pando refused or did not answer.
   */
  status: 'off' | 'registered' | 'indexing' | 'unavailable';
  /** The Pando indexing job, when one was started. */
  job?: string;
  /** What happened, in words, including why there is no code search. */
  note?: string;
};

/** What Pando's REST reindex route counted (`pando.ReindexStats`). */
export type SearchReindexKbStats = {
  scanned: number;
  added: number;
  updated: number;
  unchanged: number;
  deleted: number;
};

/** The source-tree half of a reindex, for one repository. */
export type SearchReindexRepo = {
  repo: string;
  /** The Pando code-index job the repository's source tree was handed to. */
  codeJob?: string;
  /** This repository's code-index failure alone; the job carries on. */
  codeError?: string;
};

/**
 * Where a reindex is. The two working phases are walked in this order and the
 * job settles into `completed` or `failed`; the same values arrive on the
 * `search.progress` topic.
 */
export type SearchReindexPhase = 'code' | 'kb' | 'completed' | 'failed';

/** `POST /api/v1/search/reindex` → the job, and `settings.reindex` afterwards. */
export type SearchReindexJob = {
  jobId: string;
  /**
   * The one repository the job was asked for, absent for a reindex of the
   * whole workspace (GIT-US-0101).
   */
  scope?: string;
  startedAt: string;
  endedAt?: string;
  phase: SearchReindexPhase;
  repos: SearchReindexRepo[];
  /** Real counts, only when a REST URL made a true reindex possible. */
  kb?: SearchReindexKbStats;
  /**
   * What happened to the knowledge-base half in words. Without a REST URL
   * there is no route to ask for a reindex at all, and the note says so rather
   * than letting the card claim an index operation that never ran.
   */
  kbNote?: string;
  error?: string;
};

/**
 * `GET|PATCH /api/v1/search/settings` — where Pando is, what it indexes and
 * whether it answers (story GIT-US-0091, docs/07).
 *
 * Neither Pando token is ever part of this shape. They are resolved from the
 * environment or the configuration file and stay in the companion process, so
 * the card reports where a credential comes from and never offers a field for
 * one.
 */
export type SearchSettings = {
  /** The engine `features.search` reports, the same values as the capability. */
  backend: Capabilities['fullTextSearch'];
  configured: boolean;
  mcpUrl: string;
  restUrl: string;
  projectId: string;
  allowRemote: boolean;
  /**
   * A live probe of the MCP endpoint, run while answering. `null` means no
   * endpoint is configured, which is not a failure; `false` comes with
   * `reachableError` saying what went wrong.
   */
  reachable: boolean | null;
  reachableError: string;
  /** What Pando indexes, one row per mounted repository. */
  indexed: SearchIndexedRepo[];
  /** The running job, or the last finished one; null before the first. */
  reindex: SearchReindexJob | null;
  /**
   * Whether the last change reached the configuration file. `false` means the
   * companion has no configuration path — a test, or `serve --repo` — so the
   * change lives only until the process exits.
   */
  persisted: boolean;
};

/**
 * The fields a search-settings change may carry; an absent one is left alone.
 * Tokens are deliberately absent: a credential enters the process from the
 * environment or the file, never over the API.
 */
export type SearchSettingsPatch = {
  mcpUrl?: string;
  restUrl?: string;
  projectId?: string;
  allowRemote?: boolean;
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
  /**
   * Work was aimed at a sprint whose derived status is `completed`. Moving it
   * there would make that sprint's numbers lie, so it is refused outright
   * rather than per item.
   */
  | 'sprint_target_completed'
  /**
   * The project declares no status in the `triage` category, which is simply a
   * project without an inbox (ADR-033). It is a state to explain, not an error.
   */
  | 'no_triage_status'
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
  /** The folder already holds a `team.yaml` (GIT-US-0034). */
  | 'team_exists'
  /** The team already declares this project key (GIT-US-0037, R-PROJ-1). */
  | 'team_project_exists'
  /** A board, a sprint or a retro action still references the project; force it. */
  | 'team_project_referenced'
  /** A task toggle addressed a line that is no longer a checkbox (GIT-US-0010). */
  | 'task_list_mismatch'
  /** A write lost a race, or a sprint already has a retro. */
  | 'conflict'
  /**
   * The tunnel was refused because the companion runs without authentication:
   * publishing an unguarded workspace is never done, so this is a refusal to
   * explain, not an error to retry.
   */
  | 'tunnel_requires_token'
  /**
   * The YouTrack side, kept apart from the companion's own failures. In
   * particular, an instance that refuses the credential is reported as
   * `youtrack_unauthorized` over a `502` and never as a `401`, which a browser
   * would read as its own session expiring (GIT-EP-0011).
   */
  | 'youtrack_not_configured'
  | 'youtrack_unauthorized'
  | 'youtrack_forbidden'
  | 'youtrack_not_found'
  | 'youtrack_unreachable'
  /** No job of the queue has that id — a pruned job answers this too. */
  | 'sync_job_not_found'
  /** The job exists and is in a state the transition cannot be made from. */
  | 'sync_job_not_retryable'
  /** The engine has been closed, or was never started. */
  | 'sync_engine_not_running'
  /**
   * A reindex is already running; the running one was untouched. It is a
   * refusal to explain and not a failure to retry, so the card says so and
   * keeps following the job that is already there (GIT-US-0091).
   */
  | 'search_reindex_running'
  /** No Pando endpoint: there is nothing to index. */
  | 'search_not_configured'
  /**
   * The runtime cannot do this at all — browser-only mode asked for the agent,
   * say. It is a permanent property of the runtime, not a failure to retry and
   * not a permission the user could be granted, so it is its own code rather
   * than `read_only` (GIT-US-0053).
   */
  | 'not_supported'
  | 'internal';

export type ChangeEvent =
  | { kind: 'items'; repoId: string; ids: string[] }
  | { kind: 'kb'; repoId: string; paths: string[] }
  | { kind: 'repo'; repoId: string }
  | { kind: 'index'; repoId: string; stats: IndexStats }
  /** A `sync.job.*` frame, or the `resync` phase that asks for a reconcile. */
  | { kind: 'syncJob'; job: SyncJobEvent }
  /**
   * An `inbox.changed` frame: one triage decision, or one submission filed
   * straight into the queue. `pending` is the whole queue, so a sidebar badge
   * never needs a second call (ADR-033).
   */
  | { kind: 'inbox'; repoId: string; project: string; id: string; action: string; pending: number }
  /**
   * A `sprint.changed` frame: a sprint whose scope moved, by a close or by a
   * transfer. A dry run publishes none.
   */
  | {
      kind: 'sprint';
      sprint: string;
      board: string;
      state: string;
      carried: number;
      failed: number;
    }
  /**
   * A `youtrack.kb.conflict` frame: a page and the article it mirrors both
   * changed. The page was left exactly as it is and the incoming content went
   * to `conflictPath`.
   */
  /**
   * A `search.progress` frame: one step of a semantic-search reindex. It is
   * how the settings card follows a job it started without polling, and the
   * terminal phases (`completed`, `failed`) are what tell it to re-read the
   * settings for the finished job (GIT-US-0091).
   */
  | {
      kind: 'searchProgress';
      /** The reindex job this frame belongs to (`jobId`). */
      operationId: string;
      /** The repository being worked on; empty for the whole-job phases. */
      repoId: string;
      phase: SearchReindexPhase;
      percent: number;
      done: number;
      total: number;
      message: string;
    }
  | {
      kind: 'kbConflict';
      project: string;
      path: string;
      conflictPath: string;
      articleId?: string;
      direction: 'publish' | 'pull';
    };

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
  getTeam(team?: string): Promise<TeamSummary | null>;
  /**
   * Every open team repository, in mount order. A workspace may hold several
   * since GIT-US-0036; the web app remembers which one is active and passes it
   * as `team` to every call below that reaches a board, a sprint, a retro or a
   * team knowledge base (ADR-020).
   */
  listTeams(): Promise<TeamSummary[]>;
  /**
   * Resolves a `<projectKey>/<itemId>` reference across every open repository.
   * A reference into a project nobody cloned resolves to `cloned: false` with a
   * reason, never to a failure.
   */
  resolveRef(ref: string): Promise<RefResolution>;
  mountRepo(input: MountInput): Promise<RepoInfo>;
  /**
   * Scaffolds a backlog in a repository that has none, and returns the project
   * as `listProjects` reports it. Both modes go through the same core code, so
   * the file a companion writes and the file a browser writes are identical.
   */
  createProject(input: CreateProjectInput): Promise<ProjectSummary>;
  /**
   * Turns a mounted folder into a team repository, and returns it as `getTeam`
   * reports it. Both modes go through the same core code, so the `team.yaml` a
   * companion writes and the one a browser writes are identical.
   */
  createTeam(input: CreateTeamInput): Promise<TeamSummary>;
  /**
   * Declares a project repository in a team's `team.yaml` (story GIT-US-0037,
   * docs/04 §3.9). The link between a registered repository and the entry is
   * the project key alone, never a path and never a remote URL.
   *
   * It fails with `team_project_exists` when the team already declares the key,
   * and with `validation_failed` when the entry is missing what R-PROJ-1
   * requires.
   */
  addTeamProject(project: TeamProjectDraft, team?: string): Promise<TeamProjectResult>;
  /**
   * Disconnects a project from a team. Nothing in the project repository is
   * touched.
   *
   * It fails with `team_project_referenced` when a board, a sprint or a retro
   * action still points at an item of that project; `force` accepts leaving
   * those references pointing at a project the team no longer declares, and the
   * result then lists what was broken.
   */
  removeTeamProject(
    key: string,
    opts?: { force?: boolean },
    team?: string,
  ): Promise<TeamProjectResult>;
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
  search(query: SearchQuery): Promise<SearchResult>;
  validateItem(input: { id?: string; text?: string; path?: string }): Promise<Diagnostic[]>;
  /**
   * Everything that still points at an item: a child's `parent`, a story's
   * `milestone`, a typed link, a card in a board column, a sprint's scope, a
   * promoted retro action. The web app shows it before a delete, so the user
   * reads what breaks rather than a count (docs/05-web-app.md §8.4).
   */
  getItemReferences(id: string): Promise<ItemReferencesResult>;

  // write (all rev-checked)
  createItem(input: ItemDraft): Promise<Item>;
  updateItem(id: string, patch: ItemPatch, rev: string): Promise<Item>;
  moveItem(id: string, status: ItemStatus, rev: string): Promise<Item>;
  updateMany(ops: UpdateOp[]): Promise<BatchResult>;
  /**
   * Ticks or clears one task-list checkbox of the body. `line` is the 1-based
   * line of the marker inside the body, as the Markdown renderer stamped it on
   * the checkbox. The core rewrites that line and nothing else; the write is
   * rev-guarded like every other edit.
   */
  setTaskItem(id: string, line: number, checked: boolean, rev: string): Promise<Item>;
  /**
   * Removes an item. The default is the soft delete of docs/03 §7.1 — the file
   * stays with `deleted: true`, so the id is never reused, a merge cannot
   * resurrect it, and a child that names it as its parent still resolves.
   * `hard` removes the file instead.
   */
  deleteItem(id: string, rev: string, opts?: { hard?: boolean }): Promise<void>;
  /**
   * Appends a comment to an item. With no `author` the comment is attributed to
   * the git identity (`user.name`, `user.email`) of the repository the item
   * lives in; a handle is derived from the name for the file name.
   */
  addComment(id: string, body: string, author?: string): Promise<Comment>;
  /**
   * Ticks or clears one task-list checkbox of a comment. A comment has no id
   * of its own, so `path` names its file; `line` is the 1-based line of the
   * marker inside the comment body and `rev` is the comment's revision.
   */
  setCommentTask(
    id: string,
    path: string,
    line: number,
    checked: boolean,
    rev: string,
  ): Promise<Comment>;

  // inbox (ADR-033, docs/07 §5.3)
  /**
   * One page of a project's triage queue. `counts` and `pending` are over the
   * whole queue rather than over the page, and a snoozed item whose date has
   * arrived is already counted — and listed — as pending, because that is what
   * a reader sees.
   */
  listInbox(filter?: InboxFilter): Promise<InboxPage>;
  /** Files an ordinary draft straight into the triage queue instead of the backlog. */
  createInboxItem(draft: InboxDraft): Promise<Item>;
  /**
   * One triage decision. `accept` clears triage and moves the item into the
   * ordinary workflow — it can never change the item's type, because an item id
   * encodes its type for life (R-ID-3), so "this should have been an epic" is
   * answered by creating the right item and marking this one a duplicate.
   */
  triageInboxItem(input: InboxTriageInput): Promise<InboxTriageResult>;
  writePage(scope: KbScope, path: string, content: string, rev?: string): Promise<KbPage>;
  /**
   * Appends feedback notes to the feedback block at the end of a page, as the
   * git identity of the page's repository. Each note is anchored to the body
   * lines it quotes, and any later write through the core that changes those
   * lines drops the note (ADR-030). `rev` is checked like a page write.
   */
  addPageFeedback(
    scope: KbScope,
    path: string,
    notes: KbFeedbackNoteDraft[],
    rev?: string,
  ): Promise<KbPage>;

  // boards (docs/04-team-repository.md §5)
  /** Every board of the team repository; empty when none is open. */
  listBoards(team?: string): Promise<BoardSummary[]>;
  /**
   * One board, rendered over every open repository. A card whose project
   * nobody cloned comes back `remote: true` with a reason, never missing.
   */
  getBoard(slug: string, team?: string): Promise<BoardView>;
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
  updateBoard(slug: string, patch: BoardPatch, rev?: string, team?: string): Promise<BoardView>;
  /**
   * Creates a board in the team repository. A board is a view, so creating one
   * adds no item anywhere: the cards it shows are the ones its project scope
   * and its filters select. A slug already taken fails with `duplicate_id`.
   */
  createBoard(draft: BoardDraft, team?: string): Promise<BoardView>;
  /**
   * Deletes a board file, and nothing else — every item its cards referenced
   * stays where it is. A board a sprint still names fails with `board_in_use`,
   * or `sprint_already_active` when that sprint is running.
   */
  deleteBoard(slug: string, rev?: string, team?: string): Promise<void>;

  // sprints (docs/04-team-repository.md §8)
  /** The sprints of the team repository, newest ids last; empty when none. */
  listSprints(filter?: SprintFilter, team?: string): Promise<SprintSummary[]>;
  /** One sprint: its scope, the candidates for it and its metrics. */
  getSprint(id: string, team?: string): Promise<SprintView>;
  /** Creates a sprint; the core allocates the id from the team key. */
  createSprint(input: SprintDraft, team?: string): Promise<SprintResult>;
  /**
   * Changes the goal, the dates or the scope. Every change is one write to the
   * sprint file in the team repository, so moving an item in or out of a
   * sprint stays legal for a project nobody cloned (docs/04 R-SPR-2).
   */
  updateSprint(id: string, patch: SprintPatch, rev?: string, team?: string): Promise<SprintResult>;
  /**
   * Makes a sprint active: its scope becomes its commitment and its board is
   * pointed at it. A board already running a sprint is refused once with
   * `sprint_already_active`; repeat with `force` to run two at once.
   */
  startSprint(id: string, rev?: string, force?: boolean, team?: string): Promise<SprintResult>;
  /**
   * Closes a sprint and reports completed against incomplete work. Closing
   * modifies no item by itself: `carry` carries one explicit decision per
   * unfinished item (R-SPR-3).
   */
  closeSprint(id: string, input?: SprintCloseInput, team?: string): Promise<SprintResult>;
  /**
   * Moves the unfinished references of one sprint into another sprint or back
   * to their project backlogs, without closing anything. A per-item refusal is
   * not an error: it comes back on its own `report.carried[].error` line, so
   * the rest of the transfer still happened (R-SPR-8).
   */
  transferSprintItems(
    id: string,
    input?: SprintTransferInput,
    team?: string,
  ): Promise<SprintResult>;
  /**
   * One sprint's burndown, cumulative flow diagram and flow statistics, with
   * the provenance of the history behind them (docs/04 §12). The provenance is
   * part of the answer, not decoration: the companion reconstructs the series
   * from git, and a host without git says so and shows the approximation it
   * can draw from the `updated` stamps instead of inventing a curve.
   */
  getSprintMetrics(id: string, team?: string): Promise<SprintMetricsView>;

  // retrospectives (docs/04-team-repository.md §9)
  /**
   * The retros of the team repository, newest first, with the improvement
   * actions they left open. The open actions come back with the listing
   * because a team starting a new retro has to see them first (§9.1, step 7).
   */
  listRetros(filter?: RetroFilter, team?: string): Promise<RetroListing>;
  /** One retro: its notes, its themes by votes, its actions and what it carried. */
  getRetro(id: string, team?: string): Promise<RetroView>;
  /** Creates a retro; the core allocates the id from the team key. */
  createRetro(input: RetroDraft, team?: string): Promise<RetroResult>;
  /**
   * Applies one session's edits. Notes and actions are added, changed and
   * removed one entry at a time, so two participants writing at once produce
   * diffs that merge rather than an entry that disappears.
   */
  updateRetro(id: string, patch: RetroPatch, rev?: string, team?: string): Promise<RetroResult>;
  /**
   * Turns one improvement action into a task in a project repository, and
   * writes the produced reference back into the retro. A project no open
   * repository serves is refused with `repo_not_cloned` rather than half
   * written, and the UI then offers the action as Markdown to paste
   * (docs/04 R-RETRO-2).
   */
  promoteRetroAction(input: RetroPromotion, team?: string): Promise<RetroResult>;

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

  // semantic search settings (`GET|PATCH /api/v1/search/settings`, GIT-US-0091)
  /**
   * Where Pando is, what it indexes and whether the endpoint answers right
   * now. The reachability probe runs while the request is answered, so a call
   * is a diagnosis and not a cached one.
   */
  getSearchSettings(): Promise<SearchSettings>;
  /**
   * Changes them. The running process adopts the patch first and the
   * configuration file is written afterwards, and `persisted` says whether the
   * second half happened. A non-loopback URL without `allowRemote`, or a URL
   * that is not one, is refused with `validation_failed` and nothing is
   * adopted.
   */
  updateSearchSettings(patch: SearchSettingsPatch): Promise<SearchSettings>;
  /**
   * Starts a reindex and answers the job it queued; the work runs in the
   * background and reports on the `search.progress` topic, which arrives here
   * as a `searchProgress` change event. A second call while one runs is
   * refused with `search_reindex_running`, and a companion with nothing to
   * index with answers `search_not_configured`.
   *
   * `repo` scopes the source-tree half to one mounted repository — the
   * workspace list's "enable semantic search" switch registers and indexes
   * that repository alone — and an id the companion does not serve is refused
   * with `not_found` (GIT-US-0101).
   */
  reindexSearch(repo?: string): Promise<SearchReindexJob>;

  // MCP write tools (`GET|PATCH /api/v1/mcp/settings`)
  /**
   * Whether the MCP server advertises its write tools. A runtime that has no
   * MCP surface answers `supported: false` rather than throwing.
   */
  getMcpSettings(): Promise<McpSettings>;
  /**
   * Turns the MCP write tools on or off and persists the choice. It is always
   * an explicit user action: nothing in the app may call it on its own, since
   * it grants every agent the user runs the right to edit the backlog.
   */
  setMcpWriteTools(allowWrite: boolean): Promise<McpSettings>;

  // public tunnel (`GET|POST|DELETE /api/v1/tunnel`)
  /**
   * Whether a public tunnel is running and, if so, on which URL. A runtime
   * that cannot tunnel answers `supported: false` rather than throwing.
   */
  getTunnel(): Promise<TunnelStatus>;
  /**
   * Opens or closes the tunnel. Enabling answers as soon as the hostname is
   * known — `state` is then `starting`, not `connected` — so the caller polls
   * `getTunnel()` until it settles. It is always an explicit user action:
   * nothing in the app may call this on its own.
   */
  setTunnel(enabled: boolean): Promise<TunnelStatus>;

  // YouTrack (`/api/v1/youtrack/*`, story GIT-US-0055)
  /**
   * One project's connection. It never carries the token: `hasToken` and
   * `tokenSource` are all a surface is told, by design.
   */
  getYouTrackSettings(scope?: YouTrackScope): Promise<YouTrackSettings>;
  /**
   * Changes it. The patch is sparse and every value is applied as given, so
   * `''` clears — including `token: ''`, which forgets the stored credential.
   * The answer's `persisted` says whether the change reached the configuration
   * file or only the running process.
   */
  updateYouTrackSettings(
    patch: YouTrackSettingsPatch,
    scope?: YouTrackScope,
  ): Promise<YouTrackSettings>;
  /**
   * Probes the instance and reports who the credential authenticates as. With
   * `url` and `token` it tests a connection the user has typed but not saved,
   * so a wrong token is caught before it is written anywhere.
   */
  testYouTrackConnection(
    probe?: { url?: string; token?: string },
    scope?: YouTrackScope,
  ): Promise<YouTrackTestResult>;
  /**
   * Project autosuggest; `q` filters by name and short name.
   *
   * `probe` reads the list with a URL and a token the user has typed but not
   * saved, the way `testYouTrackConnection` does: choosing the remote project
   * is part of connecting, so the picker has to work before anything is
   * stored. Without it the saved connection is used.
   */
  listYouTrackProjects(
    q?: string,
    scope?: YouTrackScope,
    probe?: { url?: string; token?: string },
  ): Promise<YouTrackProject[]>;
  /**
   * The custom fields of a YouTrack project, so the field map offers real names
   * instead of free text. `project` defaults to the linked one.
   */
  listYouTrackFields(project?: string, scope?: YouTrackScope): Promise<YouTrackFieldList>;

  // YouTrack import (`/api/v1/youtrack/issues` and the import operations,
  // stories GIT-US-0054 and GIT-US-0047; epic GIT-EP-0012)
  /**
   * The issue autosuggest of the import dialog. The companion composes the
   * effective query from the linked project, the caller's `q` and the preset,
   * and resolves `linked` from its own index, so a result that a previous
   * import already created says so.
   */
  searchYouTrackIssues(
    query: YouTrackIssueQuery,
    scope?: YouTrackScope,
  ): Promise<YouTrackIssuePage>;
  /**
   * What an import would do, without writing anything: one plan row per issue
   * with the action, the mapped type, the resolved parent and every warning the
   * mapper raised. It takes the same options `runYouTrackImport` takes, which
   * is what makes the preview honest.
   */
  previewYouTrackImport(
    options: YouTrackImportOptions,
    scope?: YouTrackScope,
  ): Promise<YouTrackImportPreviewResult>;
  /**
   * Queues the import and answers the id of the job that runs it — see
   * `YouTrackImportRun`. It never throws for a single failing issue: the job
   * records what each issue produced, and a partial failure is read back from
   * the queue rather than raised here.
   */
  runYouTrackImport(
    options: YouTrackImportOptions,
    scope?: YouTrackScope,
  ): Promise<YouTrackImportRun>;

  // background jobs (`/api/v1/sync/jobs`, story GIT-US-0078)
  /**
   * One page of the queue plus the whole queue's counts. The list is the source
   * of truth: the `sync.job.*` stream is a live hint that a client may miss
   * frames from, so a reconnect reconciles from here.
   */
  /**
   * The synchronization state of the selected knowledge-base pages. Without
   * `remote` no request leaves the process: the answer comes from each page's
   * own `external` entry and the content it would publish, which is what makes
   * a whole tree affordable to ask about.
   */
  kbSyncStatus(selector?: KbSyncSelector): Promise<KbSyncStatusResult>;
  /**
   * Queues a publish of the selected pages. It never blocks on the network and
   * it never publishes the `## Feedback` block, which stays in the repository
   * (ADR-030).
   */
  publishKbPage(selector: KbSyncSelector): Promise<KbSyncJobResult>;
  /** Queues a pull of the selected pages from their articles. */
  pullKbPage(selector: KbSyncSelector): Promise<KbSyncJobResult>;
  /**
   * Forgets the article one page mirrors: the `external:` entry leaves the
   * page's front matter and nothing else happens.
   *
   * It is local and immediate — no job, and the instance is never called — so
   * the article is neither deleted nor archived, and a page unlinked by mistake
   * is recovered by publishing it again. It needs no connection either: a page
   * keeps its reference after a project is disconnected, which is exactly when
   * somebody wants to clean one up.
   */
  unlinkKbPage(selector: KbSyncSelector): Promise<KbUnlinkResult>;
  /**
   * Queues a push of one comment, or of every comment of an item that carries
   * no YouTrack reference yet. The answer says what was *queued*: the comment's
   * `external` entry is what says it arrived.
   */
  pushCommentToYoutrack(input: CommentPushInput): Promise<CommentPushResult>;

  listSyncJobs(filter?: SyncJobFilter): Promise<SyncJobPage>;
  /** One job, or `sync_job_not_found` — which a pruned job also answers. */
  getSyncJob(id: string): Promise<SyncJob>;
  /**
   * Re-queues a failed or cancelled job; any other state is
   * `sync_job_not_retryable`. A failed job keeps its id; a cancelled one cannot
   * (the state machine has no edge out of `cancelled`) and comes back as a new
   * job, so the answer's `id` is the one to follow.
   */
  retrySyncJob(id: string): Promise<SyncJob>;
  /** Withdraws a queued or running job; any other state is `sync_job_not_retryable`. */
  cancelSyncJob(id: string): Promise<SyncJob>;
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

  // agent (Pando AG-UI, GIT-EP-0018 / GIT-US-0053)
  /**
   * The adapter's discovery document: which agents exist and which optional
   * halves of the protocol — frontend tools, human-in-the-loop, shared state,
   * interrupts — this deployment implements.
   */
  getAgentInfo(options?: AgentRequestOptions): Promise<AguiInfo>;
  /** Liveness of the adapter behind the companion, for the settings card. */
  getAgentHealth(options?: AgentRequestOptions): Promise<AgentHealth>;
  /**
   * Runs the agent and yields AG-UI events as they arrive.
   *
   * It is an async iterable rather than a callback feed because that is the
   * shape `PandoThread` consumes (`client.run`), so the SDK's own reducer can
   * be dropped straight onto this seam. The HTTP failure surfaces on the first
   * `next()`, as a `ProviderError`; aborting `options.signal` ends the
   * iteration.
   *
   * The whole transcript is resent on every turn: Pando forwards only the
   * trailing user message, and a trailing `tool` message is what resumes an
   * interrupted run rather than starting a new one.
   */
  runAgent(input: RunAgentInput, options?: AgentRunOptions): AsyncIterable<AguiEvent>;
  /** Every thread the adapter remembers for a repository, newest first. */
  listAgentThreads(options?: AgentRequestOptions): Promise<AgentThreadSummary[]>;
  /** The stored transcript of one thread, for restoring it after a reload. */
  getAgentThreadMessages(threadId: string, options?: AgentRequestOptions): Promise<AguiMessage[]>;
  /**
   * Re-attaches to a thread whose run is still live — the tab that owned it
   * reloaded, say. It yields the same event stream `runAgent` does, without
   * starting a run.
   */
  streamAgentThread(threadId: string, options?: AgentRunOptions): AsyncIterable<AguiEvent>;
  /** Forgets a thread and its transcript. */
  deleteAgentThread(threadId: string, options?: AgentRequestOptions): Promise<void>;
  /**
   * Cancels the run in flight on a thread. The stream then ends with a
   * `RUN_ERROR` carrying `code: 'cancelled'`.
   */
  cancelAgentRun(threadId: string, options?: AgentRequestOptions): Promise<void>;

  subscribe(handler: (event: ChangeEvent) => void): Unsubscribe;
}

// ------------------------------------------------------------------- agent

/**
 * What every agent call needs: which repository's adapter to talk to, and a
 * signal to give up with. One `agui-serve` runs per repository and the
 * companion routes `repo` onto `{url, token}` of its own, so the Pando token
 * never reaches the browser (decision of 2026-09-13).
 */
export type AgentRequestOptions = {
  /** Repository id; the companion's default repository when omitted. */
  repo?: string;
  signal?: AbortSignal;
};

/** A streaming agent call. Same shape; named apart so the docs stay honest. */
export type AgentRunOptions = AgentRequestOptions;

/** `GET /api/v1/agent/health` → is there an adapter answering at all. */
export type AgentHealth = {
  ok: boolean;
  /** Adapter version, when it reports one. */
  version?: string;
  /** Why it is not ok; absent when it is. */
  detail?: string;
};

/** One row of `GET /api/v1/agent/threads`. */
export type AgentThreadSummary = {
  id: string;
  /** A title the adapter derived, usually the opening message. */
  title?: string;
  /** RFC 3339. */
  createdAt?: string;
  updatedAt?: string;
  messageCount?: number;
  /** True while a run is still attached to this thread. */
  running?: boolean;
};

export type { AguiEvent, AguiInfo, AguiMessage, RunAgentInput };

/** How a retro listing is narrowed; the filters are ANDed. */
/** A draft filed straight into the triage queue (ADR-033). */
export type InboxDraft = ItemDraft & {
  /** Free text: `web`, `mcp`, `youtrack`, the name of a form. */
  source?: string;
  /** When the submission arrived, which is not when the file was written. */
  received?: string;
};

/**
 * What closing a sprint decides.
 *
 * `dryRun` computes the whole report and writes nothing, not even a write set:
 * it is what the confirmation dialog renders, so that nothing is committed
 * before a person has seen what would move where.
 */
export type SprintCloseInput = {
  /** One explicit decision per unfinished item; it wins over `transfer`. */
  carry?: SprintCarry[];
  /** One destination for every unfinished reference. */
  transfer?: SprintTransfer;
  rev?: string;
  dryRun?: boolean;
};

/** What a standalone transfer moves, and where. `mode` defaults to `next`. */
export type SprintTransferInput = {
  mode?: SprintTransferMode;
  target?: string;
  carry?: SprintCarry[];
  rev?: string;
  dryRun?: boolean;
};

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
  /** The team repository the board belongs to (GIT-US-0036). */
  team?: string;
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
  youtrackSupported: false,
  youtrack: false,
  searchSettings: false,
  agent: false,
};
