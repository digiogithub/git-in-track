/**
 * In-memory `DataProvider` for component tests and Storybook-like previews.
 *
 * It is deliberately simple: no cursor pagination beyond `limit`, substring
 * text search, and revs recomputed from a counter. It mirrors the semantics
 * that matter to the UI: rev checks, read-only capability, change events.
 */

import type {
  BatchResult,
  BoardCard,
  BoardColumnView,
  BoardMoveResult,
  BoardSummary,
  BoardView,
  CardMove,
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
  KbNode,
  KbPage,
  KbScope,
  MountInput,
  ProjectSummary,
  RefResolution,
  RepoInfo,
  SearchHit,
  SearchQuery,
  TeamSummary,
  Unsubscribe,
  UpdateOp,
} from '@/api/provider';
import { ProviderError, readOnlyCapabilities } from '@/api/provider';

export type FakeData = {
  projects?: ProjectSummary[];
  items?: Item[];
  comments?: Comment[];
  pages?: KbPage[];
  repos?: RepoInfo[];
  /** The team repository of the workspace; omit for a workspace without one. */
  team?: TeamSummary | null;
  /** The boards of the team repository; omit for the sample board. */
  boards?: FakeBoard[];
};

/**
 * A board as the team repository stores it: columns mapping onto per-project
 * statuses, an advisory WIP limit and one card ref per line under `order`
 * (docs/04-team-repository.md §5). The fake renders it the way the Go core
 * does, so a component test exercises the real semantics.
 */
export type FakeBoard = {
  id: string;
  kind: 'kanban' | 'scrum';
  title: string;
  description?: string;
  projects: string[];
  columns: {
    id: string;
    name: string;
    /** Project key, or `*` for the default rule. */
    statuses: Record<string, string[]>;
    wip?: number;
    color?: string;
  }[];
  filters?: BoardView['filters'];
  order: Record<string, string[]>;
  rev: string;
};

/** The board of `sampleTeam`: cards from a cloned project and a remote one. */
export const sampleBoard: FakeBoard = {
  id: 'delivery',
  kind: 'kanban',
  title: 'Delivery',
  description: 'Everything the squad is working on, across both repositories.',
  projects: ['ACME', 'WEB'],
  columns: [
    { id: 'todo', name: 'To Do', statuses: { '*': ['backlog', 'todo'] }, color: '#94a3b8' },
    { id: 'in_progress', name: 'In Progress', statuses: { '*': ['in_progress'] }, wip: 1 },
    { id: 'in_review', name: 'In Review', statuses: { '*': ['in_review'] }, wip: 2 },
    { id: 'done', name: 'Done', statuses: { '*': ['done', 'cancelled'] } },
  ],
  filters: { types: ['story', 'task'] },
  order: {
    todo: ['ACME/ACME-T-0107', 'WEB/WEB-US-0031'],
    in_progress: ['ACME/ACME-US-0042'],
    in_review: [],
    done: [],
  },
  rev: 'sha256:00000000000000b1',
};

const writableCapabilities: Capabilities = {
  ...readOnlyCapabilities,
  write: true,
  maxBatchWrite: 50,
};

/**
 * A team repository declaring two projects: one the workspace has open and one
 * nobody cloned, which is the shape docs/04 §7 asks the UI to render.
 */
export const sampleTeam: TeamSummary = {
  key: 'ACME-TEAM',
  name: 'ACME Delivery Team',
  description: 'Squad owning the platform and the marketing website.',
  timezone: 'Europe/Madrid',
  root: '.',
  knowledgePath: 'knowledge',
  vaultId: 'repo-team',
  members: [
    {
      handle: 'jose',
      name: 'Jose Ruiz',
      role: 'lead',
      emails: ['jose@example.com'],
      active: true,
    },
    { handle: 'marta', name: 'Marta Alonso', role: 'dev', active: true },
    { handle: 'laura', name: 'Laura Prat', role: 'dev', active: false },
  ],
  projects: [
    {
      key: 'ACME',
      name: 'ACME Platform',
      repo: 'https://github.com/acme/platform.git',
      docsPath: 'docs',
      host: 'github',
      webUrl: 'https://github.com/acme/platform',
      cloned: true,
      vaultId: 'repo-1',
      localDocsPath: 'docs',
    },
    {
      key: 'WEB',
      name: 'Marketing Website',
      repo: 'https://gitlab.com/acme/website.git',
      docsPath: 'documentation',
      host: 'gitlab',
      webUrl: 'https://gitlab.com/acme/website',
      cloned: false,
    },
  ],
  policies: { definition_of_done: 'knowledge/ways-of-working/definition-of-done.md' },
  cadence: { sprintLengthDays: 14 },
  defaults: { board: 'delivery' },
  snapshots: { enabled: true, maxAgeDays: 7 },
  diagnostics: [],
};

