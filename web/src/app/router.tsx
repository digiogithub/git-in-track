import { createRoute, createRouter, lazyRouteComponent } from '@tanstack/react-router';

import { NotFound } from '@/app/layout/NotFound';
import { rootRoute } from '@/app/rootRoute';
import { EpicTree } from '@/features/backlog/EpicTree';
import { ItemDetail } from '@/features/backlog/ItemDetail';
import { ItemTable } from '@/features/backlog/ItemTable';
import { MilestoneList } from '@/features/backlog/MilestoneList';
import { validateItemSearch } from '@/features/backlog/search';
import { BoardList } from '@/features/boards/BoardList';
import { validateNewItemSearch } from '@/features/editor/search';
import { KbViewer } from '@/features/kb/KbViewer';
import { SettingsPage } from '@/features/settings/SettingsPage';
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
  component: lazyRouteComponent(
    () => import('@/features/editor/NewItemPage'),
    'NewItemPage',
  ),
});

const itemEditorRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'items/$id/edit',
  component: lazyRouteComponent(
    () => import('@/features/editor/ItemEditorPage'),
    'ItemEditorPage',
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
    epicsRoute,
    milestonesRoute,
  ]),
  boardsRoute,
  boardRoute,
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
