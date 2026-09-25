/**
 * `DataProvider` for companion mode (docs/05-web-app.md §4.2).
 *
 * Thin by design: every method is one REST call against the `/api/v1` surface
 * of `gintrack serve` (docs/07-cli-and-api.md §5) plus a defensive mapping into
 * the shapes the UI already speaks. No parsing, indexing or validation happens
 * here — the companion runs the same Go core the browser mode compiles to WASM.
 *
 * Three rules the whole file follows:
 *
 * - **Errors are RFC 7807 problem documents.** The client switches on the
 *   stable `code` field and only falls back to the HTTP status when a body is
 *   missing or unparseable. Everything the UI sees is a `ProviderError`.
 * - **Writes are rev-checked.** Every mutation sends `If-Match: <rev>`; a
 *   rejected precondition becomes `ProviderError('stale_revision')`, which the
 *   editor already knows how to present.
 * - **`401` is not a failure to swallow.** It clears the stored token and
 *   raises `CompanionUnauthorizedError`, so the UI can ask for a new one.
 *
 * `subscribe()` attaches to the `/api/v1/events` WebSocket, translates the
 * documented envelopes into `ChangeEvent`s, reconnects with exponential
 * backoff and jitter, resumes with the last `seq`, and degrades to a plain
 * interval refresh signal when the socket cannot be opened at all.
 */

import { parseSSE } from '@pando-ai/sdk/agui/client';

import { resolveCompanionBaseUrl } from '@/api/detect';
import type {
  AgentHealth,
  AgentRequestOptions,
  AgentRunOptions,
  AgentThreadSummary,
  AguiEvent,
  AguiInfo,
  AguiMessage,
  RunAgentInput,
  CommentPushEntry,
  CommentPushInput,
  CommentPushResult,
  ConflictField,
  External,
  InboxDraft,
  InboxFilter,
  InboxPage,
  InboxStatus,
  InboxTriageAction,
  InboxTriageInput,
  InboxTriageResult,
  ItemInbox,
  KbPageSyncStatus,
  KbSyncJobResult,
  KbUnlinkResult,
  KbSyncSelector,
  KbSyncState,
  KbSyncStatusResult,
  SprintCloseInput,
  SprintTransferInput,
  BatchResult,
  BoardMoveResult,
  BoardSummary,
  BoardDraft,
  BoardPatch,
  BoardView,
  CardMove,
  Capabilities,
  CreateProjectInput,
  CreateTeamInput,
  EnableInboxInput,
  ChangeEvent,
  Comment,
  KbFeedbackNoteDraft,
  ConflictAnalysis,
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
  ItemReference,
  ItemReferencesResult,
  ItemStatus,
  ItemType,
  KbNode,
  KbPage,
  KbScope,
  MountInput,
  Priority,
  ProjectSummary,
  GitCommit,
  GitRepoStatus,
  GitSettings,
  GitSettingsPatch,
  ProviderErrorCode,
  ProviderErrorDetails,
  RefResolution,
  RepoInfo,
  SearchCodeIndex,
  SearchIndexedRepo,
  SearchHit,
  SearchQuery,
  SearchReindexJob,
  SearchReindexKbStats,
  SearchReindexPhase,
  SearchReindexRepo,
  SearchResult,
  SearchSettings,
  SearchSettingsPatch,
  SnapshotRefresh,
  SnapshotResult,
  RetroDraft,
  RetroFilter,
  RetroListing,
  RetroPatch,
  RetroPromotion,
  RetroResult,
  RetroView,
  SprintDraft,
  SprintFilter,
  SprintPatch,
  SprintMetricsView,
  SprintResult,
  SprintSummary,
  SprintView,
  SyncOptions,
  SyncRepoStatus,
  SyncResult,
  SyncEngineSettings,
  SyncJob,
  SyncJobCounts,
  SyncJobError,
  SyncJobEvent,
  SyncJobEventPhase,
  SyncJobFilter,
  SyncJobPage,
  SyncJobState,
  SyncSettings,
  SyncSettingsPatch,
  TeamProjectDraft,
  TeamProjectResult,
  TeamSummary,
  TunnelState,
  McpSettings,
  TunnelStatus,
  Unsubscribe,
  UpdateOp,
  YouTrackField,
  YouTrackFieldMapping,
  YouTrackFieldValue,
  YouTrackFieldList,
  YouTrackImportOptions,
  YouTrackImportPlanItem,
  YouTrackImportPreviewResult,
  YouTrackImportRun,
  YouTrackImportWarning,
  YouTrackIssue,
  YouTrackIssuePage,
  YouTrackIssueQuery,
  YouTrackKbSync,
  YouTrackKbSyncDirection,
  YouTrackProject,
  YouTrackPushComments,
  YouTrackScope,
  YouTrackSettings,
  YouTrackSettingsPatch,
  YouTrackTestResult,
  YouTrackTokenSource,
  CoverageFilter,
  CoverageList,
  ImpactQuery,
  ImpactReport,
  ImpactReportQuery,
  DeltaPreviewOperation,
  SpecDeltaPreviewInput,
  SpecLintFinding,
  SpecLintInput,
  ImpactResult,
  RequirementDraft,
  RequirementFilter,
  RequirementList,
  RequirementPatch,
  RequirementRead,
  RequirementWriteResult,
  SpecFilter,
  TracedRequirement,
} from '@/api/provider';
import { ProviderError, searchProjectKeys } from '@/api/provider';
import { authorizationHeader, clearToken, hasToken, withTokenQuery } from '@/api/token';
import type { Link } from '@/core-bridge/api';

/** Every route lives under this prefix (docs/07-cli-and-api.md §5). */
export const API_PREFIX = '/api/v1';

/** Where the event stream lives. */
export const EVENTS_PATH = `${API_PREFIX}/events`;

/** Reconnect schedule: 500 ms doubling up to 30 s, then jittered. */
export const RECONNECT_BASE_MS = 500;
export const RECONNECT_MAX_MS = 30_000;

/** Consecutive failed opens before the stream degrades to interval polling. */
export const MAX_SOCKET_ATTEMPTS = 3;

/** How often the degraded stream asks the UI to refetch. */
export const POLL_INTERVAL_MS = 30_000;

/** Topics the app cares about; the server sends everything without a filter. */
const SUBSCRIBE_TOPICS = [
  'file.changed',
  'index.updated',
  'item.changed',
  'sync.progress',
  'conflict.detected',
  // The background job engine (GIT-US-0074). The five topics are the contract;
  // `sync.job.started` is subscribed to even though the engine does not report
  // the queued -> running transition through its seam yet.
  'sync.job.queued',
  'sync.job.started',
  'sync.job.progress',
  'sync.job.done',
  'sync.job.failed',
  // The triage queue (ADR-033): one frame per decision, carrying the whole
  // queue's pending count so a sidebar badge never needs a second call.
  'inbox.changed',
  // A sprint whose scope moved, by a close or by a transfer. A dry run
  // publishes none (GIT-US-0085).
  'sprint.changed',
  // A knowledge-base page and its article both changed; the page was left
  // untouched and the incoming content went to `<page>.conflict.md`.
  'youtrack.kb.conflict',
  // One step of a semantic-search reindex, so the settings card follows the
  // job it started without polling (GIT-US-0091).
  'search.progress',
];

/** The `sync.job.*` topics, by the phase each one carries. */
const SYNC_JOB_PHASES: Record<string, SyncJobEventPhase> = {
  'sync.job.queued': 'queued',
  'sync.job.started': 'started',
  'sync.job.progress': 'progress',
  'sync.job.done': 'done',
  'sync.job.failed': 'failed',
};

/** What the companion can do before `GET /capabilities` answers. */
export const companionCapabilities: Capabilities = {
  write: true,
  git: true,
  ssh: true,
  watch: true,
  fullTextSearch: 'core',
  mcp: false,
  openInEditor: true,
  maxBatchWrite: 50,
  youtrackSupported: true,
  youtrack: false,
  searchSettings: true,
  agent: false,
};

/** State of the event socket, surfaced in Settings. */
export type ConnectionState =
  'idle' | 'connecting' | 'open' | 'reconnecting' | 'polling' | 'closed';

/** The slice of `WebSocket` this provider uses, so tests can supply a fake. */
export type WebSocketLike = {
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: ((event: unknown) => void) | null;
  onmessage: ((event: { data: unknown }) => void) | null;
  onclose: ((event: unknown) => void) | null;
  onerror: ((event: unknown) => void) | null;
};

export type WebSocketFactory = (url: string) => WebSocketLike;

export type CompanionProviderOptions = {
  /** `''` for same-origin; `http://127.0.0.1:7317` from the Vite dev server. */
  baseUrl?: string;
  /** Version reported by `GET /health`, shown in the mode badge tooltip. */
  version?: string | null;
  /** Injected by tests. */
  fetchImpl?: typeof fetch;
  /** Injected by tests; production uses the global `WebSocket`. */
  webSocketFactory?: WebSocketFactory | null;
  /** Injected by tests to make backoff deterministic. */
  random?: () => number;
  /** Skips the capabilities request (tests, and the fake companion). */
  capabilities?: Capabilities;
};

/**
 * A `401`. The interface's `ProviderErrorCode` union has no `unauthorized`
 * member (docs/07 §5.4 does), so the typed answer is this subclass carrying
 * `permission_denied` plus an `unauthorized` discriminant. The token is already
 * cleared by the time it is thrown.
 */
export class CompanionUnauthorizedError extends ProviderError {
  readonly unauthorized = true as const;

  constructor(message: string) {
    super('permission_denied', message);
    this.name = 'CompanionUnauthorizedError';
  }
}

export function isUnauthorized(error: unknown): error is CompanionUnauthorizedError {
  return error instanceof CompanionUnauthorizedError;
}

// ------------------------------------------------------------------ problems

/** RFC 7807 problem document (docs/07-cli-and-api.md §5.4). */
export type ProblemDocument = {
  title?: string;
  status?: number;
  detail?: string;
  code?: string;
  currentRev?: string;
  /** The fields a refused conditional write would still change (`stale_revision`). */
  conflicts?: ConflictField[];
  instance?: string;
  errors?: { field?: string; code?: string; message?: string }[];
};

/**
 * `code` catalog → provider codes. Codes the interface has no member for are
 * folded into the closest one and the message keeps the original wording.
 */
const PROBLEM_CODES: Record<string, ProviderErrorCode> = {
  stale_revision: 'stale_revision',
  conflict: 'stale_revision',
  validation_failed: 'validation_failed',
  workflow_transition_denied: 'validation_failed',
  // The body changed since it was rendered, so the line a toggle addressed is
  // no longer a checkbox: re-read the item, do not retry.
  task_list_mismatch: 'task_list_mismatch',
  not_found: 'not_found',
  repo_not_registered: 'not_found',
  read_only: 'read_only',
  forbidden: 'permission_denied',
  git_conflict: 'git_conflict',
  git_dirty: 'git_conflict',
  git_auth_failed: 'git_auth_failed',
  repo_not_cloned: 'repo_not_cloned',
  wip_limit_exceeded: 'wip_limit_exceeded',
  sprint_overlap: 'sprint_overlap',
  sprint_already_active: 'sprint_already_active',
  // A transfer aimed at a sprint that is already over (GIT-US-0085).
  sprint_target_completed: 'sprint_target_completed',
  // A project with no triage status simply has no inbox (ADR-033).
  no_triage_status: 'no_triage_status',
  // Enabling an inbox that exists, or over an ordinary `triage` status (GIT-US-0100).
  inbox_already_enabled: 'inbox_already_enabled',
  triage_status_id_taken: 'triage_status_id_taken',
  project_exists: 'project_exists',
  team_exists: 'team_exists',
  team_project_exists: 'team_project_exists',
  team_project_referenced: 'team_project_referenced',
  // Opening a tunnel over a companion started without authentication is
  // refused, not failed: the UI explains it instead of offering a retry.
  tunnel_requires_token: 'tunnel_requires_token',
  // The YouTrack side. The companion answers 502 for every remote failure so a
  // browser never mistakes the instance refusing a token for its own session
  // expiring; the code is what tells the card which message to render.
  youtrack_not_configured: 'youtrack_not_configured',
  youtrack_unauthorized: 'youtrack_unauthorized',
  youtrack_forbidden: 'youtrack_forbidden',
  youtrack_not_found: 'youtrack_not_found',
  youtrack_unreachable: 'youtrack_unreachable',
  // The background job engine (GIT-US-0078). Each of the three is a different
  // thing for the queue table to say: a job that is gone, a transition the
  // job's state does not allow, and an engine that is not running at all.
  sync_job_not_found: 'sync_job_not_found',
  sync_job_not_retryable: 'sync_job_not_retryable',
  sync_engine_not_running: 'sync_engine_not_running',
  // A reindex that is already running is a refusal to explain, and a companion
  // with nothing to index with is a configuration to fix; neither is a retry.
  search_reindex_running: 'search_reindex_running',
  search_not_configured: 'search_not_configured',
  // No tracer, coverage backend or impact resolver for this repository
  // (GIT-US-0127): a state for the view to explain, not a retry.
  unavailable: 'unavailable',
  index_unavailable: 'internal',
  rate_limited: 'internal',
  internal: 'internal',
};

function codeFromStatus(status: number, canWrite: boolean): ProviderErrorCode {
  switch (status) {
    case 403:
      // A companion started with `--read-only` answers 403 to every write.
      return canWrite ? 'permission_denied' : 'read_only';
    case 404:
      return 'not_found';
    case 409:
    case 412:
    case 428:
      return 'stale_revision';
    case 422:
      return 'validation_failed';
    default:
      return 'internal';
  }
}

function problemMessage(problem: ProblemDocument, fallback: string): string {
  const head = problem.detail ?? problem.title ?? fallback;
  const fields = (problem.errors ?? [])
    .map((entry) => {
      const label = entry.field ?? entry.code;
      const text = entry.message ?? entry.code ?? '';
      return label ? `${label}: ${text}` : text;
    })
    .filter((line) => line.length > 0);
  return fields.length > 0 ? `${head} (${fields.join('; ')})` : head;
}

// ------------------------------------------------------------- json plumbing

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

function asNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined;
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function asStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  return value.filter((entry): entry is string => typeof entry === 'string');
}

/** Assigns only defined values, so `exactOptionalPropertyTypes` stays happy. */
function put<T extends object, K extends keyof T>(
  target: T,
  key: K,
  value: T[K] | undefined,
): void {
  if (value !== undefined) target[key] = value;
}

function parseProblem(body: unknown): ProblemDocument | null {
  const record = asRecord(body);
  if (!record) return null;
  const problem: ProblemDocument = {};
  put(problem, 'title', asString(record['title']));
  put(problem, 'status', asNumber(record['status']));
  put(problem, 'detail', asString(record['detail']));
  put(problem, 'code', asString(record['code']) ?? codeFromType(record['type']));
  put(problem, 'currentRev', asString(record['currentRev']));
  put(problem, 'instance', asString(record['instance']));
  const errors = asArray(record['errors'])
    .map((entry) => asRecord(entry))
    .filter((entry): entry is Record<string, unknown> => entry !== null)
    .map((entry) => {
      const detail: { field?: string; code?: string; message?: string } = {};
      put(detail, 'field', asString(entry['field']));
      put(detail, 'code', asString(entry['code']));
      put(detail, 'message', asString(entry['message']));
      return detail;
    });
  if (errors.length > 0) problem.errors = errors;
  const conflicts = asArray(record['conflicts'])
    .map((entry) => asRecord(entry))
    .filter((entry): entry is Record<string, unknown> => entry !== null)
    .flatMap((entry): ConflictField[] => {
      const field = asString(entry['field']);
      if (field === undefined) return [];
      const conflict: ConflictField = { field };
      put(conflict, 'current', asString(entry['current']));
      put(conflict, 'proposed', asString(entry['proposed']));
      return [conflict];
    });
  if (conflicts.length > 0) problem.conflicts = conflicts;
  return problem;
}

/**
 * An RFC 7807 `type` URI ends in the same slug the `code` field carries
 * (`https://…/problems/tunnel_requires_token`), so a document that names only
 * the type still yields a code to switch on. Anything that is not a bare
 * snake-case slug — `about:blank`, most of all — yields nothing.
 */