export const sampleProject: ProjectSummary = {
  key: 'ACME',
  name: 'ACME Platform',
  docsPath: 'docs',
  statuses: [
    { id: 'backlog', name: 'Backlog', category: 'todo' },
    { id: 'todo', name: 'To Do', category: 'todo' },
    { id: 'in_progress', name: 'In Progress', category: 'in_progress', wip: 3 },
    { id: 'in_review', name: 'In Review', category: 'in_progress' },
    { id: 'done', name: 'Done', category: 'done', terminal: true },
    { id: 'cancelled', name: 'Cancelled', category: 'cancelled', terminal: true },
  ],
  labels: [
    { name: 'frontend', color: '#7c3aed' },
    { name: 'security', color: '#dc2626' },
  ],
  priorities: ['critical', 'high', 'medium', 'low'],
  itemCounts: { epic: 1, story: 2, task: 1, milestone: 1, comment: 1 },
};

export const sampleItems: Item[] = [
  {
    id: 'ACME-EP-0001',
    type: 'epic',
    title: 'Single sign-on',
    status: 'in_progress',
    priority: 'high',
    labels: ['security'],
    created: '2026-08-01T09:00:00Z',
    updated: '2026-08-20T09:00:00Z',
    body: '## Description\n\nLet tenants sign in with their identity provider.\n',
    path: 'docs/.pmngr/epics/ACME-EP-0001-single-sign-on.md',
    rev: 'sha256:0000000000000001',
  },
  {
    id: 'ACME-US-0042',
    type: 'story',
    title: 'Login with SSO',
    status: 'in_progress',
    priority: 'high',
    parent: 'ACME-EP-0001',
    milestone: 'ACME-M-0001',
    assignees: ['marta'],
    labels: ['frontend', 'security'],
    estimate: 8,
    created: '2026-08-19T09:04:02Z',
    updated: '2026-09-01T10:45:12Z',
    links: [{ kind: 'blocked_by', target: 'ACME-T-0107' }],
    body: '## Description\n\nAs an employee, I want SSO.\n\n## Acceptance Criteria\n\n- [x] Button shown\n- [ ] PKCE flow\n',
    path: 'docs/.pmngr/stories/ACME-US-0042-login-with-sso.md',
    rev: 'sha256:0000000000000042',
  },
  {
    id: 'ACME-US-0043',
    type: 'story',
    title: 'Logout everywhere',
    status: 'backlog',
    priority: 'medium',
    parent: 'ACME-EP-0001',
    labels: ['security'],
    created: '2026-08-21T09:00:00Z',
    updated: '2026-08-21T09:00:00Z',
    body: '## Description\n\nRevoke all sessions.\n',
    path: 'docs/.pmngr/stories/ACME-US-0043-logout-everywhere.md',
    rev: 'sha256:0000000000000043',
  },
  {
    id: 'ACME-T-0107',
    type: 'task',
    title: 'Add OIDC client',
    status: 'todo',
    priority: 'high',
    parent: 'ACME-US-0042',
    created: '2026-08-22T09:00:00Z',
    updated: '2026-08-22T09:00:00Z',
    body: '## Description\n\nImplement the OIDC client.\n',
    path: 'docs/.pmngr/tasks/ACME-T-0107-add-oidc-client.md',
    rev: 'sha256:0000000000000107',
  },
  {
    id: 'ACME-M-0001',
    type: 'milestone',
    title: 'Public Beta',
    status: 'in_progress',
    due: '2026-11-15',
    created: '2026-06-30T10:00:00Z',
    updated: '2026-09-01T08:00:00Z',
    body: '## Description\n\nFirst public release.\n',
    path: 'docs/.pmngr/milestones/ACME-M-0001-public-beta.md',
    rev: 'sha256:0000000000000201',
  },
];

