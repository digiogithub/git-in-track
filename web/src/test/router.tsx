/* eslint-disable react-refresh/only-export-components -- test-only helper module. */

/**
 * Test harness: renders a component inside a memory router and a query client,
 * with the `DataProvider` context wired to a `FakeProvider` by default.
 *
 * The route tree mirrors the real one closely enough for `<Link>` targets used
 * by the shell and the workspace screens to resolve.
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
import { render, type RenderResult } from '@testing-library/react';
import type { FunctionComponent } from 'react';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider } from '@/api/fake-provider';
import type { DataProvider } from '@/api/provider';

export type RenderWithRouterOptions = {
  /** Component rendered at `/`. */
  index: FunctionComponent;
  /** Layout component; defaults to a bare `<Outlet />`. */
  root?: FunctionComponent;
  provider?: DataProvider;
  initialPath?: string;
};

function Passthrough() {
  return <Outlet />;
}

function Placeholder() {
  return <div data-testid="placeholder" />;
}

export function renderWithRouter(options: RenderWithRouterOptions): RenderResult & {
  provider: DataProvider;
} {
  const provider = options.provider ?? new FakeProvider();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });

  const rootRoute = createRootRoute({ component: options.root ?? Passthrough });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: '/', component: options.index }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: '/repos/add',
      component: Placeholder,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: '/p/$project/items',
      component: Placeholder,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: '/p/$project/kb/$',
      component: Placeholder,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: '/p/$project/items/$id',
      component: Placeholder,
    }),
    createRoute({ getParentRoute: () => rootRoute, path: '/boards', component: Placeholder }),
    createRoute({ getParentRoute: () => rootRoute, path: '/boards/$slug', component: Placeholder }),
    createRoute({ getParentRoute: () => rootRoute, path: '/settings', component: Placeholder }),
  ]);

  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [options.initialPath ?? '/'] }),
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