function codeFromType(value: unknown): string | undefined {
  const type = asString(value);
  if (type === undefined) return undefined;
  const slug = type.split(/[/#]/).pop() ?? '';
  return /^[a-z][a-z0-9_]*$/.test(slug) ? slug : undefined;
}

/**
 * Maps the MCP settings document defensively. Every unknown value collapses to
 * the safe reading — unsupported, read-only — so a card can never claim the
 * write tools are on when the answer was unparseable.
 */
function toMcpSettings(body: unknown): McpSettings {
  const record = asRecord(body) ?? {};
  return {
    supported: record['supported'] === true,
    allowWrite: record['allowWrite'] === true,
    http: record['http'] === true,
    persisted: record['persisted'] === true,
    configPath: asString(record['configPath']) ?? '',
    tools: asArray(record['tools']).filter((tool): tool is string => typeof tool === 'string'),
  };
}

/** The states `GET /api/v1/tunnel` may report; anything else reads as `off`. */
const TUNNEL_STATES: TunnelState[] = ['off', 'starting', 'connected', 'reconnecting', 'error'];

/**
 * Maps a tunnel document defensively. Every unknown value collapses to the
 * safe reading: not supported, not running, no URL — never a card that claims
 * a workspace is private when the answer was unparseable, and never one that
 * presents an unknown state as a live public link.
 */
function toTunnelStatus(body: unknown): TunnelStatus {
  const record = asRecord(body) ?? {};
  const state = asString(record['state']);
  return {
    supported: record['supported'] === true,
    provider: asString(record['provider']) ?? '',
    state: TUNNEL_STATES.find((known) => known === state) ?? 'off',
    url: asString(record['url']) ?? '',
    connections: asNumber(record['connections']) ?? 0,
    since: asString(record['since']) ?? null,
    error: asString(record['error']) ?? '',
    tokenConfigured: record['tokenConfigured'] === true,
  };
}

// ------------------------------------------------------------------ mappers

const ITEM_TYPES: ItemType[] = ['epic', 'story', 'task', 'milestone', 'spec', 'comment'];
const PRIORITIES: Priority[] = ['critical', 'high', 'medium', 'low'];

function asItemType(value: unknown): ItemType | undefined {
  const text = asString(value);
  return text !== undefined && (ITEM_TYPES as string[]).includes(text)
    ? (text as ItemType)
    : undefined;
}

function asPriority(value: unknown): Priority | undefined {
  const text = asString(value);
  return text !== undefined && (PRIORITIES as string[]).includes(text)
    ? (text as Priority)
    : undefined;
}

/** REST links are `{relation,target}`; the core model calls the field `kind`. */
function toLinks(value: unknown): Link[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const links: Link[] = [];
  for (const entry of value) {
    const record = asRecord(entry);
    if (!record) continue;
    const kind = asString(record['kind']) ?? asString(record['relation']);
    const target = asString(record['target']);
    if (kind === undefined || target === undefined) continue;
    const link: Link = { kind: kind as Link['kind'], target };
    put(link, 'note', asString(record['note']));
    links.push(link);
  }
  return links;
}

function malformed(what: string): ProviderError {
  return new ProviderError('internal', `The companion returned a malformed ${what}.`);
}

/**
 * The two REST groups of the outbound YouTrack integration, in one place.
 *
 * They sit beside the rest of the `/youtrack` group the settings card and the
 * import dialog already use. Keeping the two prefixes here — rather than
 * spelled out at each call site — is what makes a change to either a one-line
 * change in this file.
 */
const YOUTRACK_KB_PATH = `${API_PREFIX}/youtrack/kb`;
const YOUTRACK_COMMENTS_PATH = `${API_PREFIX}/youtrack/comments`;

/** The five states a knowledge-base page can be in against its article. */
const KB_SYNC_STATES: KbSyncState[] = [
  'unlinked',
  'in_sync',
  'local_ahead',
  'remote_ahead',
  'conflict',
];

function toKbPageSyncStatus(value: unknown): KbPageSyncStatus {
  const record = asRecord(value) ?? {};
  const state = asString(record['state']);
  const row: KbPageSyncStatus = {
    path: asString(record['path']) ?? '',
    linked: asBoolean(record['linked']) ?? false,
    state: KB_SYNC_STATES.includes(state as KbSyncState) ? (state as KbSyncState) : 'unlinked',
  };
  put(row, 'articleId', asString(record['articleId']));
  put(row, 'url', asString(record['url']));
  put(row, 'syncedAt', asString(record['syncedAt']));
  put(row, 'error', asString(record['error']));
  return row;
}

export function toKbSyncStatusResult(value: unknown, selector: KbSyncSelector): KbSyncStatusResult {
  const record = asRecord(value) ?? {};
  return {
    project: asString(record['project']) ?? selector.project ?? '',
    pages: asArray(record['pages']).map(toKbPageSyncStatus),
    remote: asBoolean(record['remote']) ?? false,
  };
}

export function toKbSyncJobResult(value: unknown, selector: KbSyncSelector): KbSyncJobResult {
  const record = asRecord(value) ?? {};
  return {
    project: asString(record['project']) ?? selector.project ?? '',
    jobId: asString(record['jobId']) ?? '',
    pages: asStringArray(record['pages']) ?? [],
  };
}

function toCommentPushEntry(value: unknown): CommentPushEntry {
  const record = asRecord(value) ?? {};
  const entry: CommentPushEntry = { commentPath: asString(record['commentPath']) ?? '' };
  put(entry, 'youtrackCommentId', asString(record['youtrackCommentId']));
  put(entry, 'url', asString(record['url']));
  put(entry, 'reason', asString(record['reason']));
  put(entry, 'error', asString(record['error']));
  return entry;
}

export function toCommentPushResult(value: unknown, input: CommentPushInput): CommentPushResult {
  const record = asRecord(value) ?? {};
  const result: CommentPushResult = {
    project: asString(record['project']) ?? input.project ?? '',
    itemId: asString(record['itemId']) ?? input.itemId,
    pushed: asArray(record['pushed']).map(toCommentPushEntry),
    skipped: asArray(record['skipped']).map(toCommentPushEntry),
    failed: asArray(record['failed']).map(toCommentPushEntry),
  };
  put(result, 'jobId', asString(record['jobId']));
  return result;
}

/** The five triage states, in the order a filter chip row reads them (ADR-033). */
export const INBOX_STATUSES: InboxStatus[] = [
  'pending',
  'accepted',
  'rejected',
  'snoozed',
  'duplicate',
];

/**
 * `GET /api/v1/inbox` → one page of the queue.
 *
 * `counts` and `pending` are whole-queue numbers the server computed against
 * its own clock, so an expired snooze already counts as pending here. They are
 * read straight through rather than recomputed: a client cannot resolve an
 * expiry it has no clock agreement on.
 */
export function toInboxPage(value: unknown): InboxPage {
  const record = asRecord(value);
  const items = (record ? asArray(record['items']) : asArray(value)).map(toItem);
  const counts: Partial<Record<InboxStatus, number>> = {};
  const rawCounts = record ? (asRecord(record['counts']) ?? {}) : {};
  for (const status of INBOX_STATUSES) {
    const n = asNumber(rawCounts[status]);
    if (n !== undefined) counts[status] = n;
  }
  const page: InboxPage = {
    items,
    total: (record ? asNumber(record['total']) : undefined) ?? items.length,
    counts,
    pending: (record ? asNumber(record['pending']) : undefined) ?? counts.pending ?? 0,
  };
  const cursor = record ? asString(record['nextCursor']) : undefined;
  if (cursor !== undefined && cursor !== '') page.nextCursor = cursor;
  return page;
}

/** `POST /api/v1/items/{id}/triage` → the item and the queue behind it. */
export function toInboxTriageResult(value: unknown, fallback: InboxTriageInput): InboxTriageResult {
  const record = asRecord(value) ?? {};
  const action = asString(record['action']);
  return {
    item: toItem(record['item'] ?? record),
    action: (action as InboxTriageAction | undefined) ?? fallback.action,
    pending: asNumber(record['pending']) ?? 0,
  };
}

/** One `external:` entry as the API sends it. An entry without both halves is dropped. */
function toExternal(value: unknown): External | undefined {
  const record = asRecord(value);
  if (!record) return undefined;
  const system = asString(record['system']);
  const id = asString(record['id']);
  if (system === undefined || id === undefined) return undefined;
  const entry: External = { system, id };
  put(entry, 'url', asString(record['url']));
  put(entry, 'key', asString(record['key']));
  put(entry, 'syncedAt', asString(record['syncedAt']));
  return entry;
}

export function toExternalList(value: unknown): External[] | undefined {
  const entries = asArray(value)
    .map(toExternal)
    .filter((e): e is External => e !== undefined);
  return entries.length === 0 ? undefined : entries;
}

/** The `inbox:` block of an item in triage; absent for everything else. */
function toItemInbox(value: unknown): ItemInbox | undefined {
  const record = asRecord(value);
  if (!record) return undefined;
  const inbox: ItemInbox = {};
  const status = asString(record['status']);
  if (status !== undefined && INBOX_STATUSES.includes(status as InboxStatus)) {
    inbox.status = status as InboxStatus;
  }
  put(inbox, 'snoozedUntil', asString(record['snoozedUntil']));
  put(inbox, 'duplicateOf', asString(record['duplicateOf']));
  put(inbox, 'source', asString(record['source']));
  put(inbox, 'received', asString(record['received']));
  return inbox;
}

export function toItem(value: unknown): Item {
  const record = asRecord(value);
  const id = record ? asString(record['id']) : undefined;
  const type = record ? asItemType(record['type']) : undefined;
  if (!record || id === undefined || type === undefined) throw malformed('item');

  const item: Item = {
    id,
    type,
    title: asString(record['title']) ?? id,
    body: asString(record['body']) ?? '',
    path: asString(record['path']) ?? '',
    rev: asString(record['rev']) ?? '',
  };

  put(item, 'status', asString(record['status']));
  put(item, 'priority', asPriority(record['priority']));
  put(item, 'parent', asString(record['parent']));
  put(item, 'epic', asString(record['epic']));
  put(item, 'milestone', asString(record['milestone']));
  put(item, 'sprint', asString(record['sprint']));
  put(item, 'assignees', asStringArray(record['assignees']));
  put(item, 'author', asString(record['author']));
  put(item, 'owner', asString(record['owner']));
  put(item, 'labels', asStringArray(record['labels']));
  put(item, 'estimate', asNumber(record['estimate']));
  put(item, 'effort', asNumber(record['effort']));
  put(item, 'spent', asNumber(record['spent']));
  put(item, 'created', asString(record['created']));
  put(item, 'updated', asString(record['updated']));
  put(item, 'started', asString(record['started']));
  put(item, 'closed', asString(record['closed']));
  put(item, 'start', asString(record['start']));
  put(item, 'due', asString(record['due']));
  put(item, 'links', toLinks(record['links']));
  put(item, 'attachments', asStringArray(record['attachments']));
  put(item, 'custom', asRecord(record['custom']) ?? undefined);
  put(item, 'external', toExternalList(record['external']));
  put(item, 'inbox', toItemInbox(record['inbox']));
  put(item, 'deleted', asBoolean(record['deleted']));
  return item;
}

function toItemPage(value: unknown, totalHeader: string | null): ItemPage {
  const record = asRecord(value);
  const rawItems = record ? asArray(record['items']) : asArray(value);
  const items = rawItems.map(toItem);
  const headerTotal = totalHeader === null ? undefined : Number(totalHeader);
  const page: ItemPage = {
    items,
    total:
      (record ? asNumber(record['total']) : undefined) ??
      (headerTotal !== undefined && Number.isFinite(headerTotal) ? headerTotal : items.length),
  };
  const cursor = record ? asString(record['nextCursor']) : undefined;
  if (cursor !== undefined && cursor !== '') page.nextCursor = cursor;
  return page;
}

export function toComment(
  value: unknown,
  fallback: { item: string; author: string; body: string },
): Comment {
  const record = asRecord(value) ?? {};
  const comment: Comment = {
    item: asString(record['item']) ?? fallback.item,
    author: asString(record['author']) ?? fallback.author,
    body: asString(record['body']) ?? fallback.body,
    path: asString(record['path']) ?? '',
    rev: asString(record['rev']) ?? '',
  };
  put(comment, 'authorName', asString(record['authorName']));
  put(comment, 'authorEmail', asString(record['authorEmail']));
  put(comment, 'created', asString(record['created']));
  put(comment, 'updated', asString(record['updated']));
  put(comment, 'inReplyTo', asString(record['inReplyTo']));
  put(comment, 'kind', asString(record['kind']));
  put(comment, 'external', toExternalList(record['external']));
  return comment;
}

/** `GET /repos` entries carry a companion-side shape; this is the UI's. */
export function toRepoInfo(value: unknown): RepoInfo {
  const record = asRecord(value);
  const id = record ? (asString(record['key']) ?? asString(record['id'])) : undefined;
  if (!record || id === undefined) throw malformed('repository');

  const git = asRecord(record['git']);
  const repo: RepoInfo = {
    id,
    kind: asString(record['role']) === 'team' ? 'team' : 'project',
    name: asString(record['name']) ?? id,
    location: asString(record['path']) ?? id,
    docsFolder: asString(record['docs']) ?? '',
    state: asString(record['error']) === undefined ? 'ready' : 'error',
    projects: asStringArray(record['projects']) ?? (asString(record['key']) ? [id] : []),
  };
  const vcs = asRecord(record['vcs']);
  if (vcs) {
    const kind = asString(vcs['kind']);
    if (kind === 'git' || kind === 'jj' || kind === 'none') {
      const layout = asString(vcs['layout']);
      repo.vcs = {
        kind,
        ...(layout === 'colocated' || layout === 'internal' ? { layout } : {}),
        ...(typeof vcs['gitDir'] === 'boolean' ? { gitDir: vcs['gitDir'] } : {}),
      };
    }
  }
  put(repo, 'error', asString(record['error']));
  put(repo, 'lastIndexedAt', asString(record['lastIndexed']));
  if (git) {
    put(repo, 'branch', asString(git['branch']));
    put(repo, 'ahead', asNumber(git['ahead']));
    put(repo, 'behind', asNumber(git['behind']));
    put(repo, 'dirtyFiles', asNumber(git['dirty']));
  }
  return repo;
}

const STATUS_CATEGORIES: Record<string, string> = {
  backlog: 'todo',
  todo: 'todo',
  in_progress: 'in_progress',
  in_review: 'in_progress',
  done: 'done',
  cancelled: 'cancelled',
};

function humanize(id: string): string {
  return id
    .split(/[_-]/)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ');
}

function toStatuses(value: unknown): ProjectSummary['statuses'] {
  const entries = asArray(value);
  return entries
    .map((entry) => {
      if (typeof entry === 'string') {
        return { id: entry, name: humanize(entry), category: STATUS_CATEGORIES[entry] ?? 'todo' };
      }
      const record = asRecord(entry);
      const id = record ? asString(record['id']) : undefined;
      if (!record || id === undefined) return null;
      const status: ProjectSummary['statuses'][number] = {
        id,
        name: asString(record['name']) ?? humanize(id),
        category: asString(record['category']) ?? STATUS_CATEGORIES[id] ?? 'todo',
      };
      put(status, 'terminal', asBoolean(record['terminal']));
      put(status, 'wip', asNumber(record['wip']));
      return status;
    })
    .filter((entry): entry is ProjectSummary['statuses'][number] => entry !== null);
}

function toItemCounts(value: unknown): Record<ItemType, number> {
  const record = asRecord(value) ?? {};
  return {
    epic: asNumber(record['epics']) ?? 0,
    story: asNumber(record['stories']) ?? 0,
    task: asNumber(record['tasks']) ?? 0,
    milestone: asNumber(record['milestones']) ?? 0,
    spec: asNumber(record['specs']) ?? 0,
    comment: asNumber(record['comments']) ?? 0,
  };
}

export function toProjectSummary(value: unknown): ProjectSummary {
  const record = asRecord(value);
  const key = record ? asString(record['key']) : undefined;
  if (!record || key === undefined) throw malformed('project');

  const labels = asArray(record['labels'])
    .map((entry) => {
      if (typeof entry === 'string') return { name: entry };
      const label = asRecord(entry);
      const name = label ? asString(label['name']) : undefined;
      if (!label || name === undefined) return null;
      const mapped: ProjectSummary['labels'][number] = { name };
      put(mapped, 'color', asString(label['color']));
      put(mapped, 'description', asString(label['description']));
      return mapped;
    })
    .filter((entry): entry is ProjectSummary['labels'][number] => entry !== null);

  const priorities = (asStringArray(record['priorities']) ?? [])
    .map((entry) => asPriority(entry))
    .filter((entry): entry is Priority => entry !== undefined);

  const project: ProjectSummary = {
    key,
    name: asString(record['name']) ?? key,
    docsPath: asString(record['docsPath']) ?? asString(record['docs']) ?? '',
    statuses: toStatuses(record['statuses'] ?? record['workflow']),
    labels,
    priorities: priorities.length > 0 ? priorities : [...PRIORITIES],
    itemCounts: toItemCounts(record['counts'] ?? record['itemCounts']),
  };
  put(project, 'writable', asBoolean(record['writable']));
  put(project, 'configRev', asString(record['configRev']));
  return project;
}

function toKbNode(value: unknown): KbNode | null {
  const record = asRecord(value);
  const path = record ? asString(record['path']) : undefined;
  if (!record || path === undefined) return null;
  const kindText =
    asString(record['kind']) ?? (asArray(record['children']).length ? 'dir' : 'page');
  const node: KbNode = {
    path,
    name: asString(record['name']) ?? path.split('/').pop() ?? path,
    kind: kindText === 'dir' || kindText === 'asset' ? kindText : 'page',
  };
  put(node, 'title', asString(record['title']));
  const children = asArray(record['children'])
    .map(toKbNode)
    .filter((entry): entry is KbNode => entry !== null);
  if (children.length > 0) node.children = children;
  return node;
}

export function toKbTree(value: unknown): KbNode[] {
  const record = asRecord(value);
  const entries = record ? asArray(record['tree'] ?? record['nodes']) : asArray(value);
  return entries.map(toKbNode).filter((entry): entry is KbNode => entry !== null);
}

export function toKbPage(value: unknown, requestedPath: string): KbPage {
  const record = asRecord(value);
  if (!record) throw malformed('knowledge base page');
  const links = asRecord(record['links']);
  const wiki = links
    ? asArray(links['wiki'])
        .map((entry) => {
          const target = asRecord(entry);
          return target ? (asString(target['resolved']) ?? asString(target['target'])) : undefined;
        })
        .filter((entry): entry is string => entry !== undefined)
    : [];
  const path = asString(record['path']) ?? requestedPath;
  return {
    path,
    title: asString(record['title']) ?? path,
    frontMatter: asRecord(record['frontMatter']) ?? asRecord(record['frontmatter']) ?? {},
    body: asString(record['body']) ?? asString(record['raw']) ?? '',
    rev: asString(record['rev']) ?? '',
    outgoing: asStringArray(record['outgoing']) ?? wiki,
    backlinks: asStringArray(record['backlinks']) ?? [],
  };
}

/**
 * The kinds a hit can be. `file` is a document the semantic backend found
 * inside the indexed tree that the index owns neither as an item nor as a page
 * — a source file, or Markdown outside the knowledge base. `requirement` is
 * one requirement block of a spec (GIT-US-0118).
 */
function toSearchHitKind(value: string | undefined): SearchHit['kind'] {
  return value === 'item' || value === 'file' || value === 'requirement' ? value : 'page';
}

export function toSearchHits(value: unknown): SearchHit[] {
  const record = asRecord(value);
  // Three shapes are accepted, so a companion older than GIT-US-0086 keeps
  // working: the bare array it used to answer with, the `results` envelope,
  // and the `hits` envelope that now carries `degraded` alongside.
  const results = record ? asArray(record['hits'] ?? record['results']) : asArray(value);
  return results
    .map((entry) => {
      const hit = asRecord(entry);
      if (!hit) return null;
      const mapped: SearchHit = {
        kind: toSearchHitKind(asString(hit['kind'])),
        path: asString(hit['path']) ?? '',
        title: asString(hit['title']) ?? '',
        snippet: asString(hit['snippet']) ?? '',
        score: asNumber(hit['score']) ?? 0,
        // A hit that names no origin comes from the local index: only the
        // semantic half ever says `pando` (GIT-US-0086).
        source: asString(hit['source']) === 'pando' ? 'pando' : 'core',
      };
      put(mapped, 'id', asString(hit['id']));
      // Which of Pando's two indexations answered, so the row can say so
      // (GIT-US-0098). Anything else is left absent rather than guessed.
      const index = asString(hit['index']);
      if (index === 'kb' || index === 'code') mapped.index = index;
      // A workspace-wide search says which project — and which repository —
      // answered, so the UI can label every row (GIT-US-0016).
      put(mapped, 'project', asString(hit['project']));
      put(mapped, 'vaultId', asString(hit['vaultId']));
      // A requirement names its spec, its status and its block anchor.
      put(mapped, 'spec', asString(hit['spec']));
      put(mapped, 'status', asString(hit['status']));
      put(mapped, 'anchor', asString(hit['anchor']));
      return mapped;
    })
    .filter((entry): entry is SearchHit => entry !== null);
}

/** `GET /search` → the hits plus whether the semantic half answered. */
export function toSearchResult(value: unknown): SearchResult {
  const record = asRecord(value);
  const hits = toSearchHits(value);
  return asBoolean(record?.['degraded']) === true ? { hits, degraded: true } : { hits };
}

// ------------------------------------------------- semantic search settings

/** The phases the reindex walks; anything else reads as the first one. */
function toSearchPhase(value: string | undefined): SearchReindexPhase {
  switch (value) {
    case 'kb':
    case 'completed':
    case 'failed':
      return value;
    default:
      return 'code';
  }
}

/** One row of what Pando indexes, per mounted repository. */
function toSearchIndexed(value: unknown): SearchIndexedRepo[] {
  return asArray(value).map((entry) => {
    const record = asRecord(entry) ?? {};
    return {
      repo: asString(record['repo']) ?? '',
      root: asString(record['root']) ?? '',
      docs: asArray(record['docs']).flatMap((doc) => {
        const name = asString(doc);
        return name === undefined ? [] : [name];
      }),
      items: asNumber(record['items']) ?? 0,
      pages: asNumber(record['pages']) ?? 0,
      comments: asNumber(record['comments']) ?? 0,
      ...optional('code', toSearchCodeIndex(record['code'])),
    };
  });
}

/** One repository's code-project registration, absent until it has run. */
function toSearchCodeIndex(value: unknown): SearchCodeIndex | undefined {
  const record = asRecord(value);
  if (record === null) return undefined;
  const status = asString(record['status']);
  return {
    project: asString(record['project']) ?? '',
    status:
      status === 'off' || status === 'registered' || status === 'indexing' ? status : 'unavailable',
    ...optional('job', asString(record['job'])),
    ...optional('note', asString(record['note'])),
  };
}

function toSearchReindexRepos(value: unknown): SearchReindexRepo[] {
  return asArray(value).map((entry) => {
    const record = asRecord(entry) ?? {};
    return {
      repo: asString(record['repo']) ?? '',
      ...optional('codeJob', asString(record['codeJob'])),
      ...optional('codeError', asString(record['codeError'])),
    };
  });
}

function toSearchKbStats(value: unknown): SearchReindexKbStats | undefined {
  const record = asRecord(value);
  if (record === null) return undefined;
  return {
    scanned: asNumber(record['scanned']) ?? 0,
    added: asNumber(record['added']) ?? 0,
    updated: asNumber(record['updated']) ?? 0,
    unchanged: asNumber(record['unchanged']) ?? 0,
    deleted: asNumber(record['deleted']) ?? 0,
  };
}

/** `POST /search/reindex`, and the `reindex` field of the settings. */
export function toSearchReindexJob(value: unknown): SearchReindexJob {
  const record = asRecord(value) ?? {};
  return {
    jobId: asString(record['jobId']) ?? '',
    ...optional('scope', asString(record['scope'])),
    startedAt: asString(record['startedAt']) ?? '',
    ...optional('endedAt', asString(record['endedAt'])),
    phase: toSearchPhase(asString(record['phase'])),
    repos: toSearchReindexRepos(record['repos']),
    ...optional('kb', toSearchKbStats(record['kb'])),
    ...optional('kbNote', asString(record['kbNote'])),
    ...optional('error', asString(record['error'])),
  };
}

/**
 * `GET|PATCH /search/settings` → what the settings card renders.
 *
 * `reachable` keeps three states, so `null` is preserved rather than folded
 * into `false`: "nothing is configured" and "it is configured and did not
 * answer" are different things to tell the user.
 */
export function toSearchSettings(value: unknown): SearchSettings {
  const record = asRecord(value) ?? {};
  const reachable = asBoolean(record['reachable']);
  return {
    backend: toFullTextSearch(asString(record['backend'])),
    configured: asBoolean(record['configured']) ?? false,
    mcpUrl: asString(record['mcpUrl']) ?? '',
    restUrl: asString(record['restUrl']) ?? '',
    projectId: asString(record['projectId']) ?? '',
    allowRemote: asBoolean(record['allowRemote']) ?? false,
    reachable: reachable ?? null,
    reachableError: asString(record['reachableError']) ?? '',
    indexed: toSearchIndexed(record['indexed']),
    reindex:
      record['reindex'] === undefined || record['reindex'] === null
        ? null
        : toSearchReindexJob(record['reindex']),
    persisted: asBoolean(record['persisted']) ?? false,
  };
}

export function toIndexStats(value: unknown): IndexStats {
  const record = asRecord(value) ?? {};
  const counts = asRecord(record['counts']) ?? {};
  const items =
    asNumber(record['items']) ??
    (asNumber(counts['epics']) ?? 0) +
      (asNumber(counts['stories']) ?? 0) +
      (asNumber(counts['tasks']) ?? 0) +
      (asNumber(counts['milestones']) ?? 0);
  return {
    projects: asNumber(record['projects']) ?? 1,
    items,
    pages: asNumber(record['pages']) ?? asNumber(counts['pages']) ?? 0,
    comments: asNumber(record['comments']) ?? asNumber(counts['comments']) ?? 0,
    durationMs: asNumber(record['durationMs']) ?? 0,
    fingerprint: asString(record['fingerprint']) ?? '',
    diagnostics: [],
  };
}

/**
 * `GET|PATCH /git/settings` → the settings the UI shows. The companion always
 * supports committing, so `supported` is true whenever it answered at all.
 */
export function toGitSettings(value: unknown): GitSettings {
  const record = asRecord(value) ?? {};
  const backend = asString(record['backend']) ?? 'auto';
  return {
    commitOnSave: asBoolean(record['commitOnSave']) ?? false,
    commitDebounceMs: asNumber(record['commitDebounceMs']) ?? 2000,
    messageTemplate: asString(record['messageTemplate']) ?? '',
    backend: backend as GitSettings['backend'],
    resolvedBackend: asString(record['resolvedBackend']) ?? backend,
    ...optional('gitVersion', asString(record['gitVersion'])),
    ...optional('authorName', asString(record['authorName'])),
    ...optional('authorEmail', asString(record['authorEmail'])),
    signCommits: asBoolean(record['signCommits']) ?? false,
    pending: asNumber(record['pending']) ?? 0,
    ...optional('persisted', asBoolean(record['persisted'])),
    supported: true,
  };
}

/**
 * Drops a key whose value is undefined, which is what
 * `exactOptionalPropertyTypes` needs: "absent" and "present but undefined" are
 * different types, and the wire has only the first.
 */
function optional<K extends string, V>(key: K, value: V | undefined): Record<K, V> | object {
  return value === undefined ? {} : { [key]: value };
}

/**
 * Which search engine the companion reported. Anything unknown reads as the
 * always-available core index rather than promising a surface that is not
 * there.
 */
function toFullTextSearch(value: string | undefined): Capabilities['fullTextSearch'] {
  if (value === 'bleve' || value === 'pando') return value;
  return 'core';
}

/** `GET /capabilities` → the object the UI branches on. */
export function toCapabilities(value: unknown): Capabilities {
  const record = asRecord(value) ?? {};
  const features = asRecord(record['features']) ?? {};
  const limits = asRecord(record['limits']) ?? {};
  const git = asBoolean(features['git']) ?? companionCapabilities.git;
  return {
    write: asBoolean(features['write']) ?? companionCapabilities.write,
    git,
    ssh: asBoolean(features['ssh']) ?? git,
    watch: asBoolean(features['watcher']) ?? companionCapabilities.watch,
    fullTextSearch: toFullTextSearch(asString(features['search'])),
    mcp: asBoolean(features['mcpHttp']) ?? false,
    openInEditor: asBoolean(features['openInEditor']) ?? companionCapabilities.openInEditor,
    maxBatchWrite: asNumber(limits['maxBatchWrite']) ?? companionCapabilities.maxBatchWrite,
    // `youtrackSupported` says the build can speak to YouTrack at all and is
    // what gates the settings card; `youtrack` says a project is already
    // connected, which is a fact about the workspace, not a permission.
    youtrackSupported:
      asBoolean(features['youtrackSupported']) ?? companionCapabilities.youtrackSupported,
    youtrack: asBoolean(features['youtrack']) ?? companionCapabilities.youtrack,
    // The search settings routes are part of every companion; the flag exists
    // so a build without them can say so rather than answering 404 to a card.
    searchSettings: asBoolean(features['searchSettings']) ?? companionCapabilities.searchSettings,
    // A companion that does not report the flag has no agent routes: the chat
    // surface stays hidden rather than failing on the first call.
    agent: asBoolean(features['agent']) ?? companionCapabilities.agent,
  };
}

// ---------------------------------------------------------------- youtrack

/** `GET|PATCH /youtrack/settings` → the connection the settings card shows. */
export function toYouTrackSettings(value: unknown): YouTrackSettings {
  const record = asRecord(value) ?? {};
  const fieldMap: Record<string, YouTrackFieldMapping> = {};
  for (const [key, entry] of Object.entries(asRecord(record['fieldMap']) ?? {})) {
    const mapping = toYouTrackFieldMapping(entry);
    if (mapping !== undefined) fieldMap[key] = mapping;
  }
  return {
    projectKey: asString(record['projectKey']) ?? '',
    configured: asBoolean(record['configured']) ?? false,
    url: asString(record['url']) ?? '',
    project: asString(record['project']) ?? '',
    projectId: asString(record['projectId']) ?? '',
    fieldMap,
    pushComments: (asString(record['pushComments']) ?? '') as YouTrackPushComments,
    kbSync: (asString(record['kbSync']) ?? '') as YouTrackKbSync,
    kbSyncDirection: (asString(record['kbSyncDirection']) ?? '') as YouTrackKbSyncDirection,
    hasToken: asBoolean(record['hasToken']) ?? false,
    tokenSource: (asString(record['tokenSource']) ?? '') as YouTrackTokenSource,
    persisted: asBoolean(record['persisted']) ?? false,
    projectPath: asString(record['projectPath']) ?? '',
    repo: asString(record['repo']) ?? '',
  };
}

/**
 * One entry of the field map.
 *
 * The companion always writes the object form, `{field, values}`. The scalar
 * form is still read because a project.yaml may spell an entry as a bare field
 * name and an older companion answered it that way; both mean the same thing.
 */
function toYouTrackFieldMapping(value: unknown): YouTrackFieldMapping | undefined {
  const name = asString(value);
  if (name !== undefined) return { field: name };
  const record = asRecord(value);
  if (!record) return undefined;
  const values: Record<string, string> = {};
  for (const [from, to] of Object.entries(asRecord(record['values']) ?? {})) {
    const target = asString(to);
    if (target !== undefined && target !== '') values[from] = target;
  }
  return {
    field: asString(record['field']) ?? '',
    ...(Object.keys(values).length === 0 ? {} : { values }),
  };
}

/** `POST /youtrack/test` → who the credential authenticates as. */
export function toYouTrackTestResult(value: unknown): YouTrackTestResult {
  const record = asRecord(value) ?? {};
  return {
    ok: asBoolean(record['ok']) ?? false,
    baseUrl: asString(record['baseUrl']) ?? '',
    login: asString(record['login']) ?? '',
    fullName: asString(record['fullName']) ?? '',
    email: asString(record['email']) ?? '',
    project: asString(record['project']) ?? '',
  };
}

function toYouTrackProject(value: unknown): YouTrackProject {
  const record = asRecord(value) ?? {};
  return {
    id: asString(record['id']) ?? '',
    shortName: asString(record['shortName']) ?? '',
    name: asString(record['name']) ?? '',
    archived: asBoolean(record['archived']) ?? false,
  };
}

/**
 * One allowed value of a bundle-backed field.
 *
 * `isResolved` is copied only when the instance actually declared it: the
 * companion omits the key rather than sending `false` for a value it knows
 * nothing about, and reading an absent flag as `false` would turn "unknown"
 * into "this value does not close an issue", which is a claim nobody made.
 */
function toYouTrackFieldValue(value: unknown): YouTrackFieldValue {
  const record = asRecord(value) ?? {};
  const name = asString(record['name']) ?? '';
  const resolved = asBoolean(record['isResolved']);
  return {
    id: asString(record['id']) ?? '',
    name,
    label: asString(record['label']) ?? name,
    ...(asString(record['description']) === undefined
      ? {}
      : { description: asString(record['description']) ?? '' }),
    ordinal: asNumber(record['ordinal']) ?? 0,
    archived: asBoolean(record['archived']) ?? false,
    ...(resolved === undefined ? {} : { isResolved: resolved }),
  };
}

function toYouTrackField(value: unknown): YouTrackField {
  const record = asRecord(value) ?? {};
  return {
    id: asString(record['id']) ?? '',
    name: asString(record['name']) ?? '',
    type: asString(record['type']) ?? '',
    bundleId: asString(record['bundleId']) ?? '',
    bundleType: asString(record['bundleType']) ?? '',
    canBeEmpty: asBoolean(record['canBeEmpty']) ?? false,
    ...(asString(record['emptyFieldText']) === undefined
      ? {}
      : { emptyFieldText: asString(record['emptyFieldText']) ?? '' }),
    bundled: asBoolean(record['bundled']) ?? false,
    values: asArray(record['values']).map(toYouTrackFieldValue),
    warnings: asStringArray(record['warnings']) ?? [],
  };
}

/** `GET /youtrack/fields` → both halves of the mapping vocabulary. */
export function toYouTrackFieldList(value: unknown): YouTrackFieldList {
  const record = asRecord(value) ?? {};
  const fields = asArray(record['fields']).map(toYouTrackField);
  return {
    project: asString(record['project']) ?? '',
    fields,
    total: asNumber(record['total']) ?? fields.length,
    gintrackFields: asStringArray(record['gintrackFields']) ?? [],
    valueMappableFields: asStringArray(record['valueMappableFields']) ?? [],
  };
}

// ------------------------------------------------------- youtrack import

/** One row of `GET /youtrack/issues`. */
function toYouTrackIssue(value: unknown): YouTrackIssue {
  const record = asRecord(value) ?? {};
  const linked = asRecord(record['linked']);
  const itemId = linked ? asString(linked['itemId']) : undefined;
  return {
    id: asString(record['id']) ?? '',
    idReadable: asString(record['idReadable']) ?? '',
    summary: asString(record['summary']) ?? '',
    type: asString(record['type']) ?? '',
    state: asString(record['state']) ?? '',
    assignee: asString(record['assignee']) ?? '',
    updated: asString(record['updated']) ?? '',
    url: asString(record['url']) ?? '',
    linked: itemId === undefined || itemId === '' ? null : { itemId },
  };
}

/** `GET /youtrack/issues` → one page of the autosuggest. */
export function toYouTrackIssuePage(value: unknown): YouTrackIssuePage {
  const record = asRecord(value) ?? {};
  return {
    items: asArray(record['items']).map(toYouTrackIssue),
    nextCursor: asString(record['nextCursor']) ?? '',
  };
}

/** One mapper finding; every field is third-party text and stays text. */
function toImportWarning(value: unknown): YouTrackImportWarning {
  const record = asRecord(value) ?? {};
  return {
    field: asString(record['field']) ?? '',
    ...(asString(record['value']) === undefined ? {} : { value: asString(record['value']) ?? '' }),
    ...(asString(record['fallback']) === undefined
      ? {}
      : { fallback: asString(record['fallback']) ?? '' }),
    reason: asString(record['reason']) ?? '',
  };
}

function toImportWarnings(value: unknown): YouTrackImportWarning[] {
  return asArray(value).map(toImportWarning);
}

/** An action the API did not name is read as a create: it is the safe default. */
function toImportAction(value: unknown): 'create' | 'update' {
  return asString(value) === 'update' ? 'update' : 'create';
}

function toImportPlanItem(value: unknown): YouTrackImportPlanItem {
  const record = asRecord(value) ?? {};
  return {
    youtrackId: asString(record['youtrackId']) ?? '',
    title: asString(record['title']) ?? '',
    mappedType: asString(record['mappedType']) ?? '',
    action: toImportAction(record['action']),
    ...(asString(record['targetId']) === undefined
      ? {}
      : { targetId: asString(record['targetId']) ?? '' }),
    ...(asString(record['parent']) === undefined
      ? {}
      : { parent: asString(record['parent']) ?? '' }),
    ...(asString(record['milestone']) === undefined
      ? {}
      : { milestone: asString(record['milestone']) ?? '' }),
    depth: asNumber(record['depth']) ?? 0,
    comments: asNumber(record['comments']) ?? 0,
    warnings: toImportWarnings(record['warnings']),
  };
}

/** The preview operation → the plan, with nothing written. */
export function toYouTrackImportPreview(value: unknown): YouTrackImportPreviewResult {
  const record = asRecord(value) ?? {};
  return {
    project: asString(record['project']) ?? '',
    issues: asArray(record['issues']).map(toImportPlanItem),
    warnings: toImportWarnings(record['warnings']),
  };
}

/**
 * `POST /youtrack/import` → the job the import runs as.
 *
 * The route always queues and always answers `{jobId, projectKey, repo,
 * queued}`; there is no synchronous result to read, so there is nothing else
 * worth keeping here. What each issue produced is the job's business, and
 * `GET /sync/jobs/{id}` is where it is read back from.
 */
export function toYouTrackImportRun(value: unknown): YouTrackImportRun {
  const record = asRecord(value) ?? {};
  return { jobId: asString(record['jobId']) ?? asString(record['id']) ?? '' };
}

// --------------------------------------------------------- background jobs

const SYNC_JOB_STATES = new Set<string>(['queued', 'running', 'done', 'failed', 'cancelled']);

function toSyncJobState(value: unknown): SyncJobState {
  const state = asString(value) ?? '';
  return SYNC_JOB_STATES.has(state) ? (state as SyncJobState) : 'queued';
}

/** The last failure of a job. `message` is redacted third-party text. */
function toSyncJobError(value: unknown): SyncJobError | undefined {
  const record = asRecord(value);
  if (!record) return undefined;
  return {
    attempt: asNumber(record['attempt']) ?? 0,
    class: asString(record['class']) ?? '',
    message: asString(record['message']) ?? '',
    at: asString(record['at']) ?? '',
    ...(asNumber(record['retryAfter']) === undefined
      ? {}
      : { retryAfter: asNumber(record['retryAfter']) ?? 0 }),
  };
}

/** One job of `GET /sync/jobs`; the payload is never part of this shape. */
export function toSyncJob(value: unknown): SyncJob {
  const record = asRecord(value) ?? {};
  const lastError = toSyncJobError(record['lastError']);
  const nextAttempt = asString(record['nextAttempt']);
  return {
    id: asString(record['id']) ?? '',
    kind: asString(record['kind']) ?? '',
    key: asString(record['key']) ?? '',
    state: toSyncJobState(record['state']),
    attempts: asNumber(record['attempts']) ?? 0,
    createdAt: asString(record['createdAt']) ?? '',
    updatedAt: asString(record['updatedAt']) ?? '',
    ...(nextAttempt === undefined ? {} : { nextAttempt }),
    ...(lastError === undefined ? {} : { lastError }),
    ...(asBoolean(record['deadLetter']) === true ? { deadLetter: true } : {}),
  };
}

function toSyncJobCounts(value: unknown): SyncJobCounts {
  const record = asRecord(value) ?? {};
  return {
    queued: asNumber(record['queued']) ?? 0,
    running: asNumber(record['running']) ?? 0,
    done: asNumber(record['done']) ?? 0,
    failed: asNumber(record['failed']) ?? 0,
    cancelled: asNumber(record['cancelled']) ?? 0,
  };
}

/** `GET /sync/jobs` → one page plus the whole queue's summary. */
export function toSyncJobPage(value: unknown): SyncJobPage {
  const record = asRecord(value) ?? {};
  const jobs = asArray(record['jobs']).map(toSyncJob);
  return {
    jobs,
    nextCursor: asString(record['nextCursor']) ?? '',
    total: asNumber(record['total']) ?? jobs.length,
    counts: toSyncJobCounts(record['counts']),
    running: asNumber(record['running']) ?? 0,
    deadLetter: asNumber(record['deadLetter']) ?? 0,
    // An engine that is up but idle is still `true`; only an explicit `false`
    // means there is no pool.
    engine: asBoolean(record['engine']) ?? false,
  };
}

/** The engine half of the sync settings; absent on a runtime without one. */
function toSyncEngineSettings(value: unknown): SyncEngineSettings | undefined {
  const record = asRecord(value);
  if (!record) return undefined;
  return {
    workers: asNumber(record['workers']) ?? 0,
    batchSize: asNumber(record['batchSize']) ?? 0,
    rate: asNumber(record['rate']) ?? 0,
    maxAttempts: asNumber(record['maxAttempts']) ?? 0,
    retentionHours: asNumber(record['retentionHours']) ?? 0,
    drainSeconds: asNumber(record['drainSeconds']) ?? 0,
    running: asBoolean(record['running']) ?? false,
  };
}

/** `GET|PATCH /sync/settings` → the git half and the engine half together. */
export function toSyncSettings(value: unknown): SyncSettings {
  const record = asRecord(value) ?? {};
  const engine = toSyncEngineSettings(record['engine']);
  const persisted = asBoolean(record['persisted']);
  return {
    pullStrategy: (asString(record['pullStrategy']) as 'rebase' | 'merge' | undefined) ?? 'rebase',
    pushOnSync: record['pushOnSync'] !== false,
    maxPushRetries: asNumber(record['maxPushRetries']) ?? 3,
    supported: record['supported'] !== false,
    ...(asString(record['reason']) === undefined
      ? {}
      : { reason: asString(record['reason']) ?? '' }),
    ...(engine === undefined ? {} : { engine }),
    ...(persisted === undefined ? {} : { persisted }),
  };
}

/**
 * A `sync.job.*` frame. `phase` is the topic's last segment, so a topic this
 * build does not know is dropped rather than rendered as a job state.
 */
export function toSyncJobEvent(phase: SyncJobEventPhase, payload: unknown): SyncJobEvent {
  const record = asRecord(payload) ?? {};
  return {
    phase,
    id: asString(record['id']) ?? '',
    kind: asString(record['kind']) ?? '',
    key: asString(record['key']) ?? '',
    state: (asString(record['state']) ?? '') as SyncJobState | '',
    attempt: asNumber(record['attempt']) ?? 0,
    processed: asNumber(record['processed']) ?? 0,
    total: asNumber(record['total']) ?? 0,
    error: asString(record['error']) ?? '',
    errorClass: asString(record['errorClass']) ?? '',
  };
}

// ------------------------------------------------------------- query strings

type QueryValue = string | number | boolean | string[] | undefined;

/**
 * `?team=` for a team-scoped route, empty while no team is named. A workspace
 * holding one team therefore keeps sending the URLs it sent before the active
 * team existed (GIT-US-0036).
 */
function teamQuery(team?: string): string {
  return team === undefined || team === '' ? '' : `?team=${encodeURIComponent(team)}`;
}

/**
 * `?key=` for a YouTrack route. It is omitted when the caller names no project,
 * which the companion reads as "the only one you serve" — and refuses with
 * `invalid_request` when it serves several.
 */
function youtrackQuery(scope: YouTrackScope): string {
  return buildQuery({ key: scope.projectKey });
}

/**
 * The import options as `internal/vault`'s `YouTrackImportParams`. An option the
 * caller left out is left out of the body too, so the vault's own defaults
 * apply rather than a second set of defaults living here.
 */
function importBody(options: YouTrackImportOptions): Record<string, unknown> {
  return {
    ...(options.project === undefined || options.project === ''
      ? {}
      : { project: options.project }),
    ...(options.ids === undefined || options.ids.length === 0 ? {} : { ids: options.ids }),
    ...(options.query === undefined || options.query === '' ? {} : { query: options.query }),
    depth: options.depth,
    includeLinks: options.includeLinks,
    includeComments: options.includeComments,
    includeAttachments: options.includeAttachments,
  };
}

function buildQuery(params: Record<string, QueryValue>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined) continue;
    if (Array.isArray(value)) {
      // Repeatable params are OR within a field (docs/07 §5.3).
      for (const entry of value) search.append(key, entry);
      continue;
    }
    search.append(key, String(value));
  }
  const text = search.toString();
  return text === '' ? '' : `?${text}`;
}