export const samplePages: KbPage[] = [
  {
    path: 'docs/index.md',
    title: 'ACME Platform',
    frontMatter: { title: 'ACME Platform', tags: ['index'] },
    body: '# ACME Platform\n\nWelcome. See [[architecture/overview]] and [[ACME-US-0042]].\n\n```mermaid\ngraph TD; A-->B;\n```\n',
    rev: 'sha256:00000000000000a1',
    outgoing: ['docs/architecture/overview.md', 'ACME-US-0042'],
    backlinks: [],
  },
  {
    path: 'docs/architecture/overview.md',
    title: 'Architecture overview',
    frontMatter: { title: 'Architecture overview' },
    body: '# Architecture overview\n\n> [!NOTE]\n> Callouts are supported.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n',
    rev: 'sha256:00000000000000a2',
    outgoing: [],
    backlinks: ['docs/index.md'],
  },
];

export const sampleComments: Comment[] = [
  {
    item: 'ACME-US-0042',
    author: 'jose',
    created: '2026-08-25T12:00:00Z',
    body: 'Northwind is the pilot tenant.',
    path: 'docs/.pmngr/comments/ACME-US-0042/20260825T120000Z-jose.md',
    rev: 'sha256:00000000000000c1',
  },
];

function matches(item: Item, f: ItemFilter): boolean {
  const list = <T>(v: T | T[] | undefined): T[] | undefined =>
    v === undefined ? undefined : Array.isArray(v) ? v : [v];
  if (!f.includeDeleted && item.deleted) return false;
  if (f.project && !item.id.startsWith(`${f.project}-`)) return false;
  const types = list(f.type);
  if (types && !types.includes(item.type)) return false;
  const statuses = list(f.status);
  if (statuses && (!item.status || !statuses.includes(item.status))) return false;
  const priorities = list(f.priority);
  if (priorities && (!item.priority || !priorities.includes(item.priority))) return false;
  if (f.assignee && !(item.assignees ?? []).includes(f.assignee)) return false;
  const labels = list(f.label);
  if (labels && !labels.some((l) => (item.labels ?? []).includes(l))) return false;
  if (f.parent && item.parent !== f.parent) return false;
  if (f.milestone && item.milestone !== f.milestone) return false;
  if (f.updatedSince && (item.updated ?? '') < f.updatedSince) return false;
  if (f.text) {
    const needle = f.text.toLowerCase();
    const hay =
      `${item.id} ${item.title} ${item.body} ${(item.labels ?? []).join(' ')}`.toLowerCase();
    if (!hay.includes(needle)) return false;
  }
  return true;
}

export class FakeProvider implements DataProvider {
  readonly kind = 'browser' as const;
  readonly capabilities: Capabilities;

  private projects: ProjectSummary[];
  private items: Map<string, Item>;
  private comments: Comment[];
  private pages: Map<string, KbPage>;
  private repos: RepoInfo[];
  private team: TeamSummary | null;
  private boards: Map<string, FakeBoard>;
  private handlers = new Set<(event: ChangeEvent) => void>();
  private revCounter = 1000;

  constructor(data: FakeData = {}, opts: { readOnly?: boolean } = {}) {
    this.capabilities = opts.readOnly ? readOnlyCapabilities : writableCapabilities;
    this.projects = data.projects ?? [sampleProject];
    this.items = new Map((data.items ?? sampleItems).map((i) => [i.id, structuredClone(i)]));
    this.comments = structuredClone(data.comments ?? sampleComments);
    this.pages = new Map((data.pages ?? samplePages).map((p) => [p.path, structuredClone(p)]));
    this.team = data.team ?? null;
    this.boards = new Map(
      (data.boards ?? [sampleBoard]).map((b) => [b.id, structuredClone(b)]),
    );
    this.repos = data.repos ?? [
      {
        id: 'repo-1',
        kind: 'project',
        name: 'acme-platform',
        location: 'acme-platform',
        docsFolder: 'docs',
        state: 'ready',
        projects: this.projects.map((p) => p.key),
      },
    ];
  }

