/* eslint-disable react-refresh/only-export-components -- test harness, not a rendered module */
/**
 * Rendering harness for the backlog screens.
 *
 * The route tree mirrors the backlog slice of the real one (including the
 * validated search params) but is built here, so these tests exercise the
 * feature and not the assembled application router.
 */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { configure, render, type RenderResult } from '@testing-library/react';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider } from '@/api/fake-provider';
import type { DataProvider } from '@/api/provider';
import { EpicTree } from '@/features/backlog/EpicTree';
import { ItemDetail } from '@/features/backlog/ItemDetail';
import { ItemTable } from '@/features/backlog/ItemTable';
import { MilestoneList } from '@/features/backlog/MilestoneList';
import { validateItemSearch } from '@/features/backlog/search';

// Provider reads plus the Markdown pipeline can take longer than the 1s
// default when the whole suite runs in parallel.
configure({ asyncUtilTimeout: 5_000 });

function Passthrough() {
  return <Outlet />;
}

function Placeholder() {
  return <div data-testid="placeholder" />;
}

export type RenderBacklogOptions = {
  /** Initial location, e.g. `/p/ACME/items?status=backlog`. */
  path: string;
  provider?: DataProvider;
};

export function renderBacklog(
  options: RenderBacklogOptions,
): RenderResult & { provider: DataProvider } {
  const provider = options.provider ?? new FakeProvider();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });

  const rootRoute = createRootRoute({ component: Passthrough });
  const projectRoute = createRoute({ getParentRoute: () => rootRoute, path: '/p/$project' });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: '/', component: Placeholder }),
    projectRoute.addChildren([
      createRoute({
        getParentRoute: () => projectRoute,
        path: 'items',
        validateSearch: validateItemSearch,
        component: ItemTable,
      }),
      createRoute({
        getParentRoute: () => projectRoute,
        path: 'items/new',
        component: Placeholder,
      }),
      createRoute({ getParentRoute: () => projectRoute, path: 'items/$id', component: ItemDetail }),
      createRoute({
        getParentRoute: () => projectRoute,
        path: 'items/$id/edit',
        component: Placeholder,
      }),
      createRoute({ getParentRoute: () => projectRoute, path: 'epics', component: EpicTree }),
      createRoute({
        getParentRoute: () => projectRoute,
        path: 'milestones',
        component: MilestoneList,
      }),
    ]),
  ]);

  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [options.path] }),
  });

  const result = render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        <RouterProvider router={router} />
      </DataProviderProvider>
    </QueryClientProvider>,
  );

  return { ...result, provider };
}