function list(value: string | string[] | undefined): string[] | undefined {
  if (value === undefined) return undefined;
  return Array.isArray(value) ? value : [value];
}

/** The spec subtree of a project (GIT-US-0127). */
function specsBase(project: string): string {
  return `${API_PREFIX}/projects/${encodeURIComponent(project)}/specs`;
}

/** The route of one requirement: `ACME-SP-0003.R2` → `…/specs/ACME-SP-0003/requirements/R2`. */
function requirementPath(project: string, ref: string): string {
  const bare = ref.includes('/') ? ref.slice(ref.indexOf('/') + 1) : ref;
  const dot = bare.lastIndexOf('.');
  if (dot < 0) throw new ProviderError('validation_failed', `${ref} is not a requirement ref.`);
  const spec = bare.slice(0, dot);
  const req = bare.slice(dot + 1);
  return `${specsBase(project)}/${encodeURIComponent(spec)}/requirements/${encodeURIComponent(req)}`;
}

/** `ImpactQuery` → the query string of `GET …/specs/impact`. */
function impactQuery(query: ImpactQuery, extra: Record<string, QueryValue> = {}): string {
  return buildQuery({
    base: query.base,
    head: query.head,
    story: query.story,
    title: query.title,
    tiers: query.tiers?.map(String),
    depth: query.depth,
    limit: query.limit,
    ...extra,
  });
}