  private nextRev(): string {
    this.revCounter += 1;
    return `sha256:${this.revCounter.toString(16).padStart(16, '0')}`;
  }

  private emit(event: ChangeEvent) {
    for (const h of this.handlers) h(event);
  }

  private assertWritable() {
    if (!this.capabilities.write) {
      throw new ProviderError('read_only', 'This workspace is read-only');
    }
  }

  listRepos(): Promise<RepoInfo[]> {
    return Promise.resolve(structuredClone(this.repos));
  }

  listProjects(): Promise<ProjectSummary[]> {
    return Promise.resolve(structuredClone(this.projects));
  }

  getTeam(): Promise<TeamSummary | null> {
    return Promise.resolve(this.team ? structuredClone(this.team) : null);
  }

  resolveRef(ref: string): Promise<RefResolution> {
    const [project = '', item = ''] = ref.split('/');
    const declared = this.team?.projects.some((p) => p.key === project) ?? false;
    const found = this.items.get(item);
    const resolution: RefResolution = {
      ref,
      project,
      item,
      declared,
      cloned: this.projects.some((p) => p.key === project),
    };
    if (found) return Promise.resolve({ ...resolution, found: structuredClone(found) });
    return Promise.resolve({
      ...resolution,
      reason: resolution.cloned
        ? `project ${project} is open but has no item ${item}`
        : `project ${project} is not cloned on this machine`,
    });
  }

  mountRepo(input: MountInput): Promise<RepoInfo> {
    const repo: RepoInfo = {
      id: `repo-${this.repos.length + 1}`,
      kind: input.kind,
      name: input.location,
      location: input.location,
      docsFolder: input.docsFolder ?? 'docs',
      state: 'ready',
      projects: [],
    };
    this.repos.push(repo);
    return Promise.resolve(structuredClone(repo));
  }

  unmountRepo(repoId: string): Promise<void> {
    this.repos = this.repos.filter((r) => r.id !== repoId);
    return Promise.resolve();
  }

  reindex(repoId: string): Promise<IndexStats> {
    const stats: IndexStats = {
      projects: this.projects.length,
      items: this.items.size,
      pages: this.pages.size,
      comments: this.comments.length,
      durationMs: 1,
      fingerprint: `fake-${this.revCounter}`,
      diagnostics: [],
    };
    this.emit({ kind: 'index', repoId, stats });
    return Promise.resolve(stats);
  }

  listItems(query: ItemFilter): Promise<ItemPage> {
    const all = [...this.items.values()].filter((i) => matches(i, query));
    const sortKey = query.sort ?? 'updated';
    const dir = query.order ?? (sortKey === 'updated' || sortKey === 'created' ? 'desc' : 'asc');
    all.sort((a, b) => {
      const av = String(a[sortKey] ?? '');
      const bv = String(b[sortKey] ?? '');
      const c = av < bv ? -1 : av > bv ? 1 : a.id.localeCompare(b.id);
      return dir === 'asc' ? c : -c;
    });
    const offset = query.cursor ? Number(query.cursor) : 0;
    const limit = query.limit ?? 50;
    const rows = all.slice(offset, offset + limit).map((i) => {
      const clone = structuredClone(i);
      if (!query.fields?.includes('body')) clone.body = '';
      return clone;
    });
    const next = offset + limit < all.length ? String(offset + limit) : undefined;
    return Promise.resolve({
      items: rows,
      total: all.length,
      ...(next ? { nextCursor: next } : {}),
    });
  }

  getItem(id: string): Promise<Item> {
    const item = this.items.get(id);
    if (!item) return Promise.reject(new ProviderError('not_found', `Item ${id} not found`));
    return Promise.resolve(structuredClone(item));
  }

