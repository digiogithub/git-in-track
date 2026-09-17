import { createRoute, createRouter, lazyRouteComponent } from '@tanstack/react-router';

import { NotFound } from '@/app/layout/NotFound';
import { ProjectLayout } from '@/app/layout/ProjectLayout';
import { rootRoute } from '@/app/rootRoute';
import { EpicTree } from '@/features/backlog/EpicTree';
import { ItemDetail } from '@/features/backlog/ItemDetail';
import { ItemTable } from '@/features/backlog/ItemTable';
import { MilestoneList } from '@/features/backlog/MilestoneList';
import { validateItemSearch } from '@/features/backlog/search';
import { BoardList } from '@/features/boards/BoardList';
import { validateNewItemSearch } from '@/features/editor/search';
import { validateInboxSearch } from '@/features/inbox/search';
import { KbViewer } from '@/features/kb/KbViewer';
import { SettingsPage } from '@/features/settings/SettingsPage';
import { SyncPanel } from '@/features/sync/SyncPanel';
import { AddRepositoryPage } from '@/features/workspace/AddRepositoryPage';
import { WorkspaceHome } from '@/features/workspace/WorkspaceHome';

/**
 * Code-based route tree (docs/05-web-app.md §3). Declaring the tree explicitly
 * instead of using file-based routing keeps route ids, params and search
 * schemas type-checked from one place.
 */
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: WorkspaceHome,
});

const addRepositoryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/repos/add',
  component: AddRepositoryPage,
});

export const projectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/p/$project',
  component: ProjectLayout,
});

/** Splat route: everything after `kb/` is a path inside the docs folder. */
const kbRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'kb/$',
  component: KbViewer,
});

/** Filters, search and sort live in the search params, validated with zod. */
const itemsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'items',
  validateSearch: validateItemSearch,
  component: ItemTable,
});

const itemDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'items/$id',
  component: ItemDetail,
});

/** The editor pulls in CodeMirror, so both routes load it as a lazy chunk. */
const newItemRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'items/new',
  validateSearch: validateNewItemSearch,
  component: lazyRouteComponent(() => import('@/features/editor/NewItemPage'), 'NewItemPage'),
});

const itemEditorRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'items/$id/edit',
  component: lazyRouteComponent(() => import('@/features/editor/ItemEditorPage'), 'ItemEditorPage'),
});

/**
 * The triage queue (ADR-033, story GIT-US-0060). Its filter and the row it is
 * showing live in the search params, so a half-finished pass is a link; the
 * accept form is a route of its own because accepting is an edit.
 */
const inboxRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'inbox',
  validateSearch: validateInboxSearch,
  component: lazyRouteComponent(() => import('@/features/inbox/InboxPage'), 'InboxPage'),
});

const inboxAcceptRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'inbox/$id/accept',
  component: lazyRouteComponent(
    () => import('@/features/inbox/InboxAcceptPage'),
    'InboxAcceptPage',
  ),
});

const epicsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'epics',
  component: EpicTree,
});

const milestonesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'milestones',
  component: MilestoneList,
});

const boardsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/boards',
  component: BoardList,
});

/** The board itself pulls in dnd-kit, so it loads as a lazy chunk. */
const boardRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/boards/$slug',
  component: lazyRouteComponent(() => import('@/features/boards/BoardView'), 'BoardView'),
});

/** The sprint index (docs/04 §8, story GIT-US-0032). */
const sprintsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sprints',
  validateSearch: (search: Record<string, unknown>): { board?: string } =>
    typeof search['board'] === 'string' && search['board'] !== '' ? { board: search['board'] } : {},
  component: lazyRouteComponent(() => import('@/features/boards/SprintList'), 'SprintList'),
});

/** Retrospectives (docs/05 §4, docs/04 §9, story GIT-US-0027). */
const retrosRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/retros',
  component: lazyRouteComponent(() => import('@/features/retros/RetroList'), 'RetroList'),
});

const retroRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/retros/$retroId',
  component: lazyRouteComponent(() => import('@/features/retros/RetroBoard'), 'RetroBoard'),
});

/** Sprint metrics (docs/05 §16, docs/04 §12, story GIT-US-0028). */
const metricsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/metrics',
  component: lazyRouteComponent(() => import('@/features/metrics/SprintMetrics'), 'MetricsIndex'),
});

const sprintMetricsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/metrics/$sprintId',
  component: lazyRouteComponent(() => import('@/features/metrics/SprintMetrics'), 'SprintMetrics'),
});

/** The sync panel (docs/05 §5, story GIT-US-0021). */
const syncRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/sync',
  component: SyncPanel,
});

/**
 * The agent chat (docs/05 §19, story GIT-US-0057). Lazy, because the page
 * pulls the AG-UI client and the Markdown pipeline in behind it and most
 * sessions never open it; the page itself renders the unavailable state when
 * `capabilities.agent` is false, so the route always resolves.
 */
const agentRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/agent',
  component: lazyRouteComponent(() => import('@/features/agent'), 'AgentPage'),
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsPage,
});

export const routeTree = rootRoute.addChildren([
  indexRoute,
  addRepositoryRoute,
  projectRoute.addChildren([
    kbRoute,
    itemsRoute,
    newItemRoute,
    itemDetailRoute,
    itemEditorRoute,
    inboxRoute,
    inboxAcceptRoute,
    epicsRoute,
    milestonesRoute,
  ]),
  boardsRoute,
  boardRoute,
  sprintsRoute,
  retrosRoute,
  retroRoute,
  metricsRoute,
  sprintMetricsRoute,
  syncRoute,
  agentRoute,
  settingsRoute,
]);

export function createAppRouter() {
  return createRouter({
    routeTree,
    defaultPreload: 'intent',
    defaultNotFoundComponent: NotFound,
  });
}

export const router = createAppRouter();

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
