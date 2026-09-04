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
  BoardColumnPatch,
  BoardMoveResult,
  BoardPatch,
  BoardSummary,
  BoardView,
  CardMove,
  Capabilities,
  ChangeEvent,
  Comment,
  ConflictAnalysis,
  ConflictResolution,
  ConflictResolveResult,
  DataProvider,
  Diagnostic,
  GitCommit,
  GitRepoStatus,
  GitSettings,
  GitSettingsPatch,
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
  SnapshotInfo,
  SnapshotItemSummary,
  SnapshotRefresh,
  SnapshotResult,
  RetroAction,
  RetroActionView,
  RetroCategory,
  RetroDraft,
  RetroFilter,
  RetroListing,
  RetroNote,
  RetroPatch,
  RetroPromotion,
  RetroResult,
  RetroState,
  RetroSummary,
  RetroThemeView,
  RetroView,
  SprintCarry,
  SprintCarryResult,
  SprintDraft,
  SprintFilter,
  SprintPatch,
  SprintResult,
  SprintState,
  SprintSummary,
  SprintView,
  StatusCategory,
  SyncOptions,
  SyncRepoStatus,
  SyncResult,
  SyncSettings,
  SyncSettingsPatch,
  SyncStatus,
  TeamSummary,
  Unsubscribe,
  UpdateOp,
} from '@/api/provider';
import { ProviderError, readOnlyCapabilities } from '@/api/provider';
import { DEFAULT_COMMIT_TEMPLATE, validateCommitTemplate } from '@/git/message';

export type FakeData = {
  projects?: ProjectSummary[];
  items?: Item[];
  comments?: Comment[];
  pages?: KbPage[];
  repos?: RepoInfo[];
  /** The team repository of the workspace; omit for a workspace without one. */
  team?: TeamSummary | null;
  /** The boards of the team repository; omit for the sample boards. */
  boards?: FakeBoard[];
  /** The sprints of the team repository; omit for the sample sprint. */
  sprints?: FakeSprint[];
  /** The retros of the team repository; omit for the sample retro. */
  retros?: FakeRetro[];
  /** The day the sprint header counts its remaining days from. */
  today?: string;
};

/**
 * A retro as the team repository stores it (docs/04 §9.2): the notes in the
 * body, the themes, the votes and the improvement actions in the front matter.
 * The fake grades an action the way the Go core does — a promoted action is
 * done when its task is done — so a component test exercises R-RETRO-1.
 */
export type FakeRetro = {
  id: string;
  title: string;
  sprint?: string;
  board?: string;
  date: string;
  facilitator?: string;
  participants: string[];
  state: RetroState;
  anonymous?: boolean;
  votesPerPerson?: number;
  carriedFrom?: string;
  notes: RetroNote[];
  themes: { id: string; title: string; category?: RetroCategory; notes?: string[] }[];
  votes: Record<string, string[]>;
  actions: RetroAction[];
  rev: string;
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
  /** Scrum only: the sprint the board is scoped to (docs/04 §5.5). */
  sprint?: string;
  /** Scrum only: the column that offers the sprint candidates. */
  backlogColumn?: string;
  rev: string;
};

/**
 * A sprint as the team repository stores it (docs/04 §8.2): a goal, a date
 * range, a state and one `<projectKey>/<itemId>` reference per line.
 */