/** The answer of `GET …/requirements`, with the total defaulted to the row count. */
export function toRequirementList(value: unknown): RequirementList {
  const record = asRecord(value);
  const requirements = asArray(record?.['requirements']) as RequirementList['requirements'];
  return { requirements, total: asNumber(record?.['total']) ?? requirements.length };
}

/** `ItemFilter` → the documented `GET /items` query parameters. */
export function itemFilterQuery(filter: ItemFilter): string {
  const sort =
    filter.sort === undefined ? undefined : `${filter.order === 'desc' ? '-' : ''}${filter.sort}`;
  return buildQuery({
    project: filter.project,
    type: list(filter.type),
    status: list(filter.status),
    // `category` has no REST counterpart; the UI expands it into statuses.
    priority: list(filter.priority),
    assignee: filter.assignee,
    label: list(filter.label),
    parent: filter.parent,
    milestone: filter.milestone,
    updatedSince: filter.updatedSince,
    q: filter.text,
    includeDeleted: filter.includeDeleted,
    sort,
    limit: filter.limit,
    cursor: filter.cursor,
    fields: filter.fields?.join(','),
  });
}

function kbBase(scope: KbScope): string {
  return scope.kind === 'project'
    ? `${API_PREFIX}/projects/${encodeURIComponent(scope.projectKey)}/kb`
    : `${API_PREFIX}/teams/${encodeURIComponent(scope.teamId)}/kb`;
}

// ------------------------------------------------------------------ provider

type RequestOptions = {
  method?: string;
  body?: unknown;
  /** Sent as `If-Match`; `undefined` on reads. */
  rev?: string;
  accept?: string;
  /**
   * Aborts the request. Only the agent routes pass one: an aborted fetch is
   * re-thrown untouched instead of being dressed up as "the companion is
   * unreachable", which it is not.
   */
  signal?: AbortSignal;
};

/** Companion routes that proxy one repository's Pando AG-UI adapter. */
export const AGENT_PREFIX = `${API_PREFIX}/agent`;

/**
 * Turns an SSE response into AG-UI events with the SDK's own parser.
 *
 * Nothing here re-implements the protocol: `parseSSE` reassembles frames
 * across chunk boundaries and decodes each `data:` line, and `PandoThread`
 * (in `features/agent`) reduces what comes out. This function only guards the
 * two ways a proxy can hand back something that is not a stream at all.
 */
async function* agentEvents(response: Response, path: string): AsyncGenerator<AguiEvent> {
  const contentType = response.headers.get('content-type') ?? '';
  if (!contentType.includes('text/event-stream')) {
    throw new ProviderError(
      'internal',
      `The agent answered with "${contentType || '(no content type)'}" instead of an SSE stream; something between the app and the companion intercepted the request.`,
      path,
    );
  }
  if (!response.body) {
    throw new ProviderError('internal', 'The agent stream carried no body.', path);
  }
  yield* parseSSE(response.body);
}

/** The `RequestOptions` half of an `AgentRequestOptions`. */
function agentRequest(options: AgentRequestOptions): RequestOptions {
  return options.signal === undefined ? {} : { signal: options.signal };
}

/** One `GET /agent/threads` row; anything without an id is dropped. */
function toAgentThreadSummary(value: unknown): AgentThreadSummary | null {
  const record = asRecord(value);
  const id = record ? asString(record['id']) : undefined;
  if (id === undefined) return null;
  const title = asString(record?.['title']);
  const createdAt = asString(record?.['createdAt']);
  const updatedAt = asString(record?.['updatedAt']);
  const messageCount = asNumber(record?.['messageCount']);
  const running = asBoolean(record?.['running']);
  return {
    id,
    ...(title === undefined ? {} : { title }),
    ...(createdAt === undefined ? {} : { createdAt }),
    ...(updatedAt === undefined ? {} : { updatedAt }),
    ...(messageCount === undefined ? {} : { messageCount }),
    ...(running === undefined ? {} : { running }),
  };
}

export class CompanionProvider implements DataProvider {
  readonly kind = 'companion' as const;