  getChildren(id: string): Promise<Item[]> {
    return Promise.resolve(
      [...this.items.values()].filter((i) => i.parent === id).map((i) => structuredClone(i)),
    );
  }

  listComments(id: string): Promise<Comment[]> {
    return Promise.resolve(structuredClone(this.comments.filter((c) => c.item === id)));
  }

  listKbTree(_scope: KbScope): Promise<KbNode[]> {
    const root: KbNode[] = [];
    for (const page of this.pages.values()) {
      const parts = page.path.split('/');
      let level = root;
      for (let i = 0; i < parts.length; i += 1) {
        const name = parts[i] ?? '';
        const isLeaf = i === parts.length - 1;
        const path = parts.slice(0, i + 1).join('/');
        let node = level.find((n) => n.path === path);
        if (!node) {
          node = isLeaf
            ? { path, name, kind: 'page', title: page.title }
            : { path, name, kind: 'dir', children: [] };
          level.push(node);
        }
        if (!isLeaf) {
          node.children ??= [];
          level = node.children;
        }
      }
    }
    return Promise.resolve(root);
  }

  getPage(_scope: KbScope, path: string): Promise<KbPage> {
    const page = this.pages.get(path);
    if (!page)
      return Promise.reject(new ProviderError('not_found', `Page ${path} not found`, path));
    return Promise.resolve(structuredClone(page));
  }

  readAsset(_scope: KbScope, path: string): Promise<Blob> {
    return Promise.reject(new ProviderError('not_found', `Asset ${path} not found`, path));
  }

  search(query: SearchQuery): Promise<SearchHit[]> {
    const needle = query.text.toLowerCase();
    const hits: SearchHit[] = [];
    for (const item of this.items.values()) {
      if (`${item.id} ${item.title}`.toLowerCase().includes(needle)) {
        hits.push({
          kind: 'item',
          id: item.id,
          path: item.path,
          title: item.title,
          snippet: '',
          score: 2,
        });
      } else if (item.body.toLowerCase().includes(needle)) {
        hits.push({
          kind: 'item',
          id: item.id,
          path: item.path,
          title: item.title,
          snippet: '',
          score: 1,
        });
      }
    }
    for (const page of this.pages.values()) {
      if (`${page.title} ${page.body}`.toLowerCase().includes(needle)) {
        hits.push({ kind: 'page', path: page.path, title: page.title, snippet: '', score: 1 });
      }
    }
    hits.sort((a, b) => b.score - a.score || a.title.localeCompare(b.title));
    return Promise.resolve(hits.slice(0, query.limit ?? 20));
  }

  validateItem(): Promise<Diagnostic[]> {
    return Promise.resolve([]);
  }