export type FakeSprint = {
  id: string;
  title: string;
  board: string;
  state: SprintState;
  start: string;
  end: string;
  goal?: string;
  items: string[];
  committed?: string[];
  capacityHours?: number;
  velocityTarget?: number;
  participants?: string[];
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

/** The scrum board of `sampleTeam`, scoped to `sampleSprint`. */
export const sampleScrumBoard: FakeBoard = {
  id: 'acme-scrum',
  kind: 'scrum',
  title: 'SSO Sprint Board',
  description: 'The sprint the squad is running.',
  projects: ['ACME', 'WEB'],
  sprint: 'ACME-TEAM-S-0007',
  backlogColumn: 'sprint_backlog',
  columns: [
    { id: 'sprint_backlog', name: 'Sprint Backlog', statuses: { '*': ['backlog', 'todo'] } },
    { id: 'in_progress', name: 'In Progress', statuses: { '*': ['in_progress'] }, wip: 2 },
    { id: 'in_review', name: 'In Review', statuses: { '*': ['in_review'] }, wip: 2 },
    { id: 'done', name: 'Done', statuses: { '*': ['done', 'cancelled'] } },
  ],
  filters: { types: ['story', 'task'] },
  order: { in_progress: ['ACME/ACME-US-0042'] },
  rev: 'sha256:00000000000000b2',
};

/** The sprint `sampleScrumBoard` runs: one cloned item and one remote one. */
export const sampleSprint: FakeSprint = {
  id: 'ACME-TEAM-S-0007',
  title: 'Sprint 7 — SSO end to end',
  board: 'acme-scrum',
  state: 'active',
  start: '2026-08-24',
  end: '2026-09-06',
  goal: 'A tenant can log in with their identity provider in staging.',
  items: ['ACME/ACME-US-0042', 'WEB/WEB-US-0031'],
  committed: ['ACME/ACME-US-0042', 'WEB/WEB-US-0031'],
  capacityHours: 260,
  velocityTarget: 21,
  participants: ['marta', 'jose'],
  rev: 'sha256:00000000000000c1',
};

/** The retro `sampleSprint` produced: one promoted action and one process one. */
export const sampleRetro: FakeRetro = {
  id: 'ACME-TEAM-R-0007',
  title: 'Sprint 7 Retrospective',
  sprint: 'ACME-TEAM-S-0007',
  board: 'acme-scrum',
  date: '2026-09-08',
  facilitator: 'marta',
  participants: ['marta', 'jose'],
  state: 'closed',
  votesPerPerson: 3,
  notes: [
    {
      id: 'n1',
      category: 'went_well',
      text: 'Pairing on the OIDC flow unblocked us.',
      author: 'jose',
    },
    {
      id: 'n2',
      category: 'to_improve',
      text: 'Two days lost to a trailing slash.',
      author: 'marta',
    },
    { id: 'n3', category: 'puzzle', text: 'Is the stale snapshot badge useful?', author: 'jose' },
  ],
  themes: [
    { id: 't1', title: 'Pairing paid off', category: 'went_well', notes: ['n1'] },
    { id: 't2', title: 'Configuration bites us', category: 'to_improve', notes: ['n2'] },
  ],
  votes: { t1: ['jose'], t2: ['jose', 'marta'] },
  actions: [
    {
      id: 'a1',
      title: 'Assert the OIDC redirect URI at startup',
      owner: 'jose',
      due: '2026-09-12',
      theme: 't2',
      task: 'ACME/ACME-T-0107',
      status: 'promoted',
    },
    {
      id: 'a2',
      title: 'Split Monday planning into two slots',
      owner: 'marta',
      due: '2026-09-08',
      theme: 't2',
      status: 'done',
      note: 'Team process change; nothing to build.',
    },
    { id: 'a3', title: 'Write the staging runbook', due: '2026-09-20', status: 'proposed' },
  ],
  rev: 'sha256:00000000000000d1',
};

/**
 * The coarse bucket of a status. The fake knows the sample workflow, which is
 * what lets it tell finished work from open work the way the core does.
 */
function categoryOf(status: string | undefined): StatusCategory {
  switch (status) {
    case 'done':
      return 'done';
    case 'cancelled':
      return 'cancelled';
    case 'in_progress':
    case 'in_review':
    case 'doing':
    case 'review':
      return 'in_progress';
    default:
      return 'todo';
  }
}

/** A card sits in a terminal status of its own project. */
function isDone(card: BoardCard): boolean {
  const category = card.category ?? categoryOf(card.status);
  return category === 'done' || category === 'cancelled';
}

const writableCapabilities: Capabilities = {
  ...readOnlyCapabilities,
  write: true,
  maxBatchWrite: 50,
};

/**
 * The committed index snapshot of the project nobody cloned: what a remote card
 * renders from (docs/04 §6). `generated` is a day old, so the card is dated but
 * not stale.
 */
export const sampleRemoteItems: SnapshotItemSummary[] = [
  {
    id: 'WEB-US-0031',
    type: 'story',
    title: 'Rewrite the hero section',
    status: 'in_progress',
    category: 'in_progress',
    priority: 'high',
    assignees: ['marta'],
    labels: ['frontend'],
    estimate: 5,
    updated: '2026-09-01T08:30:00Z',
    path: 'documentation/.pmngr/stories/WEB-US-0031-rewrite-the-hero-section.md',
    rev: 'sha256:00000000000000c1',
  },
];

/** The state of that snapshot, as the team surface reports it. */
export const sampleSnapshotInfo: SnapshotInfo = {
  project: 'WEB',
  path: '.pmngr/index/WEB.json',
  present: true,
  enabled: true,
  generated: '2026-09-03T06:00:00Z',
  generatedBy: 'marta',
  generator: 'gintrack-core',
  items: sampleRemoteItems.length,
  ageSeconds: 30 * 3600,
  freshness: 'ageing',
  stale: false,
};

/**
 * One card of a project nobody cloned, read from the committed snapshot: the
 * fields the snapshot published, the age of the file and a link to the item on
 * the git host, never an editable card (docs/04 §7).
 */
function remoteCard(ref: string, project: string, id: string, declared: boolean): BoardCard {
  const entry = sampleRemoteItems.find((item) => item.id === id);
  if (!entry) {
    return {
      ref,
      project,
      item: id,
      declared,
      remote: true,
      reason: `project ${project} is not cloned on this machine and has no index snapshot yet; clone it to move this card`,
    };
  }
  return {
    ref,
    project,
    item: id,
    declared,
    remote: true,
    source: 'snapshot',
    snapshotAt: sampleSnapshotInfo.generated ?? '',
    stale: sampleSnapshotInfo.stale,
    remoteUrl: `https://gitlab.com/acme/website/-/blob/main/${entry.path}`,
    title: entry.title,
    type: entry.type,
    ...(entry.status === undefined ? {} : { status: entry.status }),
    ...(entry.priority === undefined ? {} : { priority: entry.priority }),
    ...(entry.assignees ? { assignees: entry.assignees } : {}),
    ...(entry.labels ? { labels: entry.labels } : {}),
    ...(entry.estimate === undefined ? {} : { estimate: entry.estimate }),
    ...(entry.updated === undefined ? {} : { updated: entry.updated }),
    path: entry.path,
    rev: entry.rev,
    reason: `${project} is not cloned on this machine: this card is read from the index snapshot of 1 day ago and cannot be edited here`,
  };
}

/** A project whose snapshot has never been generated. */
const missingSnapshot = (project: string): SnapshotInfo => ({
  project,
  path: `.pmngr/index/${project}.json`,
  present: false,
  enabled: true,
  items: 0,
  freshness: 'unknown',
  stale: false,
});

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
      snapshot: missingSnapshot('ACME'),
      browseUrl: 'https://github.com/acme/platform',
    },
    {
      key: 'WEB',
      name: 'Marketing Website',
      repo: 'https://gitlab.com/acme/website.git',
      docsPath: 'documentation',
      host: 'gitlab',
      webUrl: 'https://gitlab.com/acme/website',
      cloned: false,
      snapshot: sampleSnapshotInfo,
      browseUrl: 'https://gitlab.com/acme/website',
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
  private sprints: Map<string, FakeSprint>;
  private retros: Map<string, FakeRetro>;
  private today: string;
  private handlers = new Set<(event: ChangeEvent) => void>();
  private revCounter = 1000;
  /** Commit-on-save settings, in memory (story GIT-US-0020). */
  private git: GitSettings;

  constructor(data: FakeData = {}, opts: { readOnly?: boolean } = {}) {
    this.capabilities = opts.readOnly ? readOnlyCapabilities : writableCapabilities;
    this.projects = data.projects ?? [sampleProject];
    this.items = new Map((data.items ?? sampleItems).map((i) => [i.id, structuredClone(i)]));
    this.comments = structuredClone(data.comments ?? sampleComments);
    this.pages = new Map((data.pages ?? samplePages).map((p) => [p.path, structuredClone(p)]));
    this.team = data.team ?? null;
    this.boards = new Map(
      (data.boards ?? [sampleBoard, sampleScrumBoard]).map((b) => [b.id, structuredClone(b)]),
    );
    this.sprints = new Map((data.sprints ?? [sampleSprint]).map((s) => [s.id, structuredClone(s)]));
    this.retros = new Map((data.retros ?? [sampleRetro]).map((r) => [r.id, structuredClone(r)]));
    this.today = data.today ?? '2026-09-02';
    this.git = {
      commitOnSave: false,
      commitDebounceMs: 2000,
      messageTemplate: DEFAULT_COMMIT_TEMPLATE,
      backend: 'auto',
      resolvedBackend: 'go-git',
      signCommits: false,
      pending: 0,
      supported: !opts.readOnly,
      ...(opts.readOnly ? { reason: 'This vault is read-only.' } : {}),
    };
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
        ...(b.sprint === undefined ? {} : { sprint: b.sprint }),
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

  updateBoard(slug: string, patch: BoardPatch, rev?: string): Promise<BoardView> {
    this.assertWritable();
    const board = this.boards.get(slug);
    if (!board) return Promise.reject(new ProviderError('not_found', `No board ${slug}`));
    if (rev !== undefined && rev !== '*' && rev !== board.rev) {
      return Promise.reject(new ProviderError('stale_revision', `Board ${slug} changed on disk`));
    }
    if (patch.sprint !== undefined) {
      if (board.kind !== 'scrum') {
        return Promise.reject(
          new ProviderError('validation_failed', 'a kanban board cannot be scoped to a sprint'),
        );
      }
      if (patch.sprint !== '' && !this.sprints.has(patch.sprint)) {
        return Promise.reject(new ProviderError('not_found', `No sprint ${patch.sprint}`));
      }
      board.sprint = patch.sprint;
    }
    if (patch.title !== undefined) board.title = patch.title;
    if (patch.description !== undefined) board.description = patch.description;
    if (patch.backlogColumn !== undefined) board.backlogColumn = patch.backlogColumn;
    if (patch.columns !== undefined) {
      board.columns = patch.columns.map((column: BoardColumnPatch) => ({
        id: column.id,
        name: column.name ?? column.id,
        statuses: column.statuses ?? {},
        ...(column.wip === undefined ? {} : { wip: column.wip }),
        ...(column.color === undefined ? {} : { color: column.color }),
      }));
    }
    board.rev = this.nextRev();
    this.emit({ kind: 'repo', repoId: 'repo-team' });
    return Promise.resolve(this.renderBoard(board));
  }

  // ------------------------------------------------------------------- retros

  listRetros(filter: RetroFilter = {}): Promise<RetroListing> {
    const rows = [...this.retros.values()]
      .filter((r) => (filter.sprint ? r.sprint === filter.sprint : true))
      .filter((r) => (filter.board ? r.board === filter.board : true))
      .filter((r) => (filter.state ? r.state === filter.state : true))
      .sort((a, b) =>
        a.date === b.date ? b.id.localeCompare(a.id) : b.date.localeCompare(a.date),
      );
    return Promise.resolve({
      retros: rows.map((r) => this.summarizeRetro(r)),
      carried: rows.flatMap((r) => this.retroActions(r)).filter((a) => a.open),
      diagnostics: [],
    });
  }

  getRetro(id: string): Promise<RetroView> {
    const retro = this.retros.get(id);
    if (!retro) return Promise.reject(new ProviderError('not_found', `No retro ${id}`));
    return Promise.resolve(this.renderRetro(retro));
  }

  createRetro(input: RetroDraft): Promise<RetroResult> {
    if (!this.capabilities.write)
      return Promise.reject(new ProviderError('read_only', 'This workspace is read-only'));
    const sprint = input.sprint ? this.sprints.get(input.sprint) : undefined;
    if (input.sprint && !sprint) {
      return Promise.reject(new ProviderError('not_found', `No sprint ${input.sprint}`));
    }
    const taken = [...this.retros.values()].find((r) => input.sprint && r.sprint === input.sprint);
    if (taken) {
      return Promise.reject(
        new ProviderError('conflict', `sprint ${input.sprint} already has retro ${taken.id}`),
      );
    }
    const numbers = [...this.retros.keys()].map((id) => Number(id.split('-').pop() ?? 0));
    const next = Math.max(0, ...numbers) + 1;
    const previous = [...this.retros.values()].at(-1);
    const retro: FakeRetro = {
      id: `ACME-TEAM-R-${String(next).padStart(4, '0')}`,
      title: input.title ?? `${sprint?.title ?? 'Retrospective'} Retrospective`,
      ...(input.sprint === undefined ? {} : { sprint: input.sprint }),
      ...((input.board ?? sprint?.board)
        ? { board: (input.board ?? sprint?.board) as string }
        : {}),
      date: input.date ?? this.today,
      ...(input.facilitator === undefined ? {} : { facilitator: input.facilitator }),
      participants: input.participants ?? sprint?.participants ?? [],
      state: input.state ?? 'collecting',
      ...(input.anonymous === undefined ? {} : { anonymous: input.anonymous }),
      ...(input.votesPerPerson === undefined ? {} : { votesPerPerson: input.votesPerPerson }),
      ...((input.carriedFrom ?? previous?.id)
        ? { carriedFrom: (input.carriedFrom ?? previous?.id) as string }
        : {}),
      notes: [],
      themes: [],
      votes: {},
      actions: [],
      rev: this.nextRev(),
    };
    this.retros.set(retro.id, retro);
    this.emit({ kind: 'repo', repoId: 'repo-1' });
    return Promise.resolve({ retro: this.renderRetro(retro), writes: [] });
  }

  updateRetro(id: string, patch: RetroPatch, rev?: string): Promise<RetroResult> {
    if (!this.capabilities.write)
      return Promise.reject(new ProviderError('read_only', 'This workspace is read-only'));
    const retro = this.retros.get(id);
    if (!retro) return Promise.reject(new ProviderError('not_found', `No retro ${id}`));
    if (rev !== undefined && rev !== '*' && rev !== retro.rev) {
      return Promise.reject(new ProviderError('stale_revision', `Retro ${id} changed on disk`));
    }
    if (patch.title !== undefined) retro.title = patch.title;
    if (patch.date !== undefined) retro.date = patch.date;
    if (patch.state !== undefined) retro.state = patch.state;
    if (patch.facilitator !== undefined) retro.facilitator = patch.facilitator;
    if (patch.participants !== undefined) retro.participants = [...patch.participants];
    if (patch.anonymous !== undefined) retro.anonymous = patch.anonymous;
    if (patch.votesPerPerson !== undefined) retro.votesPerPerson = patch.votesPerPerson;
    if (patch.carriedFrom !== undefined) retro.carriedFrom = patch.carriedFrom;
    for (const draft of patch.addNotes ?? []) {
      retro.notes.push({
        id: `n${retro.notes.length + 1}`,
        category: draft.category,
        text: draft.text,
        ...(draft.author === undefined || retro.anonymous ? {} : { author: draft.author }),
      });
    }
    for (const edit of patch.updateNotes ?? []) {
      const note = retro.notes.find((n) => n.id === edit.id);
      if (!note) return Promise.reject(new ProviderError('not_found', `No note ${edit.id}`));
      if (edit.text !== undefined) note.text = edit.text;
      if (edit.author !== undefined) note.author = edit.author;
      if (edit.category !== undefined) note.category = edit.category;
    }
    if (patch.removeNotes) {
      retro.notes = retro.notes.filter((n) => !patch.removeNotes?.includes(n.id ?? ''));
    }
    if (patch.themes !== undefined) retro.themes = structuredClone(patch.themes);
    if (patch.votes !== undefined) retro.votes = structuredClone(patch.votes);
    for (const draft of patch.addActions ?? []) {
      retro.actions.push({
        id: draft.id ?? `a${retro.actions.length + 1}`,
        title: draft.title,
        ...(draft.owner === undefined ? {} : { owner: draft.owner }),
        ...(draft.due === undefined ? {} : { due: draft.due }),
        ...(draft.theme === undefined ? {} : { theme: draft.theme }),
        ...(draft.note === undefined ? {} : { note: draft.note }),
        status: 'proposed',
      });
    }
    for (const edit of patch.updateActions ?? []) {
      const action = retro.actions.find((a) => a.id === edit.id);
      if (!action) return Promise.reject(new ProviderError('not_found', `No action ${edit.id}`));
      if (edit.title !== undefined) action.title = edit.title;
      if (edit.owner !== undefined) action.owner = edit.owner;
      if (edit.due !== undefined) action.due = edit.due;
      if (edit.theme !== undefined) action.theme = edit.theme;
      if (edit.note !== undefined) action.note = edit.note;
      if (edit.status !== undefined) action.status = edit.status;
    }
    if (patch.removeActions) {
      retro.actions = retro.actions.filter((a) => !patch.removeActions?.includes(a.id));
    }
    retro.rev = this.nextRev();
    this.emit({ kind: 'repo', repoId: 'repo-1' });
    return Promise.resolve({ retro: this.renderRetro(retro), writes: [] });
  }

  promoteRetroAction(input: RetroPromotion): Promise<RetroResult> {
    if (!this.capabilities.write)
      return Promise.reject(new ProviderError('read_only', 'This workspace is read-only'));
    const retro = this.retros.get(input.retro);
    if (!retro) return Promise.reject(new ProviderError('not_found', `No retro ${input.retro}`));
    if (input.rev !== undefined && input.rev !== '*' && input.rev !== retro.rev) {
      return Promise.reject(
        new ProviderError('stale_revision', `Retro ${retro.id} changed on disk`),
      );
    }
    const action = retro.actions.find((a) => a.id === input.action);
    if (!action) return Promise.reject(new ProviderError('not_found', `No action ${input.action}`));
    if (action.task) {
      return Promise.reject(
        new ProviderError(
          'retro_action_promoted',
          `action ${action.id} is already promoted to ${action.task}`,
        ),
      );
    }
    if (!this.projects.some((p) => p.key === input.project)) {
      return Promise.reject(
        new ProviderError(
          'repo_not_cloned',
          `project ${input.project} is not cloned on this machine; clone it, or copy the action as Markdown`,
        ),
      );
    }
    const numbers = [...this.items.values()]
      .filter((i) => i.type === 'task')
      .map((i) => Number(i.id.split('-').pop() ?? 0));
    const id = `${input.project}-T-${String(Math.max(0, ...numbers) + 1).padStart(4, '0')}`;
    const task: Item = {
      id,
      type: 'task',
      title: action.title,
      status: 'todo',
      ...(action.owner === undefined ? {} : { assignees: [action.owner] }),
      ...(retro.facilitator === undefined ? {} : { author: retro.facilitator }),
      labels: input.labels ?? ['retro'],
      ...(action.due === undefined ? {} : { due: action.due }),
      body: `## Description\n\n${action.note ? `${action.note}\n\n` : ''}Promoted from retro ${retro.id} (action ${action.id}).\n`,
      path: `docs/.pmngr/tasks/${id.toLowerCase()}.md`,
      rev: this.nextRev(),
    };
    this.items.set(task.id, task);
    action.task = `${input.project}/${task.id}`;
    action.status = 'promoted';
    retro.rev = this.nextRev();
    this.emit({ kind: 'items', repoId: 'repo-1', ids: [task.id] });
    return Promise.resolve({ retro: this.renderRetro(retro), task, writes: [] });
  }

  /** Renders a retro the way `core.BuildRetroView` does. */
  private renderRetro(retro: FakeRetro): RetroView {
    const actions = this.retroActions(retro);
    const themes: RetroThemeView[] = retro.themes
      .map((theme) => ({
        ...theme,
        votes: (retro.votes[theme.id] ?? []).length,
        voters: [...(retro.votes[theme.id] ?? [])].sort(),
        noteTexts: (theme.notes ?? [])
          .map((id) => retro.notes.find((n) => n.id === id))
          .filter((n): n is RetroNote => n !== undefined),
        actions: retro.actions.filter((a) => a.theme === theme.id).map((a) => a.id),
      }))
      .sort((a, b) => (a.votes === b.votes ? a.id.localeCompare(b.id) : b.votes - a.votes));
    const sprintOfRetro = retro.sprint ? this.sprints.get(retro.sprint) : undefined;
    const earlier = [...this.retros.values()].filter(
      (r) => r.id !== retro.id && (!retro.carriedFrom || r.id === retro.carriedFrom),
    );
    return {
      retro: this.summarizeRetro(retro),
      notes: structuredClone(retro.notes),
      themes,
      actions,
      carried: earlier.flatMap((r) => this.retroActions(r)).filter((a) => a.open),
      ...(sprintOfRetro ? { sprint: this.renderSprint(sprintOfRetro).sprint } : {}),
      diagnostics: [],
    };
  }

  /** The header and the follow-through counts of one retro. */
  private summarizeRetro(retro: FakeRetro): RetroSummary {
    const actions = this.retroActions(retro);
    return {
      id: retro.id,
      title: retro.title,
      ...(retro.sprint === undefined ? {} : { sprint: retro.sprint }),
      ...(retro.board === undefined ? {} : { board: retro.board }),
      date: retro.date,
      ...(retro.facilitator === undefined ? {} : { facilitator: retro.facilitator }),
      participants: [...retro.participants],
      state: retro.state,
      ...(retro.anonymous === undefined ? {} : { anonymous: retro.anonymous }),
      voteBudget: retro.votesPerPerson ?? 3,
      ...(retro.carriedFrom === undefined ? {} : { carriedFrom: retro.carriedFrom }),
      notes: retro.notes.length,
      themes: retro.themes.length,
      metrics: {
        actions: retro.actions.length,
        promoted: actions.filter((a) => a.task).length,
        done: actions.filter((a) => a.done && a.status !== 'dropped').length,
        open: actions.filter((a) => a.open).length,
        dropped: actions.filter((a) => a.status === 'dropped').length,
        noOwner: actions.filter((a) => !a.owner).length,
      },
      actions: structuredClone(retro.actions),
      path: `.pmngr/retros/${retro.id}.md`,
      rev: retro.rev,
    };
  }

  /**
   * Grades every improvement action against the task it was promoted into: the
   * task's status decides, and the retro's own `status` is only the fallback
   * for an action that was never promoted (docs/04 R-RETRO-1).
   */
  private retroActions(retro: FakeRetro): RetroActionView[] {
    return retro.actions.map((action) => {
      const card = action.task ? this.refCard(action.task) : undefined;
      const done =
        action.status === 'dropped'
          ? false
          : card && card.category
            ? card.category === 'done' || card.category === 'cancelled'
            : action.status === 'done';
      return {
        ...action,
        retro: retro.id,
        retroTitle: retro.title,
        ...(card ? { card } : {}),
        ...(card?.reason ? { reason: card.reason } : {}),
        done,
        open: !done && action.status !== 'dropped',
      };
    });
  }

  // ------------------------------------------------------------------- sprints

  listSprints(filter: SprintFilter = {}): Promise<SprintSummary[]> {
    if (!this.team) return Promise.resolve([]);
    const rows = [...this.sprints.values()]
      .filter((s) => (filter.board ? s.board === filter.board : true))
      .filter((s) => (filter.state ? s.state === filter.state : true))
      .sort((a, b) => a.id.localeCompare(b.id))
      .map((s) => this.renderSprint(s).sprint);
    return Promise.resolve(rows);
  }

  getSprint(id: string): Promise<SprintView> {
    const sprint = this.sprints.get(id);
    if (!sprint) return Promise.reject(new ProviderError('not_found', `No sprint ${id}`));
    return Promise.resolve(this.renderSprint(sprint));
  }

  createSprint(input: SprintDraft): Promise<SprintResult> {
    this.assertWritable();
    if (!this.boards.has(input.board)) {
      return Promise.reject(new ProviderError('not_found', `No board ${input.board}`));
    }
    const overlap = [...this.sprints.values()].find(
      (s) => s.board === input.board && s.start <= input.end && input.start <= s.end,
    );
    if (overlap) {
      return Promise.reject(
        new ProviderError(
          'sprint_overlap',
          `${input.start} to ${input.end} overlaps sprint ${overlap.id} (${overlap.start} to ${overlap.end}) on board ${input.board}; sprints on one board cannot share a day`,
        ),
      );
    }
    const numbers = [...this.sprints.keys()].map((id) => Number(id.split('-').pop() ?? 0));
    const next = (numbers.length > 0 ? Math.max(...numbers) : 0) + 1;
    const sprint: FakeSprint = {
      id: `ACME-TEAM-S-${String(next).padStart(4, '0')}`,
      title: input.title ?? `Sprint ${next}`,
      board: input.board,
      state: input.state ?? 'planned',
      start: input.start,
      end: input.end,
      ...(input.goal === undefined ? {} : { goal: input.goal }),
      items: [...(input.items ?? [])],
      rev: this.nextRev(),
    };
    this.sprints.set(sprint.id, sprint);
    this.emit({ kind: 'repo', repoId: 'repo-team' });
    return Promise.resolve({ sprint: this.renderSprint(sprint), writes: [] });
  }

  updateSprint(id: string, patch: SprintPatch, rev?: string): Promise<SprintResult> {
    this.assertWritable();
    const sprint = this.sprints.get(id);
    if (!sprint) return Promise.reject(new ProviderError('not_found', `No sprint ${id}`));
    if (rev !== undefined && rev !== '*' && rev !== sprint.rev) {
      return Promise.reject(new ProviderError('stale_revision', `Sprint ${id} changed on disk`));
    }
    if (patch.title !== undefined) sprint.title = patch.title;
    if (patch.goal !== undefined) sprint.goal = patch.goal;
    if (patch.start !== undefined) sprint.start = patch.start;
    if (patch.end !== undefined) sprint.end = patch.end;
    if (patch.state !== undefined) sprint.state = patch.state;
    if (patch.capacityHours !== undefined) sprint.capacityHours = patch.capacityHours;
    if (patch.velocityTarget !== undefined) sprint.velocityTarget = patch.velocityTarget;
    if (patch.participants !== undefined) sprint.participants = [...patch.participants];
    if (patch.items !== undefined) sprint.items = [...patch.items];
    for (const ref of patch.addItems ?? []) {
      if (!sprint.items.includes(ref)) sprint.items.push(ref);
    }
    if (patch.removeItems?.length) {
      sprint.items = sprint.items.filter((ref) => !patch.removeItems?.includes(ref));
    }
    sprint.rev = this.nextRev();
    this.emit({ kind: 'repo', repoId: 'repo-team' });
    return Promise.resolve({ sprint: this.renderSprint(sprint), writes: [] });
  }

  startSprint(id: string, rev?: string, force?: boolean): Promise<SprintResult> {
    this.assertWritable();
    const sprint = this.sprints.get(id);
    if (!sprint) return Promise.reject(new ProviderError('not_found', `No sprint ${id}`));
    if (rev !== undefined && rev !== '*' && rev !== sprint.rev) {
      return Promise.reject(new ProviderError('stale_revision', `Sprint ${id} changed on disk`));
    }
    const running = [...this.sprints.values()].find(
      (s) => s.id !== id && s.board === sprint.board && s.state === 'active',
    );
    if (running && !force) {
      return Promise.reject(
        new ProviderError(
          'sprint_already_active',
          `board ${sprint.board} is already running sprint ${running.id}; close it first, or confirm to run two at once`,
        ),
      );
    }
    sprint.state = 'active';
    sprint.committed = [...sprint.items];
    sprint.rev = this.nextRev();
    const board = this.boards.get(sprint.board);
    const result: SprintResult = { sprint: this.renderSprint(sprint), writes: [] };
    if (board && board.sprint !== sprint.id) {
      board.sprint = sprint.id;
      board.rev = this.nextRev();
      result.board = this.renderBoard(board);
    }
    this.emit({ kind: 'repo', repoId: 'repo-team' });
    return Promise.resolve(result);
  }

  closeSprint(id: string, carry: SprintCarry[] = [], rev?: string): Promise<SprintResult> {
    this.assertWritable();
    const sprint = this.sprints.get(id);
    if (!sprint) return Promise.reject(new ProviderError('not_found', `No sprint ${id}`));
    if (rev !== undefined && rev !== '*' && rev !== sprint.rev) {
      return Promise.reject(new ProviderError('stale_revision', `Sprint ${id} changed on disk`));
    }
    const view = this.renderSprint(sprint);
    const completed = view.cards.filter((card) => isDone(card));
    const incomplete = view.cards.filter((card) => !isDone(card));
    const carried = carry.map((decision) => {
      const outcome: SprintCarryResult = { ref: decision.ref, action: decision.action };
      if (decision.action === 'next') {
        const target =
          (decision.sprint ? this.sprints.get(decision.sprint) : undefined) ??
          [...this.sprints.values()].find((s) => s.board === sprint.board && s.state === 'planned');
        if (!target) {
          outcome.error = `no sprint to carry ${decision.ref} into`;
          return outcome;
        }
        if (!target.items.includes(decision.ref)) target.items.push(decision.ref);
        target.rev = this.nextRev();
        outcome.sprint = target.id;
        return outcome;
      }
      if (decision.action === 'backlog') {
        const id = decision.ref.split('/')[1] ?? '';
        const item = this.items.get(id);
        if (!item) {
          outcome.error = `project ${decision.ref.split('/')[0] ?? ''} is not cloned on this machine`;
          return outcome;
        }
        const status = decision.status ?? 'backlog';
        this.items.set(id, { ...item, status, rev: this.nextRev() });
        outcome.status = status;
        this.emit({ kind: 'items', repoId: 'repo-1', ids: [id] });
      }
      return outcome;
    });
    sprint.state = 'closed';
    sprint.rev = this.nextRev();
    this.emit({ kind: 'repo', repoId: 'repo-team' });
    return Promise.resolve({
      sprint: this.renderSprint(sprint),
      report: {
        sprint: sprint.id,
        board: sprint.board,
        completed,
        incomplete,
        unresolved: [],
        completedPoints: completed.reduce((sum, card) => sum + (card.estimate ?? 0), 0),
        incompletePoints: incomplete.reduce((sum, card) => sum + (card.estimate ?? 0), 0),
        metrics: view.sprint.metrics,
        carried,
      },
      writes: [],
    });
  }

  /** Renders a sprint the way `core.BuildSprintView` does. */
  private renderSprint(sprint: FakeSprint): SprintView {
    const board = this.boards.get(sprint.board);
    const cards = sprint.items.map((ref) => this.cardFor(ref, sprint));
    const backlog = board ? this.candidatesFor(board, sprint) : [];
    return {
      sprint: this.summarize(sprint, cards),
      cards,
      backlog,
      diagnostics: [],
    };
  }

  /** The header of a sprint: the file's own fields plus what the cards say. */
  private summarize(sprint: FakeSprint, cards: BoardCard[]): SprintSummary {
    const committed = new Set(sprint.committed ?? []);
    const started = sprint.state !== 'planned' || committed.size > 0;
    const resolved = cards.filter((card) => card.status !== undefined);
    const done = resolved.filter((card) => isDone(card));
    const points = (list: BoardCard[]) => list.reduce((sum, card) => sum + (card.estimate ?? 0), 0);
    const days = (from: string, to: string) =>
      Math.floor((Date.parse(to) - Date.parse(from)) / 86_400_000) + 1;
    return {
      id: sprint.id,
      title: sprint.title,
      board: sprint.board,
      state: sprint.state,
      start: sprint.start,
      end: sprint.end,
      ...(sprint.goal === undefined ? {} : { goal: sprint.goal }),
      ...(sprint.capacityHours === undefined ? {} : { capacityHours: sprint.capacityHours }),
      ...(sprint.velocityTarget === undefined ? {} : { velocityTarget: sprint.velocityTarget }),
      ...(sprint.participants ? { participants: sprint.participants } : {}),
      items: [...sprint.items],
      ...(sprint.committed ? { committed: [...sprint.committed] } : {}),
      totalDays: days(sprint.start, sprint.end),
      remainingDays: Math.max(0, days(this.today, sprint.end)),
      metrics: {
        items: sprint.items.length,
        resolved: resolved.length,
        done: done.length,
        points: points(resolved),
        committedPoints: points(resolved.filter((card) => committed.has(card.ref))),
        donePoints: points(done),
        added: started ? sprint.items.filter((ref) => !committed.has(ref)).length : 0,
        unresolved: sprint.items.length - resolved.length,
      },
      path: `.pmngr/sprints/${sprint.id}.md`,
      rev: sprint.rev,
    };
  }

  /** One card of a sprint scope, live or read from the committed snapshot. */
  private refCard(ref: string): BoardCard {
    const [project = '', id = ''] = ref.split('/');
    const declared = (this.team?.projects.map((p) => p.key) ?? []).includes(project);
    if (!this.projects.some((p) => p.key === project)) {
      return remoteCard(ref, project, id, declared);
    }
    const item = this.items.get(id);
    if (!item) {
      return {
        ref,
        project,
        item: id,
        declared,
        remote: false,
        reason: `${id} does not exist in the clone of ${project}`,
      };
    }
    return this.liveCard(project, item);
  }

  private cardFor(ref: string, sprint: FakeSprint): BoardCard {
    const [project = '', id = ''] = ref.split('/');
    const declared = (this.team?.projects.map((p) => p.key) ?? []).includes(project);
    const cloned = new Set(this.projects.map((p) => p.key));
    const committed = (sprint.committed ?? []).includes(ref);
    if (!cloned.has(project)) {
      return { ...remoteCard(ref, project, id, declared), inSprint: true, committed };
    }
    const item = this.items.get(id);
    if (!item) {
      return {
        ref,
        project,
        item: id,
        declared,
        remote: false,
        inSprint: true,
        committed,
        reason: `${id} does not exist in the clone of ${project}`,
      };
    }
    return { ...this.liveCard(project, item), inSprint: true, committed };
  }

  /** The candidates a board offers for a sprint: what it shows and the sprint does not. */
  private candidatesFor(board: FakeBoard, sprint: FakeSprint): BoardCard[] {
    const column = board.columns.find((c) => c.id === board.backlogColumn);
    const out: BoardCard[] = [];
    for (const item of this.items.values()) {
      if (item.deleted) continue;
      const project = item.id.split('-')[0] ?? '';
      if (!board.projects.includes(project)) continue;
      const types = board.filters?.types;
      if (types && !types.includes(item.type)) continue;
      const ref = `${project}/${item.id}`;
      if (sprint.items.includes(ref)) continue;
      const mapped = column
        ? (column.statuses[project] ?? column.statuses['*'] ?? [])
        : board.columns.flatMap((c) => c.statuses[project] ?? c.statuses['*'] ?? []);
      if (!item.status || !mapped.includes(item.status)) continue;
      out.push({ ...this.liveCard(project, item), backlog: true });
    }
    return out.sort((a, b) => a.ref.localeCompare(b.ref));
  }

  /** One card read from an open repository. */
  private liveCard(project: string, item: Item): BoardCard {
    return {
      ref: `${project}/${item.id}`,
      project,
      item: item.id,
      declared: true,
      remote: false,
      vaultId: 'repo-1',
      source: 'live',
      title: item.title,
      type: item.type,
      ...(item.status === undefined
        ? {}
        : { status: item.status, category: categoryOf(item.status) }),
      ...(item.priority === undefined ? {} : { priority: item.priority }),
      ...(item.assignees ? { assignees: item.assignees } : {}),
      ...(item.labels ? { labels: item.labels } : {}),
      ...(item.estimate === undefined ? {} : { estimate: item.estimate }),
      ...(item.updated === undefined ? {} : { updated: item.updated }),
      path: item.path,
      rev: item.rev,
    };
  }

  /**
   * Renders a board the way `core.BuildBoardView` does: filters first, then one
   * column per status mapping, then the `order` list, then everything else by
   * priority. A ref into a project the workspace has not opened stays where the
   * board puts it and is marked remote.
   */
  listSnapshots(): Promise<SnapshotResult[]> {
    return Promise.resolve(this.snapshotRows('unchanged'));
  }

  refreshSnapshots(input: SnapshotRefresh = {}): Promise<SnapshotResult[]> {
    if (!input.dryRun) this.assertWritable();
    const rows = this.snapshotRows('written').filter(
      (row) => !input.projects?.length || input.projects.includes(row.project),
    );
    return Promise.resolve(rows);
  }

  /** One row per declared project: cloned ones are regenerated, the rest skipped. */
  private snapshotRows(status: 'written' | 'unchanged'): SnapshotResult[] {
    const cloned = new Set(this.projects.map((p) => p.key));
    return (this.team?.projects ?? []).map((project) => ({
      project: project.key,
      path: project.snapshot.path,
      status: cloned.has(project.key) ? status : 'skipped',
      items: project.snapshot.items,
      ...(cloned.has(project.key)
        ? {}
        : { reason: 'no open repository serves this project; clone it to refresh its snapshot' }),
      info: project.snapshot,
    }));
  }

  private renderBoard(board: FakeBoard): BoardView {
    const declared = this.team?.projects.map((p) => p.key) ?? board.projects;
    const cloned = new Set(this.projects.map((p) => p.key));
    const rank: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 };
    // A scrum board shows the scope of its sprint, plus the candidates the
    // sprint does not list in its backlog column (docs/04 §5.5).
    const sprint =
      board.kind === 'scrum' && board.sprint ? this.sprints.get(board.sprint) : undefined;
    const inSprint = (ref: string) => sprint?.items.includes(ref) ?? false;
    const committed = (ref: string) => sprint?.committed?.includes(ref) ?? false;

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
        if (sprint && !inSprint(ref) && column.id !== board.backlogColumn) continue;
        cards.push({
          ...(sprint
            ? inSprint(ref)
              ? { inSprint: true, committed: committed(ref) }
              : { backlog: true }
            : {}),
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
        if (sprint && !inSprint(ref)) continue;
        cards.push({
          ...remoteCard(ref, project, id, declared.includes(project)),
          ...(sprint ? { inSprint: true, committed: committed(ref) } : {}),
        });
        placed.add(ref);
      }
      // A remote item the sprint lists but the order does not: it lands in the
      // column its snapshot status maps to.
      for (const ref of sprint?.items ?? []) {
        if (placed.has(ref)) continue;
        const [project = '', id = ''] = ref.split('/');
        if (cloned.has(project)) continue;
        const card = remoteCard(ref, project, id, declared.includes(project));
        const mapped = column.statuses[project] ?? column.statuses['*'] ?? [];
        if (!card.status || !mapped.includes(card.status)) continue;
        cards.push({ ...card, inSprint: true, committed: committed(ref) });
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
      if (sprint && !inSprint(ref)) continue;
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
      ...(board.sprint === undefined ? {} : { sprint: board.sprint }),
      ...(board.backlogColumn === undefined ? {} : { backlogColumn: board.backlogColumn }),
      ...(sprint
        ? {
            sprintInfo: this.summarize(
              sprint,
              sprint.items.map((ref) => this.cardFor(ref, sprint)),
            ),
          }
        : {}),
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

  // ---------------------------------------------------------------------- git

  /**
   * Commit-on-save, in memory. The fake keeps the settings and renders the
   * subject with the same rules as the runtimes, so a component test can prove
   * the form works without a git repository anywhere near it.
   */
  getGitSettings(): Promise<GitSettings> {
    return Promise.resolve({ ...this.git });
  }

  updateGitSettings(patch: GitSettingsPatch): Promise<GitSettings> {
    const next = { ...this.git, ...patch };
    if (next.commitDebounceMs < 0) {
      return Promise.reject(
        new ProviderError('validation_failed', 'commitDebounceMs must not be negative'),
      );
    }
    try {
      validateCommitTemplate(next.messageTemplate);
    } catch (error) {
      return Promise.reject(
        new ProviderError(
          'validation_failed',
          error instanceof Error ? error.message : String(error),
        ),
      );
    }
    this.git = { ...next, persisted: true };
    return Promise.resolve({ ...this.git });
  }

  getGitStatus(repoId?: string): Promise<GitRepoStatus[]> {
    return Promise.resolve(
      this.repos
        .filter((repo) => repoId === undefined || repo.id === repoId)
        .map((repo) => ({
          repo: repo.id,
          path: repo.location,
          git: true,
          backend: 'go-git',
          identity: 'Test User <test@example.com>',
          capabilities: {
            backend: 'go-git',
            hooks: false,
            signing: false,
            credentialHelpers: false,
            pathspecCommit: false,
          },
        })),
    );
  }

  commitNow(
    input: { repoId?: string; paths?: string[]; message?: string } = {},
  ): Promise<GitCommit[]> {
    const repo = input.repoId ?? this.repos[0]?.id ?? 'default';
    this.git = { ...this.git, pending: 0 };
    return Promise.resolve([
      {
        repo,
        sha: 'fake0000',
        subject: input.message ?? 'pmngr: update 1 item',
        empty: false,
        paths: input.paths ?? [],
      },
    ]);
  }

  // --------------------------------------------------------------- git sync

  /**
   * A clean, up-to-date repository unless a test moves `syncStatuses`. The
   * sync panel is then rendered from the same shapes both runtimes produce.
   */
  syncStatuses: SyncRepoStatus[] | null = null;

  /** The reports the next `sync()` resolves with. */
  syncResults: SyncResult[] | null = null;

  getSyncStatus(repoId?: string): Promise<SyncRepoStatus[]> {
    const rows =
      this.syncStatuses ??
      this.repos.map((repo) => ({
        repo: repo.id,
        path: repo.location,
        git: true,
        backend: 'go-git',
        pending: 0,
        status: {
          branch: 'main',
          detached: false,
          clean: true,
          trackedChanges: false,
          remote: 'origin',
          upstream: 'origin/main',
          ahead: 0,
          behind: 0,
          state: 'up_to_date' as const,
        },
      }));
    return Promise.resolve(rows.filter((row) => repoId === undefined || row.repo === repoId));
  }

  /** The sync settings a test may move with `updateSyncSettings`. */
  syncSettings: SyncSettings = {
    pullStrategy: 'rebase',
    pushOnSync: true,
    maxPushRetries: 3,
    supported: true,
  };

  getSyncSettings(): Promise<SyncSettings> {
    return Promise.resolve({ ...this.syncSettings });
  }

  updateSyncSettings(patch: SyncSettingsPatch): Promise<SyncSettings> {
    this.syncSettings = { ...this.syncSettings, ...patch };
    return Promise.resolve({ ...this.syncSettings });
  }

  async sync(repoId: string | undefined, opts: SyncOptions = {}): Promise<SyncResult[]> {
    if (this.syncResults) return this.syncResults;
    const rows = await this.getSyncStatus(repoId);
    return rows.map((row) => ({
      repo: row.repo,
      dryRun: opts.dryRun === true,
      strategy: opts.strategy ?? 'rebase',
      phase: 'done' as const,
      before: row.status as SyncStatus,
      after: row.status as SyncStatus,
      pulled: 0,
      pushed: 0,
      retries: 0,
      durationMs: 1,
    }));
  }

  async abortSync(repoId: string): Promise<SyncRepoStatus> {
    const rows = await this.getSyncStatus(repoId);
    const row = rows[0];
    if (!row) throw new ProviderError('not_found', `no repository ${repoId}`);
    return row;
  }

  listSyncConflicts(): Promise<{ repo: string; paths: string[]; operation?: string }[]> {
    const out = [...this.conflicts.values()].map((analysis) => ({
      repo: analysis.repo,
      paths: [analysis.path],
      ...(analysis.operation === undefined ? {} : { operation: analysis.operation }),
    }));
    return Promise.resolve(out);
  }

  /**
   * Conflicts a test seeded, keyed by `<repo>:<path>`. The fake provider is
   * what component tests render the ConflictResolver against, so it carries the
   * same analysis shape the two real providers return.
   */
  conflicts = new Map<string, ConflictAnalysis>();

  /** The resolutions the fake recorded, newest last; tests assert on them. */
  resolutions: { repo: string; path: string; resolution: ConflictResolution }[] = [];

  readConflict(repoId: string, path: string): Promise<ConflictAnalysis> {
    const analysis = this.conflicts.get(`${repoId}:${path}`);
    if (!analysis) {
      return Promise.reject(
        new ProviderError('not_found', `${path} is not conflicted in ${repoId}`),
      );
    }
    return Promise.resolve(analysis);
  }

  resolveConflict(
    repoId: string,
    path: string,
    resolution: ConflictResolution,
  ): Promise<ConflictResolveResult> {
    const analysis = this.conflicts.get(`${repoId}:${path}`);
    if (!analysis) {
      return Promise.reject(
        new ProviderError('not_found', `${path} is not conflicted in ${repoId}`),
      );
    }
    this.resolutions.push({ repo: repoId, path, resolution });
    this.conflicts.delete(`${repoId}:${path}`);
    const merge = analysis.merge ?? {
      path,
      structured: false,
      content: analysis.versions.ours ?? '',
      conflicted: 0,
      review: 0,
      clean: true,
    };
    return Promise.resolve({
      repo: repoId,
      path,
      merge: { ...merge, clean: true, conflicted: 0 },
      result: { staged: true, continued: resolution.continue !== false, remaining: [] },
    });
  }

  subscribe(handler: (event: ChangeEvent) => void): Unsubscribe {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }
}
