import { createRoute, createRouter } from '@tanstack/react-router';

import { NotFound } from '@/app/layout/NotFound';
import { rootRoute } from '@/app/rootRoute';
import { ItemDetail } from '@/features/backlog/ItemDetail';
import { ItemTable } from '@/features/backlog/ItemTable';
import { BoardList } from '@/features/boards/BoardList';
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

const itemsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'items',
  component: ItemTable,
});

const itemDetailRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: 'items/$id',
  component: ItemDetail,
});

const boardsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/boards',
  component: BoardList,
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SettingsPage,
});

export const routeTree = rootRoute.addChildren([
  indexRoute,
  addRepositoryRoute,
  projectRoute.addChildren([kbRoute, itemsRoute, itemDetailRoute]),
  boardsRoute,
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