  readonly #baseUrl: string;
  readonly #fetch: typeof fetch;
  readonly #webSocketFactory: WebSocketFactory | null;
  readonly #random: () => number;
  readonly #handlers = new Set<(event: ChangeEvent) => void>();
  readonly #connectionListeners = new Set<(state: ConnectionState) => void>();

  #capabilities: Capabilities;
  #version: string | null;
  #socket: WebSocketLike | null = null;
  #reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  #pollTimer: ReturnType<typeof setInterval> | null = null;
  #attempts = 0;
  #lastSeq: number | null = null;
  #connection: ConnectionState = 'idle';
  #disposed = false;

  /** Resolves once `GET /capabilities` answered (or failed harmlessly). */
  readonly ready: Promise<void>;

  constructor(options: CompanionProviderOptions = {}) {
    this.#baseUrl = options.baseUrl ?? resolveCompanionBaseUrl();
    // Bound lazily: a missing global `fetch` must fail on use, not on construction.
    this.#fetch = options.fetchImpl ?? ((input, init) => globalThis.fetch(input, init));
    this.#webSocketFactory =
      options.webSocketFactory === undefined ? defaultWebSocketFactory() : options.webSocketFactory;
    this.#random = options.random ?? Math.random;
    this.#capabilities = options.capabilities ?? companionCapabilities;
    this.#version = options.version ?? null;
    this.ready = options.capabilities ? Promise.resolve() : this.refreshCapabilities();
  }

  get capabilities(): Capabilities {
    return this.#capabilities;
  }

  /** Base URL of the companion, shown in Settings. */
  get baseUrl(): string {
    return this.#baseUrl === '' ? (globalThis.location?.origin ?? '') : this.#baseUrl;
  }

  get version(): string | null {
    return this.#version;
  }

  get connectionState(): ConnectionState {
    return this.#connection;
  }

  onConnectionStateChange(listener: (state: ConnectionState) => void): Unsubscribe {
    this.#connectionListeners.add(listener);
    listener(this.#connection);
    return () => {
      this.#connectionListeners.delete(listener);
    };
  }

  /**
   * Reads `GET /capabilities`. Failure is not fatal: the provider keeps the
   * optimistic companion defaults and the next call surfaces the real problem.
   */
  async refreshCapabilities(): Promise<void> {
    try {
      const body = await this.#json(`${API_PREFIX}/capabilities`);
      this.#capabilities = toCapabilities(body);
      const record = asRecord(body);
      const version = record ? asString(record['version']) : undefined;
      if (version !== undefined) this.#version = version;
    } catch {
      // `401` already cleared the token; the UI asks for a new one.
    }
  }

  // ---------------------------------------------------------------- workspace

  async listRepos(): Promise<RepoInfo[]> {
    const body = await this.#json(`${API_PREFIX}/repos`);
    const record = asRecord(body);
    const entries = record ? asArray(record['repos'] ?? record['items']) : asArray(body);
    return entries.map(toRepoInfo);
  }

  async listProjects(): Promise<ProjectSummary[]> {
    const body = await this.#json(`${API_PREFIX}/projects`);
    const record = asRecord(body);
    const entries = record ? asArray(record['projects'] ?? record['items']) : asArray(body);
    return entries.map(toProjectSummary);
  }

  /**
   * The team repository of the workspace. The companion serves one at most, so
   * an empty list means "no team repository is registered", which is a normal
   * state rather than an error.
   */
  async getTeam(team?: string): Promise<TeamSummary | null> {
    if (team !== undefined && team !== '') {
      try {
        return (await this.#json(`${API_PREFIX}/teams/${encodeURIComponent(team)}`)) as TeamSummary;
      } catch (error) {
        if (error instanceof ProviderError && error.code === 'not_found') return null;
        throw error;
      }
    }
    const teams = await this.listTeams();
    return teams[0] ?? null;
  }

  /**
   * Every registered repository holding a `team.yaml`, in mount order. An
   * empty list means "no team repository is registered", which is a normal
   * state rather than an error.
   */
  async listTeams(): Promise<TeamSummary[]> {
    const body = await this.#json(`${API_PREFIX}/teams`);
    const record = asRecord(body);
    const entries = record ? asArray(record['teams'] ?? record['items']) : asArray(body);
    return entries as TeamSummary[];
  }

  async resolveRef(ref: string): Promise<RefResolution> {
    const body = await this.#json(`${API_PREFIX}/refs${buildQuery({ ref })}`);
    return body as RefResolution;
  }

  async mountRepo(input: MountInput): Promise<RepoInfo> {
    const body = await this.#json(`${API_PREFIX}/repos`, {
      method: 'POST',
      body: {
        path: input.location,
        role: input.kind,
        ...(input.docsFolder === undefined ? {} : { docs: input.docsFolder }),
      },
    });
    return toRepoInfo(body);
  }

