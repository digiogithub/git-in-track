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

import { resolveCompanionBaseUrl } from '@/api/detect';
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
  ItemType,
  KbNode,
  KbPage,
  KbScope,
  MountInput,
  Priority,
  ProjectSummary,
  ProviderErrorCode,
  RepoInfo,
  SearchHit,
  SearchQuery,
  Unsubscribe,
  UpdateOp,
} from '@/api/provider';
import { ProviderError } from '@/api/provider';
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
];

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
  not_found: 'not_found',
  repo_not_registered: 'not_found',
  read_only: 'read_only',
  forbidden: 'permission_denied',
  git_conflict: 'git_conflict',
  git_dirty: 'git_conflict',
  git_auth_failed: 'git_auth_failed',
  repo_not_cloned: 'repo_not_cloned',
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
  put(problem, 'code', asString(record['code']));
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
  return problem;
}

// ------------------------------------------------------------------ mappers

const ITEM_TYPES: ItemType[] = ['epic', 'story', 'task', 'milestone', 'comment'];
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
  put(comment, 'created', asString(record['created']));
  put(comment, 'updated', asString(record['updated']));
  put(comment, 'inReplyTo', asString(record['inReplyTo']));
  put(comment, 'kind', asString(record['kind']));
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

  return {
    key,
    name: asString(record['name']) ?? key,
    docsPath: asString(record['docsPath']) ?? asString(record['docs']) ?? '',
    statuses: toStatuses(record['statuses'] ?? record['workflow']),
    labels,
    priorities: priorities.length > 0 ? priorities : [...PRIORITIES],
    itemCounts: toItemCounts(record['counts'] ?? record['itemCounts']),
  };
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

export function toSearchHits(value: unknown): SearchHit[] {
  const record = asRecord(value);
  const results = record ? asArray(record['results']) : asArray(value);
  return results
    .map((entry) => {
      const hit = asRecord(entry);
      if (!hit) return null;
      const mapped: SearchHit = {
        kind: asString(hit['kind']) === 'item' ? 'item' : 'page',
        path: asString(hit['path']) ?? '',
        title: asString(hit['title']) ?? '',
        snippet: asString(hit['snippet']) ?? '',
        score: asNumber(hit['score']) ?? 0,
      };
      put(mapped, 'id', asString(hit['id']));
      return mapped;
    })
    .filter((entry): entry is SearchHit => entry !== null);
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
    fullTextSearch: asString(features['search']) === 'bleve' ? 'bleve' : 'core',
    mcp: asBoolean(features['mcpHttp']) ?? false,
    openInEditor: asBoolean(features['openInEditor']) ?? companionCapabilities.openInEditor,
    maxBatchWrite: asNumber(limits['maxBatchWrite']) ?? companionCapabilities.maxBatchWrite,
  };
}

// ------------------------------------------------------------- query strings

type QueryValue = string | number | boolean | string[] | undefined;

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
};

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

  async search(query: SearchQuery): Promise<SearchHit[]> {
    const search = buildQuery({
      q: query.text,
      scope: 'items,kb',
      project: query.projectKey,
      limit: query.limit,
    });
    return toSearchHits(await this.#json(`${API_PREFIX}/search${search}`));
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

  async moveItem(id: string, status: ItemStatus, rev: string): Promise<Item> {
    const body = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}/move`, {
      method: 'POST',
      rev,
      body: { status },
    });
    return this.#hydrate(body, id);
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

  async deleteItem(id: string, rev: string): Promise<void> {
    await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}`, { method: 'DELETE', rev });
  }

  async addComment(id: string, body: string, author = 'me'): Promise<Comment> {
    const answer = await this.#json(`${API_PREFIX}/items/${encodeURIComponent(id)}/comments`, {
      method: 'POST',
      body: { body, author },
    });
    return toComment(answer, { item: id, author, body });
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
        ...(options.body === undefined ? {} : { body: JSON.stringify(options.body) }),
      });
    } catch (error) {
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
    return new ProviderError(code, problemMessage(problem ?? {}, fallback), path);
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
      this.#attempts = 0;
      this.#stopPolling();
      this.#setConnection('open');
      socket.send(JSON.stringify({ op: 'subscribe', topics: SUBSCRIBE_TOPICS }));
      // Replays everything missed while the socket was down (docs/07 §5.6).
      if (this.#lastSeq !== null) {
        socket.send(JSON.stringify({ op: 'resume', seq: this.#lastSeq }));
      }
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
    if (type === 'resume.gap') {
      // The ring buffer no longer holds our position: refetch everything.
      this.#lastSeq = null;
      this.#emitRefresh();
      return;
    }

    const payload = asRecord(frame['data']) ?? {};
    const repoId = asString(payload['repo']) ?? '';

    switch (type) {
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