  createItem(input: ItemDraft): Promise<Item> {
    this.assertWritable();
    const code = { epic: 'EP', story: 'US', task: 'T', milestone: 'M' }[input.type];
    const existing = [...this.items.keys()].filter((k) =>
      k.startsWith(`${input.project}-${code}-`),
    );
    const n = existing.length + 1;
    const id = `${input.project}-${code}-${String(n).padStart(4, '0')}`;
    const slug = input.title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '');
    const folder = { epic: 'epics', story: 'stories', task: 'tasks', milestone: 'milestones' }[
      input.type
    ];
    const now = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
    const item: Item = {
      id,
      type: input.type,
      title: input.title,
      status: input.status ?? 'backlog',
      priority: input.priority ?? 'medium',
      ...(input.parent ? { parent: input.parent } : {}),
      ...(input.milestone ? { milestone: input.milestone } : {}),
      ...(input.assignees ? { assignees: input.assignees } : {}),
      ...(input.author ? { author: input.author } : {}),
      ...(input.labels ? { labels: input.labels } : {}),
      ...(input.estimate !== undefined ? { estimate: input.estimate } : {}),
      ...(input.due ? { due: input.due } : {}),
      ...(input.links ? { links: input.links } : {}),
      ...(input.custom ? { custom: input.custom } : {}),
      created: now,
      updated: now,
      body: input.body ?? '## Description\n\n',
      path: `docs/.pmngr/${folder}/${id}-${slug}.md`,
      rev: this.nextRev(),
    };
    this.items.set(id, item);
    this.emit({ kind: 'items', repoId: 'repo-1', ids: [id] });
    return Promise.resolve(structuredClone(item));
  }

  updateItem(id: string, patch: ItemPatch, rev: string): Promise<Item> {
    this.assertWritable();
    const item = this.items.get(id);
    if (!item) return Promise.reject(new ProviderError('not_found', `Item ${id} not found`));
    if (item.rev !== rev) {
      return Promise.reject(new ProviderError('stale_revision', `Item ${id} changed on disk`));
    }
    const next: Item = { ...item, ...(patch.set ?? {}) };
    for (const key of patch.unset ?? []) delete (next as Record<string, unknown>)[key];
    if (patch.body !== undefined) next.body = patch.body;
    next.updated = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
    next.rev = this.nextRev();
    this.items.set(id, next);
    this.emit({ kind: 'items', repoId: 'repo-1', ids: [id] });
    return Promise.resolve(structuredClone(next));
  }

  moveItem(id: string, status: string, rev: string): Promise<Item> {
    return this.updateItem(id, { set: { status } }, rev);
  }

  async updateMany(ops: UpdateOp[]): Promise<BatchResult> {
    const result: BatchResult = { applied: 0, failed: [] };
    for (const op of ops) {
      try {
        await this.updateItem(op.id, op.patch, op.rev);
        result.applied += 1;
      } catch (err) {
        const code = err instanceof ProviderError ? err.code : 'internal';
        const message = err instanceof Error ? err.message : String(err);
        result.failed.push({ id: op.id, code, message });
      }
    }
    return result;
  }

  deleteItem(id: string, rev: string): Promise<void> {
    this.assertWritable();
    const item = this.items.get(id);
    if (!item) return Promise.reject(new ProviderError('not_found', `Item ${id} not found`));
    if (item.rev !== rev) {
      return Promise.reject(new ProviderError('stale_revision', `Item ${id} changed on disk`));
    }
    this.items.set(id, { ...item, deleted: true, rev: this.nextRev() });
    this.emit({ kind: 'items', repoId: 'repo-1', ids: [id] });
    return Promise.resolve();
  }

  addComment(id: string, body: string, author = 'me'): Promise<Comment> {
    this.assertWritable();
    if (!this.items.has(id)) {
      return Promise.reject(new ProviderError('not_found', `Item ${id} not found`));
    }
    const created = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
    const stamp = created.replace(/[-:]/g, '');
    const comment: Comment = {
      item: id,
      author,
      created,
      body,
      path: `docs/.pmngr/comments/${id}/${stamp}-${author}.md`,
      rev: this.nextRev(),
    };
    this.comments.push(comment);
    this.emit({ kind: 'items', repoId: 'repo-1', ids: [id] });
    return Promise.resolve(structuredClone(comment));
  }

  // -------------------------------------------------------------------- boards

  listBoards(): Promise<BoardSummary[]> {
    if (!this.team) return Promise.resolve([]);
    return Promise.resolve(
      [...this.boards.values()].map((b) => ({
        id: b.id,
        kind: b.kind,
        title: b.title,
        ...(b.description === undefined ? {} : { description: b.description }),
        path: `.pmngr/boards/${b.id}.md`,
        rev: b.rev,
        vaultId: 'repo-team',
        projects: b.projects,
        columns: b.columns.length,
        diagnostics: [],
      })),
    );
  }

  getBoard(slug: string): Promise<BoardView> {
    const board = this.boards.get(slug);
    if (!board) return Promise.reject(new ProviderError('not_found', `No board ${slug}`));
    return Promise.resolve(this.renderBoard(board));
  }

  moveCard(move: CardMove): Promise<BoardMoveResult> {
    this.assertWritable();
    const board = this.boards.get(move.board);
    if (!board) return Promise.reject(new ProviderError('not_found', `No board ${move.board}`));
    if (move.rev !== undefined && move.rev !== board.rev) {
      return Promise.reject(
        new ProviderError('stale_revision', `Board ${board.id} changed on disk`),
      );
    }
    const column = board.columns.find((c) => c.id === move.toColumn);
    if (!column) {
      return Promise.reject(
        new ProviderError('validation_failed', `Board ${board.id} has no column ${move.toColumn}`),
      );
    }

    const view = this.renderBoard(board);
    const card = view.columns.flatMap((c) => c.cards).find((c) => c.ref === move.ref);
    if (!card) {
      return Promise.reject(new ProviderError('not_found', `${move.ref} is not on this board`));
    }
    if (card.remote) {
      return Promise.reject(
        new ProviderError(
          'repo_not_cloned',
          `${card.project} is not cloned on this machine; clone it to move this card`,
        ),
      );
    }

    const from = view.columns.find((c) => c.cards.some((entry) => entry.ref === move.ref));
    const target = view.columns.find((c) => c.id === column.id);
    const used = (target?.cards.length ?? 0) + (from?.id === column.id ? 0 : 1);
    const exceeded = (column.wip ?? 0) > 0 && used > (column.wip ?? 0);
    if (exceeded && !move.force) {
      return Promise.reject(
        new ProviderError(
          'wip_limit_exceeded',
          `${column.name} is at its WIP limit of ${column.wip}; confirm the move to exceed it`,
        ),
      );
    }

    const choices = column.statuses[card.project] ?? column.statuses['*'] ?? [];
    const status = move.status ?? (from?.id === column.id ? card.status : choices[0]);
    const statusChanged = status !== undefined && status !== card.status;
    let item: Item | undefined;
    if (statusChanged && status !== undefined) {
      const existing = this.items.get(card.item);
      if (!existing) return Promise.reject(new ProviderError('not_found', `No item ${card.item}`));
      if (move.itemRev !== undefined && move.itemRev !== existing.rev) {
        return Promise.reject(
          new ProviderError('stale_revision', `Item ${card.item} changed on disk`),
        );
      }
      item = { ...existing, status, rev: this.nextRev() };
      this.items.set(card.item, item);
    }

    for (const id of Object.keys(board.order)) {
      board.order[id] = (board.order[id] ?? []).filter((ref) => ref !== move.ref);
    }
    const list = board.order[column.id] ?? [];
    const at = move.position < 0 || move.position > list.length ? list.length : move.position;
    board.order[column.id] = [...list.slice(0, at), move.ref, ...list.slice(at)];
    board.rev = this.nextRev();

    this.emit({ kind: 'repo', repoId: 'repo-team' });
    if (item) this.emit({ kind: 'items', repoId: 'repo-1', ids: [item.id] });

    return Promise.resolve({
      board: this.renderBoard(board),
      ...(item ? { item: structuredClone(item) } : {}),
      move: {
        ref: move.ref,
        ...(from ? { fromColumn: from.id } : {}),
        toColumn: column.id,
        ...(status === undefined ? {} : { status }),
        statusChanged,
        choices,
        wip: { column: column.id, used, limit: column.wip ?? 0, exceeded },
      },
      writes: [],
    });
  }

  /**
   * Renders a board the way `core.BuildBoardView` does: filters first, then one
   * column per status mapping, then the `order` list, then everything else by
   * priority. A ref into a project the workspace has not opened stays where the
   * board puts it and is marked remote.
   */
  private renderBoard(board: FakeBoard): BoardView {
    const declared = this.team?.projects.map((p) => p.key) ?? board.projects;
    const cloned = new Set(this.projects.map((p) => p.key));
    const rank: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 };

    const placed = new Set<string>();
    const columns: BoardColumnView[] = board.columns.map((column) => {
      const cards: BoardCard[] = [];
      for (const item of this.items.values()) {
        if (item.deleted) continue;
        const project = item.id.split('-')[0] ?? '';
        if (!cloned.has(project) || !board.projects.includes(project)) continue;
        const types = board.filters?.types;
        if (types && !types.includes(item.type)) continue;
        const mapped = column.statuses[project] ?? column.statuses['*'] ?? [];
        if (!item.status || !mapped.includes(item.status)) continue;
        const ref = `${project}/${item.id}`;
        cards.push({
          ref,
          project,
          item: item.id,
          declared: declared.includes(project),
          remote: false,
          vaultId: 'repo-1',
          title: item.title,
          type: item.type,
          ...(item.status === undefined ? {} : { status: item.status }),
          ...(item.priority === undefined ? {} : { priority: item.priority }),
          ...(item.assignees ? { assignees: item.assignees } : {}),
          ...(item.labels ? { labels: item.labels } : {}),
          ...(item.estimate === undefined ? {} : { estimate: item.estimate }),
          ...(item.updated === undefined ? {} : { updated: item.updated }),
          path: item.path,
          rev: item.rev,
        });
        placed.add(ref);
      }
      for (const ref of board.order[column.id] ?? []) {
        if (placed.has(ref)) continue;
        const [project = '', id = ''] = ref.split('/');
        if (cloned.has(project)) continue;
        cards.push({
          ref,
          project,
          item: id,
          declared: declared.includes(project),
          remote: true,
          reason: `project ${project} is not cloned on this machine; clone it to move this card`,
        });
        placed.add(ref);
      }
      const order = board.order[column.id] ?? [];
      cards.sort((a, b) => {
        const ia = order.indexOf(a.ref);
        const ib = order.indexOf(b.ref);
        if (ia >= 0 && ib >= 0) return ia - ib;
        if (ia >= 0) return -1;
        if (ib >= 0) return 1;
        return (
          (rank[a.priority ?? 'low'] ?? 3) - (rank[b.priority ?? 'low'] ?? 3) ||
          a.ref.localeCompare(b.ref)
        );
      });
      return {
        id: column.id,
        name: column.name,
        ...(column.wip === undefined ? {} : { wip: column.wip }),
        ...(column.color === undefined ? {} : { color: column.color }),
        cards,
        exceeded: (column.wip ?? 0) > 0 && cards.length > (column.wip ?? 0),
      };
    });

    const unmapped: BoardCard[] = [];
    for (const item of this.items.values()) {
      if (item.deleted) continue;
      const project = item.id.split('-')[0] ?? '';
      if (!cloned.has(project) || !board.projects.includes(project)) continue;
      const types = board.filters?.types;
      if (types && !types.includes(item.type)) continue;
      const ref = `${project}/${item.id}`;
      if (placed.has(ref)) continue;
      unmapped.push({
        ref,
        project,
        item: item.id,
        declared: true,
        remote: false,
        title: item.title,
        type: item.type,
        ...(item.status === undefined ? {} : { status: item.status }),
        reason: `status ${item.status ?? ''} maps to no column of this board`,
      });
    }

    return {
      id: board.id,
      kind: board.kind,
      title: board.title,
      ...(board.description === undefined ? {} : { description: board.description }),
      path: `.pmngr/boards/${board.id}.md`,
      rev: board.rev,
      teamVaultId: 'repo-team',
      projects: board.projects,
      filters: board.filters ?? {},
      swimlanes: {},
      card: {},
      columns,
      unmapped,
      diagnostics: [],
    };
  }

  writePage(_scope: KbScope, path: string, content: string, rev?: string): Promise<KbPage> {
    this.assertWritable();
    const existing = this.pages.get(path);
    if (existing && rev !== undefined && existing.rev !== rev) {
      return Promise.reject(
        new ProviderError('stale_revision', `Page ${path} changed on disk`, path),
      );
    }
    const title = /^#\s+(.+)$/m.exec(content)?.[1] ?? path;
    const page: KbPage = {
      path,
      title,
      frontMatter: existing?.frontMatter ?? {},
      body: content,
      rev: this.nextRev(),
      outgoing: existing?.outgoing ?? [],
      backlinks: existing?.backlinks ?? [],
    };
    this.pages.set(path, page);
    this.emit({ kind: 'kb', repoId: 'repo-1', paths: [path] });
    return Promise.resolve(structuredClone(page));
  }

  subscribe(handler: (event: ChangeEvent) => void): Unsubscribe {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }
}