  /**
   * Scaffolds a backlog in a registered repository that has none. The companion
   * writes the files itself, through the same core code browser-only mode runs,
   * so the two modes produce byte-identical projects.
   */
  async createProject(input: CreateProjectInput): Promise<ProjectSummary> {
    const repoId = input.repoId ?? (await this.#soleRepoId());
    const body = await this.#json(`${API_PREFIX}/repos/${encodeURIComponent(repoId)}/projects`, {
      method: 'POST',
      body: {
        docsFolder: input.docsFolder,
        key: input.key,
        ...(input.name === undefined ? {} : { name: input.name }),
        ...(input.description === undefined ? {} : { description: input.description }),
        ...(input.timezone === undefined ? {} : { timezone: input.timezone }),
      },
    });
    const record = asRecord(body);
    return toProjectSummary(record?.['project'] ?? body);
  }

  /**
   * Adds the triage status to a project's `project.yaml`. `If-Match` carries
   * the `configRev` the listing reported; without one the write is
   * unconditional, which is safe because it only ever inserts one status.
   */
  async enableInbox(input: EnableInboxInput): Promise<ProjectSummary> {
    const body = await this.#json(
      `${API_PREFIX}/projects/${encodeURIComponent(input.project)}/inbox`,
      {
        method: 'POST',
        ...(input.rev === undefined ? {} : { rev: input.rev }),
      },
    );
    const record = asRecord(body);
    return toProjectSummary(record?.['project'] ?? body);
  }

  /**
   * Turns a registered repository into a team repository. The companion writes
   * the files itself, through the same core code browser-only mode runs, so the
   * two modes produce byte-identical `team.yaml` files.
   */
  async createTeam(input: CreateTeamInput): Promise<TeamSummary> {
    const repoId = input.repoId ?? (await this.#soleRepoId());
    const body = await this.#json(`${API_PREFIX}/repos/${encodeURIComponent(repoId)}/team`, {
      method: 'POST',
      body: {
        key: input.key,
        ...(input.root === undefined ? {} : { root: input.root }),
        ...(input.name === undefined ? {} : { name: input.name }),
        ...(input.description === undefined ? {} : { description: input.description }),
        ...(input.timezone === undefined ? {} : { timezone: input.timezone }),
        ...(input.knowledgePath === undefined ? {} : { knowledgePath: input.knowledgePath }),
        ...(input.members === undefined ? {} : { members: input.members }),
      },
    });
    const record = asRecord(body);
    return (record?.['team'] ?? body) as TeamSummary;
  }

  /**
   * Declares a project repository in a team's `team.yaml`. The team travels in
   * the path here rather than as `?team=`, because the project list belongs to
   * one team repository and to nothing else.
   */
  async addTeamProject(project: TeamProjectDraft, team?: string): Promise<TeamProjectResult> {
    const key = await this.#teamKey(team);
    const body = await this.#json(`${API_PREFIX}/teams/${encodeURIComponent(key)}/projects`, {
      method: 'POST',
      body: project,
    });
    return body as TeamProjectResult;
  }

  /** Disconnects a project from a team's `team.yaml`. */
  async removeTeamProject(
    key: string,
    opts?: { force?: boolean },
    team?: string,
  ): Promise<TeamProjectResult> {
    const teamKey = await this.#teamKey(team);
    const query = opts?.force ? '?force=true' : '';
    const body = await this.#json(
      `${API_PREFIX}/teams/${encodeURIComponent(teamKey)}/projects/${encodeURIComponent(key)}${query}`,
      { method: 'DELETE' },
    );
    return body as TeamProjectResult;
  }

  /**
   * The team a call that names none acts on. The routes address a team by path
   * segment, so unlike `?team=` there is no "omit it and the only team answers"
   * form: the sole open team is resolved here instead.
   */
  async #teamKey(team?: string): Promise<string> {
    if (team !== undefined && team !== '') return team;
    const teams = await this.listTeams();
    if (teams.length === 1 && teams[0]) return teams[0].key;
    throw new ProviderError(
      'not_found',
      teams.length === 0
        ? 'No team repository is registered. Create or mount one first.'
        : 'This workspace holds several team repositories: name the one to act on.',
    );
  }

  /** The registered repository a call that names none is written into. */
  async #soleRepoId(): Promise<string> {
    const repos = await this.listRepos();
    if (repos.length === 1 && repos[0]) return repos[0].id;
    throw new ProviderError(
      'not_found',
      repos.length === 0
        ? 'No repository is registered. Register one with `gintrack add <path>`.'
        : 'This workspace holds several repositories: name the one to create the project in.',
    );
  }

  async unmountRepo(repoId: string): Promise<void> {
    await this.#json(`${API_PREFIX}/repos/${encodeURIComponent(repoId)}`, { method: 'DELETE' });
  }

  async reindex(repoId: string, opts?: { full?: boolean }): Promise<IndexStats> {
    const body = await this.#json(`${API_PREFIX}/repos/${encodeURIComponent(repoId)}/reindex`, {
      method: 'POST',
      body: { full: opts?.full ?? false },
    });
    return toIndexStats(body);
  }

  // --------------------------------------------------------------------- read

  async listItems(query: ItemFilter): Promise<ItemPage> {
    const response = await this.#send(`${API_PREFIX}/items${itemFilterQuery(query)}`);
    const body = await readJson(response);
    return toItemPage(body, response.headers?.get('X-Total-Count') ?? null);
  }

  async getItem(id: string): Promise<Item> {
    return toItem(await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}`));
  }

  async getChildren(id: string): Promise<Item[]> {
    const page = await this.listItems({ parent: id, limit: 500 });
    return page.items;
  }

  async listComments(id: string): Promise<Comment[]> {
    const body = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}/comments`);
    const record = asRecord(body);
    const entries = record ? asArray(record['comments'] ?? record['items']) : asArray(body);
    return entries.map((entry) => toComment(entry, { item: id, author: '', body: '' }));
  }

  async listKbTree(scope: KbScope): Promise<KbNode[]> {
    return toKbTree(await this.#json(`${kbBase(scope)}/tree`));
  }

  async getPage(scope: KbScope, path: string): Promise<KbPage> {
    const query = buildQuery({ path, format: 'raw' });
    return toKbPage(await this.#json(`${kbBase(scope)}/page${query}`), path);
  }

  /**
   * Assets are bytes, not JSON. docs/07 §5.5 documents no asset route, so this
   * reads the raw representation of the path under the same KB base.
   */
  async readAsset(scope: KbScope, path: string): Promise<Blob> {
    const response = await this.#send(`${kbBase(scope)}/asset${buildQuery({ path })}`, {
      accept: 'application/octet-stream',
    });
    return response.blob();
  }

  async search(query: SearchQuery): Promise<SearchResult> {
    const search = buildQuery({
      q: query.text,
      scope: 'items,kb',
      // Repeated `project=` params; none at all searches every project.
      project: searchProjectKeys(query),
      limit: query.limit,
    });
    return toSearchResult(await this.#json(`${API_PREFIX}/search${search}`));
  }

  /**
   * Server-side validation has no documented route; when the companion answers
   * `404` the UI falls back to its own client-side diagnostics.
   */
  async validateItem(input: { id?: string; text?: string; path?: string }): Promise<Diagnostic[]> {
    try {
      const body = await this.#json(`${API_PREFIX}/items/validate`, {
        method: 'POST',
        body: input,
      });
      const record = asRecord(body);
      const entries = record ? asArray(record['diagnostics']) : asArray(body);
      return entries
        .map((entry) => {
          const diagnostic = asRecord(entry);
          if (!diagnostic) return null;
          const mapped: Diagnostic = {
            code: asString(diagnostic['code']) ?? 'unknown',
            severity:
              asString(diagnostic['severity']) === 'warning'
                ? 'warning'
                : asString(diagnostic['severity']) === 'info'
                  ? 'info'
                  : 'error',
            message: asString(diagnostic['message']) ?? '',
          };
          put(mapped, 'path', asString(diagnostic['path']));
          put(mapped, 'field', asString(diagnostic['field']));
          return mapped;
        })
        .filter((entry): entry is Diagnostic => entry !== null);
    } catch (error) {
      if (error instanceof ProviderError && error.code === 'not_found') return [];
      throw error;
    }
  }

  // -------------------------------------------------------------------- write

  async createItem(input: ItemDraft): Promise<Item> {
    const body = await this.#json(`${API_PREFIX}/items`, { method: 'POST', body: input });
    return this.#hydrate(body);
  }

  async updateItem(id: string, patch: ItemPatch, rev: string): Promise<Item> {
    const body = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      rev,
      body: toRestPatch(patch),
    });
    return this.#hydrate(body, id);
  }

  /**
   * Every open team repository is searched, not just the active one: a card in
   * a team the user is not currently looking at breaks just as badly.
   */
  async getItemReferences(id: string): Promise<ItemReferencesResult> {
    const body = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}/references`);
    const record = asRecord(body);
    return {
      id,
      references: asArray(record?.['references']) as ItemReference[],
      children: asArray(record?.['children']) as ItemReference[],
    };
  }

  async setTaskItem(id: string, line: number, checked: boolean, rev: string): Promise<Item> {
    const body = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}/tasks`, {
      method: 'POST',
      rev,
      body: { line, checked },
    });
    return this.#hydrate(body, id);
  }

  async moveItem(id: string, status: ItemStatus, rev: string): Promise<Item> {
    const body = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}/move`, {
      method: 'POST',
      rev,
      body: { status },
    });
    return this.#hydrate(body, id);
  }

  // -------------------------------------------------------------------- boards

  async listBoards(team?: string): Promise<BoardSummary[]> {
    const body = await this.#json(`${API_PREFIX}/boards${teamQuery(team)}`);
    const record = asRecord(body);
    const entries = record ? asArray(record['boards'] ?? record['items']) : asArray(body);
    return entries as BoardSummary[];
  }

  async getBoard(slug: string, team?: string): Promise<BoardView> {
    return (await this.#json(
      `${API_PREFIX}/boards/${encodeURIComponent(slug)}${teamQuery(team)}`,
    )) as BoardView;
  }

  /**
   * `If-Match` carries the board revision; `itemRev` carries the item's,
   * because the two live in different repositories and therefore hold two
   * independent optimistic locks.
   */
  async moveCard(move: CardMove): Promise<BoardMoveResult> {
    const body = await this.#json(
      `${API_PREFIX}/boards/${encodeURIComponent(move.board)}/cards/move${teamQuery(move.team)}`,
      {
        method: 'POST',
        rev: move.rev ?? '*',
        body: {
          ref: move.ref,
          toColumn: move.toColumn,
          position: move.position,
          ...(move.status === undefined ? {} : { status: move.status }),
          ...(move.itemRev === undefined ? {} : { itemRev: move.itemRev }),
          ...(move.force === undefined ? {} : { force: move.force }),
        },
      },
    );
    return body as BoardMoveResult;
  }

  /** `If-Match` carries the board revision; `*` overwrites unconditionally. */
  async updateBoard(
    slug: string,
    patch: BoardPatch,
    rev?: string,
    team?: string,
  ): Promise<BoardView> {
    const body = await this.#json(
      `${API_PREFIX}/boards/${encodeURIComponent(slug)}${teamQuery(team)}`,
      {
        method: 'PATCH',
        rev: rev ?? '*',
        body: patch,
      },
    );
    const record = asRecord(body);
    return (record ? record['board'] : body) as BoardView;
  }

  /**
   * Creates a board in the team repository. There is nothing yet to conflict
   * with, so the call carries no `If-Match`; a slug already taken comes back
   * as `duplicate_id`.
   */
  async createBoard(draft: BoardDraft, team?: string): Promise<BoardView> {
    const body = await this.#json(`${API_PREFIX}/boards${teamQuery(team)}`, {
      method: 'POST',
      body: draft,
    });
    const record = asRecord(body);
    return (record ? record['board'] : body) as BoardView;
  }

  /** `If-Match` carries the board revision; `*` deletes unconditionally. */
  async deleteBoard(slug: string, rev?: string, team?: string): Promise<void> {
    await this.#json(`${API_PREFIX}/boards/${encodeURIComponent(slug)}${teamQuery(team)}`, {
      method: 'DELETE',
      rev: rev ?? '*',
    });
  }

  // ------------------------------------------------------------------- retros

  async listRetros(filter: RetroFilter = {}, team?: string): Promise<RetroListing> {
    const query = new URLSearchParams();
    if (filter.sprint) query.set('sprint', filter.sprint);
    if (filter.board) query.set('board', filter.board);
    if (filter.state) query.set('state', filter.state);
    if (team) query.set('team', team);
    const suffix = query.size > 0 ? `?${query.toString()}` : '';
    return (await this.#json(`${API_PREFIX}/retros${suffix}`)) as RetroListing;
  }

  async getRetro(id: string, team?: string): Promise<RetroView> {
    return (await this.#json(
      `${API_PREFIX}/retros/${encodeURIComponent(id)}${teamQuery(team)}`,
    )) as RetroView;
  }

  async createRetro(input: RetroDraft, team?: string): Promise<RetroResult> {
    return (await this.#json(`${API_PREFIX}/retros${teamQuery(team)}`, {
      method: 'POST',
      body: input,
    })) as RetroResult;
  }

  async updateRetro(
    id: string,
    patch: RetroPatch,
    rev?: string,
    team?: string,
  ): Promise<RetroResult> {
    return (await this.#json(`${API_PREFIX}/retros/${encodeURIComponent(id)}${teamQuery(team)}`, {
      method: 'PATCH',
      rev: rev ?? '*',
      body: patch,
    })) as RetroResult;
  }

  async promoteRetroAction(input: RetroPromotion, team?: string): Promise<RetroResult> {
    const path =
      `${API_PREFIX}/retros/${encodeURIComponent(input.retro)}/actions/promote` + teamQuery(team);
    return (await this.#json(path, {
      method: 'POST',
      rev: input.rev ?? '*',
      body: {
        action: input.action,
        project: input.project,
        ...(input.labels === undefined ? {} : { labels: input.labels }),
      },
    })) as RetroResult;
  }

  // ------------------------------------------------------------------- sprints

  async listSprints(filter: SprintFilter = {}, team?: string): Promise<SprintSummary[]> {
    const query = new URLSearchParams();
    if (filter.board) query.set('board', filter.board);
    if (filter.state) query.set('state', filter.state);
    if (team) query.set('team', team);
    const suffix = query.size > 0 ? `?${query.toString()}` : '';
    const body = await this.#json(`${API_PREFIX}/sprints${suffix}`);
    const record = asRecord(body);
    return asArray(record ? record['sprints'] : body) as SprintSummary[];
  }

  async getSprint(id: string, team?: string): Promise<SprintView> {
    return (await this.#json(
      `${API_PREFIX}/sprints/${encodeURIComponent(id)}${teamQuery(team)}`,
    )) as SprintView;
  }

  async getSprintMetrics(id: string, team?: string): Promise<SprintMetricsView> {
    return (await this.#json(
      `${API_PREFIX}/sprints/${encodeURIComponent(id)}/burndown${teamQuery(team)}`,
    )) as SprintMetricsView;
  }

  async createSprint(input: SprintDraft, team?: string): Promise<SprintResult> {
    return (await this.#json(`${API_PREFIX}/sprints${teamQuery(team)}`, {
      method: 'POST',
      body: input,
    })) as SprintResult;
  }

  async updateSprint(
    id: string,
    patch: SprintPatch,
    rev?: string,
    team?: string,
  ): Promise<SprintResult> {
    return (await this.#json(`${API_PREFIX}/sprints/${encodeURIComponent(id)}${teamQuery(team)}`, {
      method: 'PATCH',
      rev: rev ?? '*',
      body: patch,
    })) as SprintResult;
  }

  async startSprint(
    id: string,
    rev?: string,
    force?: boolean,
    team?: string,
  ): Promise<SprintResult> {
    return (await this.#json(
      `${API_PREFIX}/sprints/${encodeURIComponent(id)}/start${teamQuery(team)}`,
      {
        method: 'POST',
        rev: rev ?? '*',
        body: { ...(force === undefined ? {} : { force }) },
      },
    )) as SprintResult;
  }

  /**
   * `POST /api/v1/sprints/{id}/close`.
   *
   * A `dryRun` computes the whole report and writes nothing, not even a write
   * set: it is what the confirmation dialog renders before anything moves.
   */
  async closeSprint(
    id: string,
    input: SprintCloseInput = {},
    team?: string,
  ): Promise<SprintResult> {
    return (await this.#json(
      `${API_PREFIX}/sprints/${encodeURIComponent(id)}/close${teamQuery(team)}`,
      {
        method: 'POST',
        rev: input.rev ?? '*',
        body: {
          carry: input.carry ?? [],
          ...(input.transfer === undefined ? {} : { transfer: input.transfer }),
          ...(input.dryRun === undefined ? {} : { dryRun: input.dryRun }),
        },
      },
    )) as SprintResult;
  }

  /** `POST /api/v1/sprints/{id}/transfer`: move scope without closing anything. */
  async transferSprintItems(
    id: string,
    input: SprintTransferInput = {},
    team?: string,
  ): Promise<SprintResult> {
    return (await this.#json(
      `${API_PREFIX}/sprints/${encodeURIComponent(id)}/transfer${teamQuery(team)}`,
      {
        method: 'POST',
        rev: input.rev ?? '*',
        body: {
          ...(input.mode === undefined ? {} : { mode: input.mode }),
          ...(input.target === undefined ? {} : { target: input.target }),
          ...(input.carry === undefined ? {} : { carry: input.carry }),
          ...(input.dryRun === undefined ? {} : { dryRun: input.dryRun }),
        },
      },
    )) as SprintResult;
  }

  // ----------------------------------------------------------------- snapshots

  async listSnapshots(): Promise<SnapshotResult[]> {
    const body = await this.#json(`${API_PREFIX}/snapshots`);
    const record = asRecord(body);
    return asArray(record ? record['snapshots'] : body) as SnapshotResult[];
  }

  /**
   * Regenerating a snapshot writes into the team repository, so it is a POST
   * with no optimistic lock: the file is derived data the core rewrites whole,
   * and an unchanged one is not written at all.
   */
  async refreshSnapshots(input: SnapshotRefresh = {}): Promise<SnapshotResult[]> {
    const body = await this.#json(`${API_PREFIX}/snapshots`, {
      method: 'POST',
      body: {
        ...(input.projects === undefined ? {} : { projects: input.projects }),
        ...(input.generatedBy === undefined ? {} : { generatedBy: input.generatedBy }),
        ...(input.includeClosed === undefined ? {} : { includeClosed: input.includeClosed }),
        ...(input.dryRun === undefined ? {} : { dryRun: input.dryRun }),
      },
    });
    const record = asRecord(body);
    return asArray(record ? record['snapshots'] : body) as SnapshotResult[];
  }

  /** Sequential, so one rejected rev does not abort the rest of the batch. */
  async updateMany(ops: UpdateOp[]): Promise<BatchResult> {
    const result: BatchResult = { applied: 0, failed: [] };
    for (const op of ops) {
      try {
        await this.updateItem(op.id, op.patch, op.rev);
        result.applied += 1;
      } catch (error) {
        const provider =
          error instanceof ProviderError
            ? error
            : new ProviderError('internal', error instanceof Error ? error.message : String(error));
        result.failed.push({ id: op.id, code: provider.code, message: provider.message });
      }
    }
    return result;
  }

  async deleteItem(id: string, rev: string, opts: { hard?: boolean } = {}): Promise<void> {
    const query = opts.hard ? '?hard=true' : '';
    await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}${query}`, {
      method: 'DELETE',
      rev,
    });
  }

  async addComment(id: string, body: string, author?: string): Promise<Comment> {
    // No author: the companion attributes the comment to the git identity of
    // the item's repository.
    const answer = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}/comments`, {
      method: 'POST',
      body: author ? { body, author } : { body },
    });
    return toComment(answer, { item: id, author: author ?? '', body });
  }

  async setCommentTask(
    id: string,
    path: string,
    line: number,
    checked: boolean,
    rev: string,
  ): Promise<Comment> {
    const answer = await this.#json(
      `${API_PREFIX}/items/${encodeURIComponent(id)}/comments/tasks`,
      { method: 'POST', rev, body: { path, line, checked } },
    );
    return toComment(answer, { item: id, author: '', body: '' });
  }

  // --------------------------------------------------------------------- inbox

  /**
   * `GET /api/v1/inbox?project=&status=&…`.
   *
   * The filter's `status` is the *triage* state, never a workflow status:
   * every item this route can answer with is in a triage status by
   * construction (ADR-033).
   */
  async listInbox(filter: InboxFilter = {}): Promise<InboxPage> {
    const sort =
      filter.sort === undefined ? undefined : `${filter.order === 'desc' ? '-' : ''}${filter.sort}`;
    const query = buildQuery({
      project: filter.project,
      status: filter.status,
      type: filter.type,
      label: filter.label,
      assignee: filter.assignee,
      q: filter.text,
      sort,
      limit: filter.limit,
      cursor: filter.cursor === '' ? undefined : filter.cursor,
      fields: filter.fields?.join(','),
    });
    return toInboxPage(await this.#json(`${API_PREFIX}/inbox${query}`));
  }

  /** `POST /api/v1/items` with the `inbox` option: a submission, not a backlog item. */
  async createInboxItem(draft: InboxDraft): Promise<Item> {
    const { source, received, ...rest } = draft;
    const body = await this.#json(`${API_PREFIX}/items`, {
      method: 'POST',
      body: {
        ...rest,
        inbox: {
          ...(source === undefined ? {} : { source }),
          ...(received === undefined ? {} : { received }),
        },
      },
    });
    return this.#hydrate(body);
  }

  /**
   * `POST /api/v1/items/{id}/triage`. The revision travels in `If-Match`, never
   * in the body, so one write cannot claim two different preconditions.
   */
  async triageInboxItem(input: InboxTriageInput): Promise<InboxTriageResult> {
    const answer = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(input.id)}/triage`, {
      method: 'POST',
      rev: input.rev ?? '*',
      body: {
        action: input.action,
        ...(input.status === undefined ? {} : { status: input.status }),
        ...(input.parent === undefined ? {} : { parent: input.parent }),
        ...(input.snoozedUntil === undefined ? {} : { snoozedUntil: input.snoozedUntil }),
        ...(input.duplicateOf === undefined ? {} : { duplicateOf: input.duplicateOf }),
      },
    });
    return toInboxTriageResult(answer, input);
  }

  async addPageFeedback(
    scope: KbScope,
    path: string,
    notes: KbFeedbackNoteDraft[],
    rev?: string,
  ): Promise<KbPage> {
    const answer = await this.#json(`${kbBase(scope)}/feedback`, {
      method: 'POST',
      ...(rev === undefined ? {} : { rev }),
      body: { path, notes },
    });
    const page = toKbPage(answer, path);
    return page.rev === '' || page.body === '' ? this.getPage(scope, path) : page;
  }

  async writePage(scope: KbScope, path: string, content: string, rev?: string): Promise<KbPage> {
    const answer = await this.#json(`${kbBase(scope)}/page`, {
      method: 'PUT',
      ...(rev === undefined ? {} : { rev }),
      body: { path, content },
    });
    const page = toKbPage(answer, path);
    // A companion that answers with only `{path, rev}` still owes the UI a page.
    return page.rev === '' || page.body === '' ? this.getPage(scope, path) : page;
  }

  // ------------------------------------------------------------------- events

  /**
   * Opens the event socket on the first subscriber and closes it with the
   * last one, so an idle tab holds no connection.
   */
  // ---------------------------------------------------------------------- git

  /** `GET /api/v1/git/settings` (docs/07 §5.5, story GIT-US-0020). */
  async getGitSettings(): Promise<GitSettings> {
    return toGitSettings(await this.#json(`${API_PREFIX}/git/settings`));
  }

  /**
   * `PATCH /api/v1/git/settings`. The companion validates the template before
   * it applies anything, so a rejected patch leaves the running settings and
   * the configuration file untouched.
   */
  async updateGitSettings(patch: GitSettingsPatch): Promise<GitSettings> {
    return toGitSettings(
      await this.#json(`${API_PREFIX}/git/settings`, { method: 'PATCH', body: patch }),
    );
  }

  /** `GET /api/v1/git/status`. */
  async getGitStatus(repoId?: string): Promise<GitRepoStatus[]> {
    const body = await this.#json(`${API_PREFIX}/git/status${buildQuery({ repo: repoId })}`);
    const record = asRecord(body);
    return asArray(record ? record['repos'] : body) as GitRepoStatus[];
  }

  // ------------------------------------------------ semantic search settings

  /** `GET /api/v1/search/settings` (docs/07, story GIT-US-0091). */
  async getSearchSettings(): Promise<SearchSettings> {
    return toSearchSettings(await this.#json(`${API_PREFIX}/search/settings`));
  }

  /**
   * `PATCH /api/v1/search/settings`. Only the five location fields travel:
   * tokens are not patchable, so a credential can never be copied into the
   * configuration file by this path.
   */
  async updateSearchSettings(patch: SearchSettingsPatch): Promise<SearchSettings> {
    return toSearchSettings(
      await this.#json(`${API_PREFIX}/search/settings`, { method: 'PATCH', body: patch }),
    );
  }

  /**
   * `POST /api/v1/search/reindex` → `202` with the queued job. The work runs
   * in the background and reports on `search.progress`. A `repo` scopes it
   * to that one repository.
   */
  async reindexSearch(repo?: string): Promise<SearchReindexJob> {
    const body = repo === undefined || repo === '' ? {} : { repo };
    return toSearchReindexJob(
      await this.#json(`${API_PREFIX}/search/reindex`, { method: 'POST', body }),
    );
  }

  // --------------------------------------------------------- mcp write tools

  /** `GET /api/v1/mcp/settings`. */
  async getMcpSettings(): Promise<McpSettings> {
    return toMcpSettings(await this.#json(`${API_PREFIX}/mcp/settings`));
  }

  /** `PATCH /api/v1/mcp/settings`. */
  async setMcpWriteTools(allowWrite: boolean): Promise<McpSettings> {
    return toMcpSettings(
      await this.#json(`${API_PREFIX}/mcp/settings`, {
        method: 'PATCH',
        body: { allowWrite },
      }),
    );
  }

  // ------------------------------------------------------------------ tunnel

  /** `GET /api/v1/tunnel`. */
  async getTunnel(): Promise<TunnelStatus> {
    return toTunnelStatus(await this.#json(`${API_PREFIX}/tunnel`));
  }

  /**
   * `POST /api/v1/tunnel` to open it, `DELETE` to close it. The enable answers
   * `202` as soon as the hostname exists, which is before the edge resolves
   * it, so the returned `state` is normally `starting` and the caller polls.
   */
  async setTunnel(enabled: boolean): Promise<TunnelStatus> {
    return toTunnelStatus(
      await this.#json(`${API_PREFIX}/tunnel`, { method: enabled ? 'POST' : 'DELETE' }),
    );
  }

  // ---------------------------------------------------------------- youtrack

  /** `GET /api/v1/youtrack/settings`. */
  async getYouTrackSettings(scope: YouTrackScope = {}): Promise<YouTrackSettings> {
    return toYouTrackSettings(
      await this.#json(`${API_PREFIX}/youtrack/settings${youtrackQuery(scope)}`),
    );
  }

  /**
   * `PATCH /api/v1/youtrack/settings`. The body is passed through as given:
   * a key the caller omitted is left alone and one set to `''` is cleared, so
   * forgetting a token is `{token: ''}` and disconnecting a project is
   * `{url: '', project: ''}`.
   */
  async updateYouTrackSettings(
    patch: YouTrackSettingsPatch,
    scope: YouTrackScope = {},
  ): Promise<YouTrackSettings> {
    return toYouTrackSettings(
      await this.#json(`${API_PREFIX}/youtrack/settings${youtrackQuery(scope)}`, {
        method: 'PATCH',
        body: patch,
      }),
    );
  }

  /**
   * `POST /api/v1/youtrack/test`. An empty probe tests the saved connection;
   * a probe carrying a URL and a token tests one that has not been saved, which
   * is what lets the card refuse to write a credential that does not work.
   */
  async testYouTrackConnection(
    probe: { url?: string; token?: string } = {},
    scope: YouTrackScope = {},
  ): Promise<YouTrackTestResult> {
    return toYouTrackTestResult(
      await this.#json(`${API_PREFIX}/youtrack/test${youtrackQuery(scope)}`, {
        method: 'POST',
        body: {
          ...(probe.url === undefined ? {} : { url: probe.url }),
          ...(probe.token === undefined ? {} : { token: probe.token }),
        },
      }),
    );
  }

  /**
   * `GET /api/v1/youtrack/projects?q=`, or `POST` of the same path when the
   * caller carries a connection that is typed but not yet saved — which is how
   * the settings picker lists projects while it is being filled in.
   */
  async listYouTrackProjects(
    q?: string,
    scope: YouTrackScope = {},
    probe?: { url?: string; token?: string },
  ): Promise<YouTrackProject[]> {
    const unsaved = probe !== undefined && (probe.url ?? '') !== '' && (probe.token ?? '') !== '';
    const query = buildQuery({
      key: scope.projectKey,
      ...(unsaved ? {} : { q: q === '' ? undefined : q }),
    });
    const body = await this.#json(
      `${API_PREFIX}/youtrack/projects${query}`,
      unsaved
        ? { method: 'POST', body: { url: probe?.url, token: probe?.token, q: q ?? '' } }
        : undefined,
    );
    const record = asRecord(body);
    return asArray(record ? record['projects'] : body).map(toYouTrackProject);
  }

  /** `GET /api/v1/youtrack/fields?project=`. */
  async listYouTrackFields(
    project?: string,
    scope: YouTrackScope = {},
  ): Promise<YouTrackFieldList> {
    const query = buildQuery({
      key: scope.projectKey,
      project: project === '' ? undefined : project,
    });
    return toYouTrackFieldList(await this.#json(`${API_PREFIX}/youtrack/fields${query}`));
  }

  // --------------------------------------------------- youtrack import

  /**
   * `GET /api/v1/youtrack/issues?key=&q=&preset=&limit=&cursor=`
   * (story GIT-US-0054).
   */
  async searchYouTrackIssues(
    query: YouTrackIssueQuery,
    scope: YouTrackScope = {},
  ): Promise<YouTrackIssuePage> {
    const search = buildQuery({
      key: scope.projectKey,
      q: query.q === '' ? undefined : query.q,
      preset: query.preset === '' ? undefined : query.preset,
      limit: query.limit,
      cursor: query.cursor === '' ? undefined : query.cursor,
    });
    return toYouTrackIssuePage(await this.#json(`${API_PREFIX}/youtrack/issues${search}`));
  }

  /**
   * The preview operation of story GIT-US-0047, over REST.
   *
   * The vault method `youtrack.import.preview` exists and is what this calls;
   * the HTTP route in front of it is owned by the server side of this epic.
   * The body is the vault's `YouTrackImportParams` verbatim, so the two cannot
   * drift.
   */
  async previewYouTrackImport(
    options: YouTrackImportOptions,
    scope: YouTrackScope = {},
  ): Promise<YouTrackImportPreviewResult> {
    return toYouTrackImportPreview(
      await this.#json(`${API_PREFIX}/youtrack/import/preview${youtrackQuery(scope)}`, {
        method: 'POST',
        body: importBody(options),
      }),
    );
  }

  /**
   * The run operation of story GIT-US-0047, over REST. The companion enqueues
   * the import on its job engine and answers `202` with the job id; the caller
   * follows it over the `sync.job.*` events.
   */
  async runYouTrackImport(
    options: YouTrackImportOptions,
    scope: YouTrackScope = {},
  ): Promise<YouTrackImportRun> {
    return toYouTrackImportRun(
      await this.#json(`${API_PREFIX}/youtrack/import${youtrackQuery(scope)}`, {
        method: 'POST',
        body: importBody(options),
      }),
    );
  }

  // ------------------------------------------------- youtrack knowledge base

  /**
   * `GET /api/v1/youtrack/kb/status?key=&path=&recursive=&remote=`.
   *
   * Without `remote` nothing leaves the companion: the state comes from each
   * page's own `external` entry and the content it would publish, which is what
   * makes asking about a whole tree affordable.
   */
  async kbSyncStatus(selector: KbSyncSelector = {}): Promise<KbSyncStatusResult> {
    const query = buildQuery({
      key: selector.project,
      path: selector.path,
      recursive: selector.recursive,
      remote: selector.remote,
    });
    return toKbSyncStatusResult(await this.#json(`${YOUTRACK_KB_PATH}/status${query}`), selector);
  }

  /**
   * `POST /api/v1/youtrack/kb/publish`. It queues a job and returns: a handbook
   * is hundreds of articles, so the work happens where retries and rate
   * limiting already live. The `## Feedback` block is never part of it — it
   * stays in the repository (ADR-030).
   */
  async publishKbPage(selector: KbSyncSelector): Promise<KbSyncJobResult> {
    return this.#kbSyncJob('publish', selector);
  }

  /**
   * `POST /api/v1/youtrack/kb/unlink`. A page, never a folder: a tree-wide
   * unlink would be a bulk edit of committed files behind one click.
   */
  async unlinkKbPage(selector: KbSyncSelector): Promise<KbUnlinkResult> {
    const answer = asRecord(
      await this.#json(`${YOUTRACK_KB_PATH}/unlink${buildQuery({ key: selector.project })}`, {
        method: 'POST',
        body: { path: selector.path ?? '' },
      }),
    );
    const articleId = asString(answer?.['articleId']) ?? '';
    return {
      project: asString(answer?.['project']) ?? selector.project ?? '',
      path: asString(answer?.['path']) ?? selector.path ?? '',
      unlinked: asBoolean(answer?.['unlinked']) ?? false,
      ...(articleId === '' ? {} : { articleId }),
    };
  }

  /** `POST /api/v1/youtrack/kb/pull`. */
  async pullKbPage(selector: KbSyncSelector): Promise<KbSyncJobResult> {
    return this.#kbSyncJob('pull', selector);
  }

  async #kbSyncJob(
    direction: 'publish' | 'pull',
    selector: KbSyncSelector,
  ): Promise<KbSyncJobResult> {
    const answer = await this.#json(
      `${YOUTRACK_KB_PATH}/${direction}${buildQuery({ key: selector.project })}`,
      {
        method: 'POST',
        body: {
          ...(selector.path === undefined ? {} : { path: selector.path }),
          ...(selector.recursive === undefined ? {} : { recursive: selector.recursive }),
        },
      },
    );
    return toKbSyncJobResult(answer, selector);
  }

  /**
   * `POST /api/v1/youtrack/comments/push`.
   *
   * The answer says what was *queued*, never what arrived: the comment's own
   * `external` entry, written by the job, is the evidence a reader trusts.
   */
  async pushCommentToYoutrack(input: CommentPushInput): Promise<CommentPushResult> {
    const answer = await this.#json(
      `${YOUTRACK_COMMENTS_PATH}/push${buildQuery({ key: input.project })}`,
      {
        method: 'POST',
        body: {
          itemId: input.itemId,
          ...(input.commentPath === undefined ? {} : { commentPath: input.commentPath }),
          ...(input.all === undefined ? {} : { all: input.all }),
        },
      },
    );
    return toCommentPushResult(answer, input);
  }

  // ----------------------------------------------------- background jobs

  /** `GET /api/v1/sync/jobs?state=&kind=&limit=&cursor=`. */
  async listSyncJobs(filter: SyncJobFilter = {}): Promise<SyncJobPage> {
    const query = buildQuery({
      state: filter.state,
      kind: filter.kind,
      limit: filter.limit,
      cursor: filter.cursor === '' ? undefined : filter.cursor,
    });
    return toSyncJobPage(await this.#json(`${API_PREFIX}/sync/jobs${query}`));
  }

  /** `GET /api/v1/sync/jobs/{id}`. */
  async getSyncJob(id: string): Promise<SyncJob> {
    return toSyncJob(await this.#json(`${API_PREFIX}/sync/jobs/${encodeURIComponent(id)}`));
  }

  /** `POST /api/v1/sync/jobs/{id}/retry`. */
  async retrySyncJob(id: string): Promise<SyncJob> {
    return toSyncJob(
      await this.#json(`${API_PREFIX}/sync/jobs/${encodeURIComponent(id)}/retry`, {
        method: 'POST',
      }),
    );
  }

  /** `POST /api/v1/sync/jobs/{id}/cancel`. */
  async cancelSyncJob(id: string): Promise<SyncJob> {
    return toSyncJob(
      await this.#json(`${API_PREFIX}/sync/jobs/${encodeURIComponent(id)}/cancel`, {
        method: 'POST',
      }),
    );
  }

  /** `POST /api/v1/git/commit`; with no paths it flushes the batched edits. */
  async commitNow(
    input: { repoId?: string; paths?: string[]; message?: string } = {},
  ): Promise<GitCommit[]> {
    const body = await this.#json(`${API_PREFIX}/git/commit`, {
      method: 'POST',
      body: {
        ...(input.repoId === undefined ? {} : { repo: input.repoId }),
        ...(input.paths === undefined ? {} : { paths: input.paths }),
        ...(input.message === undefined ? {} : { message: input.message }),
      },
    });
    const record = asRecord(body);
    return asArray(record ? record['commits'] : body) as GitCommit[];
  }

  // --------------------------------------------------------------- git sync

  /** `GET /api/v1/sync/status`. */
  async getSyncStatus(repoId?: string): Promise<SyncRepoStatus[]> {
    const body = asRecord(
      await this.#json(`${API_PREFIX}/sync/status${buildQuery({ repo: repoId })}`),
    );
    return asArray(body ? body['repos'] : []) as SyncRepoStatus[];
  }

  /**
   * `GET /api/v1/sync/settings` — the git half of GIT-US-0021 and the engine
   * half of GIT-US-0084 in one document.
   */
  async getSyncSettings(): Promise<SyncSettings> {
    return toSyncSettings(await this.#json(`${API_PREFIX}/sync/settings`));
  }

  /**
   * `PATCH /api/v1/sync/settings`. The engine knobs go nested under `engine`,
   * which is the form that wins when a companion is sent both.
   */
  async updateSyncSettings(patch: SyncSettingsPatch): Promise<SyncSettings> {
    return toSyncSettings(
      await this.#json(`${API_PREFIX}/sync/settings`, { method: 'PATCH', body: patch }),
    );
  }

  /**
   * `POST /api/v1/sync/run`. The companion commits what commit-on-save batched
   * before it fetches, so the panel needs no separate "commit first" step.
   */
  async sync(repoId: string | undefined, opts: SyncOptions = {}): Promise<SyncResult[]> {
    const body = asRecord(
      await this.#json(`${API_PREFIX}/sync/run`, {
        method: 'POST',
        body: {
          ...(repoId === undefined ? {} : { repos: [repoId] }),
          ...(opts.dryRun === undefined ? {} : { dryRun: opts.dryRun }),
          ...(opts.push === undefined ? {} : { push: opts.push }),
          ...(opts.strategy === undefined ? {} : { strategy: opts.strategy }),
        },
      }),
    );
    return asArray(body ? body['results'] : []) as SyncResult[];
  }

  /** `POST /api/v1/sync/abort`. */
  async abortSync(repoId: string): Promise<SyncRepoStatus> {
    return (await this.#json(`${API_PREFIX}/sync/abort`, {
      method: 'POST',
      body: { repo: repoId },
    })) as SyncRepoStatus;
  }

  /** `GET /api/v1/sync/conflicts`. */
  async listSyncConflicts(
    repoId?: string,
  ): Promise<{ repo: string; paths: string[]; operation?: string }[]> {
    const body = asRecord(
      await this.#json(`${API_PREFIX}/sync/conflicts${buildQuery({ repo: repoId })}`),
    );
    return asArray(body ? body['conflicts'] : []) as {
      repo: string;
      paths: string[];
      operation?: string;
    }[];
  }

  /** `GET /api/v1/sync/conflicts/file`: the three versions and the merge. */
  async readConflict(repoId: string, path: string): Promise<ConflictAnalysis> {
    return (await this.#json(
      `${API_PREFIX}/sync/conflicts/file${buildQuery({ repo: repoId, path })}`,
    )) as ConflictAnalysis;
  }

  /**
   * `POST /api/v1/sync/conflicts/resolve`: write the resolution, stage it and
   * finish the rebase or merge. The merge itself runs in the core, so browser
   * mode and the companion resolve a conflict by exactly the same rules.
   */
  async resolveConflict(
    repoId: string,
    path: string,
    resolution: ConflictResolution,
  ): Promise<ConflictResolveResult> {
    return (await this.#json(`${API_PREFIX}/sync/conflicts/resolve`, {
      method: 'POST',
      body: {
        repo: repoId,
        path,
        resolution: resolution.resolution,
        ...(resolution.content === undefined ? {} : { content: resolution.content }),
        ...(resolution.body === undefined ? {} : { body: resolution.body }),
        ...(resolution.fields === undefined ? {} : { fields: resolution.fields }),
        ...(resolution.hunks === undefined ? {} : { hunks: resolution.hunks }),
        ...(resolution.hunkText === undefined ? {} : { hunkText: resolution.hunkText }),
        ...(resolution.continue === undefined ? {} : { continue: resolution.continue }),
      },
    })) as ConflictResolveResult;
  }

  // ------------------------------------------------------------------ agent

  async getAgentInfo(options: AgentRequestOptions = {}): Promise<AguiInfo> {
    return (await this.#json(
      `${AGENT_PREFIX}/info${buildQuery({ repo: options.repo })}`,
      agentRequest(options),
    )) as AguiInfo;
  }

  async getAgentHealth(options: AgentRequestOptions = {}): Promise<AgentHealth> {
    const body = asRecord(
      await this.#json(
        `${AGENT_PREFIX}/health${buildQuery({ repo: options.repo })}`,
        agentRequest(options),
      ),
    );
    const version = asString(body?.['version']);
    const detail = asString(body?.['detail']);
    return {
      ok: asBoolean(body?.['ok']) ?? true,
      ...(version === undefined ? {} : { version }),
      ...(detail === undefined ? {} : { detail }),
    };
  }

  /**
   * POSTs a `RunAgentInput` and streams back the AG-UI events.
   *
   * It is `async *` rather than a callback feed on purpose: `PandoThread`
   * consumes exactly this shape, so the SDK's reducer plugs onto the provider
   * seam with no adapter in between. The bearer token is the companion's own
   * (`token.ts`); the Pando token lives on the companion side and never
   * reaches the browser.
   */
  async *runAgent(input: RunAgentInput, options: AgentRunOptions = {}): AsyncIterable<AguiEvent> {
    const path = `${AGENT_PREFIX}/run${buildQuery({ repo: options.repo })}`;
    const response = await this.#send(path, {
      ...agentRequest(options),
      method: 'POST',
      body: input,
      accept: 'text/event-stream',
    });
    yield* agentEvents(response, path);
  }

  async listAgentThreads(options: AgentRequestOptions = {}): Promise<AgentThreadSummary[]> {
    const body = await this.#json(
      `${AGENT_PREFIX}/threads${buildQuery({ repo: options.repo })}`,
      agentRequest(options),
    );
    const rows = Array.isArray(body) ? body : (asRecord(body)?.['threads'] ?? []);
    if (!Array.isArray(rows)) return [];
    return rows.map(toAgentThreadSummary).filter((row): row is AgentThreadSummary => row !== null);
  }

  async getAgentThreadMessages(
    threadId: string,
    options: AgentRequestOptions = {},
  ): Promise<AguiMessage[]> {
    const body = await this.#json(
      `${AGENT_PREFIX}/threads/${encodeURIComponent(threadId)}/messages${buildQuery({
        repo: options.repo,
      })}`,
      agentRequest(options),
    );
    const rows = Array.isArray(body) ? body : (asRecord(body)?.['messages'] ?? []);
    return Array.isArray(rows) ? (rows as AguiMessage[]) : [];
  }

  async *streamAgentThread(
    threadId: string,
    options: AgentRunOptions = {},
  ): AsyncIterable<AguiEvent> {
    const path = `${AGENT_PREFIX}/threads/${encodeURIComponent(threadId)}/stream${buildQuery({
      repo: options.repo,
    })}`;
    const response = await this.#send(path, {
      ...agentRequest(options),
      accept: 'text/event-stream',
    });
    yield* agentEvents(response, path);
  }

  async deleteAgentThread(threadId: string, options: AgentRequestOptions = {}): Promise<void> {
    await this.#send(
      `${AGENT_PREFIX}/threads/${encodeURIComponent(threadId)}${buildQuery({ repo: options.repo })}`,
      { ...agentRequest(options), method: 'DELETE' },
    );
  }

  async cancelAgentRun(threadId: string, options: AgentRequestOptions = {}): Promise<void> {
    await this.#send(
      `${AGENT_PREFIX}/runs/${encodeURIComponent(threadId)}/cancel${buildQuery({
        repo: options.repo,
      })}`,
      { ...agentRequest(options), method: 'POST' },
    );
  }

  // ---------------------------------------------------------------- specs
  //
  // `/projects/{key}/specs` (docs/07 §5.5, GIT-US-0127). The companion answers
  // with the core's own values, so these are passed through with no mapping
  // beyond the list total; a missing seam arrives as `503 unavailable`.

  async listSpecs(project: string, filter: SpecFilter = {}): Promise<ItemPage> {
    const response = await this.#send(`${specsBase(project)}${itemFilterQuery(filter)}`);
    const body = await readJson(response);
    return toItemPage(body, response.headers?.get('X-Total-Count') ?? null);
  }

  async getSpec(project: string, id: string): Promise<Item> {
    return toItem(await this.#json(`${specsBase(project)}/${encodeURIComponent(id)}`));
  }

  async listRequirements(
    project: string,
    filter: RequirementFilter = {},
  ): Promise<RequirementList> {
    const { spec, ...rest } = filter;
    const path =
      spec === undefined
        ? `${specsBase(project)}/requirements`
        : `${specsBase(project)}/${encodeURIComponent(spec)}/requirements`;
    const body = await this.#json(
      `${path}${buildQuery({
        status: rest.status,
        q: rest.q,
        text: rest.text,
        includeDeleted: rest.includeDeleted,
      })}`,
    );
    return toRequirementList(body);
  }

  async getRequirement(project: string, ref: string): Promise<RequirementRead> {
    return (await this.#json(requirementPath(project, ref))) as RequirementRead;
  }

  async createRequirement(
    project: string,
    draft: RequirementDraft,
  ): Promise<RequirementWriteResult> {
    const { spec, ...body } = draft;
    return (await this.#json(`${specsBase(project)}/${encodeURIComponent(spec)}/requirements`, {
      method: 'POST',
      body,
    })) as RequirementWriteResult;
  }

  async updateRequirement(
    project: string,
    ref: string,
    patch: RequirementPatch,
    rev: string,
  ): Promise<RequirementWriteResult> {
    return (await this.#json(requirementPath(project, ref), {
      method: 'PATCH',
      rev,
      body: { patch },
    })) as RequirementWriteResult;
  }

  async traceRequirement(project: string, ref: string): Promise<TracedRequirement> {
    const body = asRecord(await this.#json(`${requirementPath(project, ref)}/trace`));
    const trace = body?.['trace'];
    if (asRecord(trace) === null) throw malformed('requirement trace');
    return trace as TracedRequirement;
  }

  async listCoverage(project: string, filter: CoverageFilter = {}): Promise<CoverageList> {
    const query = buildQuery({ spec: filter.spec, ref: filter.refs, status: filter.status });
    const body = asRecord(await this.#json(`${specsBase(project)}/coverage${query}`));
    const coverage = asArray(body?.['coverage']) as CoverageList['coverage'];
    return { coverage, total: asNumber(body?.['total']) ?? coverage.length };
  }

  async queryImpact(project: string, query: ImpactQuery = {}): Promise<ImpactResult> {
    const body = asRecord(await this.#json(`${specsBase(project)}/impact${impactQuery(query)}`));
    const impact = body?.['impact'];
    if (asRecord(impact) === null) throw malformed('impact');
    return impact as ImpactResult;
  }

  async getImpactReport(project: string, query: ImpactReportQuery = {}): Promise<ImpactReport> {
    const { budget, cursor, format, ...rest } = query;
    const search = impactQuery(rest, { budget, cursor, format });
    const body = asRecord(await this.#json(`${specsBase(project)}/impact/report${search}`));
    const report = body?.['report'];
    if (asRecord(report) === null) throw malformed('impact report');
    return report as ImpactReport;
  }

  async lintSpecText(project: string, input: SpecLintInput): Promise<SpecLintFinding[]> {
    const body = asRecord(
      await this.#json(`${specsBase(project)}/lint`, { method: 'POST', body: input }),
    );
    if (body === null) throw malformed('spec lint');
    return asArray(body['findings']) as SpecLintFinding[];
  }

  async previewSpecDelta(
    project: string,
    input: SpecDeltaPreviewInput,
  ): Promise<DeltaPreviewOperation[]> {
    const body = asRecord(
      await this.#json(`${specsBase(project)}/delta/preview`, { method: 'POST', body: input }),
    );
    if (body === null) throw malformed('spec delta preview');
    return asArray(body['operations']) as DeltaPreviewOperation[];
  }

  subscribe(handler: (event: ChangeEvent) => void): Unsubscribe {
    this.#handlers.add(handler);
    if (this.#handlers.size === 1) this.#connect();
    return () => {
      this.#handlers.delete(handler);
      if (this.#handlers.size === 0) this.#teardown('idle');
    };
  }

  /** Releases the socket and every timer; called when the provider is replaced. */
  dispose(): void {
    this.#disposed = true;
    this.#handlers.clear();
    this.#teardown('closed');
    this.#connectionListeners.clear();
  }

  // ---------------------------------------------------------------- internals

  #url(path: string): string {
    return `${this.#baseUrl}${path}`;
  }

  async #send(path: string, options: RequestOptions = {}): Promise<Response> {
    const headers: Record<string, string> = {
      Accept: options.accept ?? 'application/json',
      ...authorizationHeader(),
    };
    if (options.body !== undefined) headers['Content-Type'] = 'application/json';
    // Optimistic concurrency: every mutation carries the rev it read.
    if (options.rev !== undefined) headers['If-Match'] = options.rev;

    let response: Response;
    try {
      response = await this.#fetch(this.#url(path), {
        method: options.method ?? 'GET',
        mode: 'cors',
        credentials: 'omit',
        headers,
        ...(options.signal === undefined ? {} : { signal: options.signal }),
        ...(options.body === undefined ? {} : { body: JSON.stringify(options.body) }),
      });
    } catch (error) {
      // The caller aborted on purpose. Reporting that as an unreachable
      // companion would send the UI hunting for a connection problem that is
      // not there.
      if (options.signal?.aborted === true) throw error;
      throw new ProviderError(
        'internal',
        `The companion at ${this.baseUrl} is unreachable (${
          error instanceof Error ? error.message : String(error)
        }).`,
        path,
      );
    }

    if (!response.ok) throw await this.#toProviderError(response, path);
    return response;
  }

  async #json(path: string, options: RequestOptions = {}): Promise<unknown> {
    return readJson(await this.#send(path, options));
  }

  /** A partial write answer (`{id, rev}`) is completed with one read. */
  async #hydrate(body: unknown, fallbackId?: string): Promise<Item> {
    const record = asRecord(body);
    const id = (record ? asString(record['id']) : undefined) ?? fallbackId;
    if (record && asItemType(record['type']) !== undefined && asString(record['title'])) {
      return toItem(record);
    }
    if (id === undefined) throw malformed('item');
    return this.getItem(id);
  }

  async #toProviderError(response: Response, path: string): Promise<ProviderError> {
    const problem = parseProblem(await readJson(response).catch(() => null));
    const status = response.status;
    const fallback = `${status} ${response.statusText || 'Request failed'}`;

    const mapped = problem?.code === undefined ? undefined : PROBLEM_CODES[problem.code];
    // A `401` carrying, say, `git_auth_failed` is the *remote* refusing an SSH
    // key, not the companion refusing our token: only an unmapped 401 (or the
    // explicit `unauthorized` code) invalidates the stored credential.
    if (problem?.code === 'unauthorized' || (status === 401 && mapped === undefined)) {
      // The stored credential is worthless: forget it and let the UI ask again.
      clearToken();
      return new CompanionUnauthorizedError(
        problemMessage(
          problem ?? {},
          hasToken()
            ? 'The companion rejected the access token.'
            : 'The companion needs an access token.',
        ),
      );
    }

    const code = mapped ?? codeFromStatus(status, this.#capabilities.write);
    const details: ProviderErrorDetails = {};
    put(details, 'currentRev', problem?.currentRev);
    put(details, 'conflicts', problem?.conflicts);
    return new ProviderError(code, problemMessage(problem ?? {}, fallback), path, details);
  }

  #setConnection(state: ConnectionState): void {
    if (this.#connection === state) return;
    this.#connection = state;
    for (const listener of [...this.#connectionListeners]) listener(state);
  }

  #emit(event: ChangeEvent): void {
    for (const handler of [...this.#handlers]) handler(event);
  }

  /** "Something changed, refetch": used by `resume.gap` and by polling. */
  #emitRefresh(repoId = ''): void {
    this.#emit({ kind: 'repo', repoId });
    this.#emit({ kind: 'kb', repoId, paths: [] });
    this.#emitSyncJobResync();
  }

  /**
   * "The job stream lost its place, reconcile from `GET /sync/jobs`."
   *
   * A client may miss frames — the hub disconnects one that fills its
   * 256-event buffer — so a reconnect, a `stream.overflow` and a `resume.gap`
   * all mean the live counts are no longer trustworthy. The listing is the
   * engine's own consistent snapshot and is the only thing that is.
   */
  #emitSyncJobResync(): void {
    this.#emit({
      kind: 'syncJob',
      job: {
        phase: 'resync',
        id: '',
        kind: '',
        key: '',
        state: '',
        attempt: 0,
        processed: 0,
        total: 0,
        error: '',
        errorClass: '',
      },
    });
  }

  #eventsUrl(): string {
    const base = this.#baseUrl === '' ? (globalThis.location?.origin ?? '') : this.#baseUrl;
    const url = `${base}${EVENTS_PATH}`;
    return withTokenQuery(url.replace(/^http/, 'ws'));
  }

  #connect(): void {
    if (this.#disposed || this.#socket !== null || this.#handlers.size === 0) return;
    if (this.#webSocketFactory === null) {
      this.#degrade();
      return;
    }

    this.#setConnection(this.#attempts === 0 ? 'connecting' : 'reconnecting');

    let socket: WebSocketLike;
    try {
      socket = this.#webSocketFactory(this.#eventsUrl());
    } catch {
      this.#scheduleReconnect();
      return;
    }
    this.#socket = socket;

    socket.onopen = () => {
      const reconnected = this.#attempts > 0;
      this.#attempts = 0;
      this.#stopPolling();
      this.#setConnection('open');
      socket.send(JSON.stringify({ op: 'subscribe', topics: SUBSCRIBE_TOPICS }));
      // Replays everything missed while the socket was down (docs/07 §5.6).
      if (this.#lastSeq !== null) {
        socket.send(JSON.stringify({ op: 'resume', seq: this.#lastSeq }));
      }
      // The replay ring may no longer hold our position, and a job can have
      // finished while the socket was down. Reconcile rather than trust it.
      if (reconnected) this.#emitSyncJobResync();
    };
    socket.onmessage = (event) => {
      this.#receive(event.data);
    };
    socket.onerror = () => {
      // `onclose` always follows; the reconnect is scheduled there.
    };
    socket.onclose = () => {
      this.#socket = null;
      if (this.#disposed || this.#handlers.size === 0) return;
      this.#scheduleReconnect();
    };
  }

  #receive(data: unknown): void {
    if (typeof data !== 'string') return;
    let parsed: unknown;
    try {
      parsed = JSON.parse(data);
    } catch {
      return;
    }
    const frame = asRecord(parsed);
    if (!frame) return;

    const seq = asNumber(frame['seq']);
    if (seq !== undefined) this.#lastSeq = seq;

    const type = asString(frame['type']);
    if (type === undefined) return;
    if (type === 'resume.gap' || type === 'stream.overflow') {
      // The ring buffer no longer holds our position, or this client fell
      // behind and was cut off: refetch everything.
      this.#lastSeq = null;
      this.#emitRefresh();
      return;
    }

    const payload = asRecord(frame['data']) ?? {};
    const repoId = asString(payload['repo']) ?? '';

    const phase = SYNC_JOB_PHASES[type];
    if (phase !== undefined) {
      // Progress is already coalesced server-side to one frame per 500 ms per
      // group, and a terminal frame is never throttled, so this layer adds no
      // throttling of its own and treats a missing intermediate frame as
      // normal.
      this.#emit({ kind: 'syncJob', job: toSyncJobEvent(phase, payload) });
      return;
    }

    switch (type) {
      case 'inbox.changed': {
        this.#emit({
          kind: 'inbox',
          repoId,
          project: asString(payload['project']) ?? '',
          id: asString(payload['id']) ?? '',
          action: asString(payload['action']) ?? '',
          pending: asNumber(payload['pendingCount']) ?? 0,
        });
        return;
      }
      case 'sprint.changed': {
        this.#emit({
          kind: 'sprint',
          sprint: asString(payload['sprint']) ?? '',
          board: asString(payload['board']) ?? '',
          state: asString(payload['state']) ?? '',
          carried: asNumber(payload['carried']) ?? 0,
          failed: asNumber(payload['failed']) ?? 0,
        });
        return;
      }
      case 'youtrack.kb.conflict': {
        const path = asString(payload['path']) ?? '';
        const event: Extract<ChangeEvent, { kind: 'kbConflict' }> = {
          kind: 'kbConflict',
          project: asString(payload['project']) ?? '',
          path,
          conflictPath: asString(payload['conflictPath']) ?? '',
          direction: asString(payload['direction']) === 'publish' ? 'publish' : 'pull',
        };
        const articleId = asString(payload['articleId']);
        if (articleId !== undefined) event.articleId = articleId;
        this.#emit(event);
        return;
      }
      case 'item.changed': {
        const id = asString(payload['id']);
        if (id !== undefined) this.#emit({ kind: 'items', repoId, ids: [id] });
        return;
      }
      case 'index.updated': {
        this.#emit({ kind: 'index', repoId, stats: toIndexStats(payload) });
        return;
      }
      case 'file.changed': {
        // Item files are already covered by `item.changed`; KB files are not.
        if (asBoolean(payload['isKb']) === true) {
          const path = asString(payload['path']);
          this.#emit({ kind: 'kb', repoId, paths: path === undefined ? [] : [path] });
        }
        return;
      }
      case 'search.progress': {
        this.#emit({
          kind: 'searchProgress',
          operationId: asString(payload['operationId']) ?? '',
          repoId,
          phase: toSearchPhase(asString(payload['phase'])),
          percent: asNumber(payload['percent']) ?? 0,
          done: asNumber(payload['done']) ?? 0,
          total: asNumber(payload['total']) ?? 0,
          message: asString(payload['message']) ?? '',
        });
        return;
      }
      case 'sync.progress':
      case 'conflict.detected': {
        this.#emit({ kind: 'repo', repoId });
        return;
      }
      default:
        return;
    }
  }

  /** Exponential backoff with jitter, then a plain polling fallback. */
  #scheduleReconnect(): void {
    this.#attempts += 1;
    if (this.#attempts >= MAX_SOCKET_ATTEMPTS) {
      this.#degrade();
      return;
    }
    const ceiling = Math.min(RECONNECT_BASE_MS * 2 ** (this.#attempts - 1), RECONNECT_MAX_MS);
    const delay = ceiling / 2 + this.#random() * (ceiling / 2);
    this.#setConnection('reconnecting');
    this.#reconnectTimer = setTimeout(() => {
      this.#reconnectTimer = null;
      this.#connect();
    }, delay);
  }

  /**
   * The socket cannot be opened (no WebSocket, a proxy in the way, a companion
   * that only speaks REST). Fall back to an interval refresh signal and keep
   * trying to upgrade back to the socket on every tick.
   */
  #degrade(): void {
    if (this.#pollTimer !== null) return;
    this.#setConnection('polling');
    this.#pollTimer = setInterval(() => {
      this.#emitRefresh();
      this.#connect();
    }, POLL_INTERVAL_MS);
  }

  #stopPolling(): void {
    if (this.#pollTimer === null) return;
    clearInterval(this.#pollTimer);
    this.#pollTimer = null;
  }

  #teardown(state: ConnectionState): void {
    if (this.#reconnectTimer !== null) {
      clearTimeout(this.#reconnectTimer);
      this.#reconnectTimer = null;
    }
    this.#stopPolling();
    this.#attempts = 0;
    const socket = this.#socket;
    this.#socket = null;
    if (socket) {
      socket.onopen = null;
      socket.onmessage = null;
      socket.onerror = null;
      socket.onclose = null;
      try {
        socket.close();
      } catch {
        // A socket that refuses to close is already gone.
      }
    }
    this.#setConnection(state);
  }
}

/** `ItemPatch` → the flat body `PATCH /items/{id}` documents. */
export function toRestPatch(patch: ItemPatch): Record<string, unknown> {
  const body: Record<string, unknown> = { ...(patch.set ?? {}) };
  if (patch.unset !== undefined && patch.unset.length > 0) body['unset'] = patch.unset;
  if (patch.removeExternal !== undefined && patch.removeExternal.length > 0) {
    body['removeExternal'] = patch.removeExternal;
  }
  if (patch.body !== undefined) body['body'] = patch.body;
  return body;
}

async function readJson(response: Response): Promise<unknown> {
  if (response.status === 204) return null;
  try {
    return (await response.json()) as unknown;
  } catch {
    return null;
  }
}

function defaultWebSocketFactory(): WebSocketFactory | null {
  if (typeof globalThis.WebSocket !== 'function') return null;
  return (url) => new globalThis.WebSocket(url) as unknown as WebSocketLike;
}
