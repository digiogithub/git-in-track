/* eslint-disable react-refresh/only-export-components -- test harness, not a refreshable module */

/**
 * Router + query harness for the editor pages. The app router is registered
 * globally for types, so tests mount an equivalent memory-history tree with the
 * same paths.
 */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  Outlet,
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  useParams,
} from '@tanstack/react-router';
import { render } from '@testing-library/react';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import type { DataProvider } from '@/api/provider';
import { ItemEditorPage } from '@/features/editor/ItemEditorPage';
import { NewItemPage } from '@/features/editor/NewItemPage';
import { validateNewItemSearch } from '@/features/editor/search';

function ItemDetailStub() {
  const params = useParams({ strict: false });
  return <p>Detail of {params.id}</p>;
}

function ItemListStub() {
  return <p>Item list</p>;
}

export function renderEditorRoute(path: string, provider: DataProvider) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const projectRoute = createRoute({ getParentRoute: () => rootRoute, path: '/p/$project' });
  const itemsRoute = createRoute({
    getParentRoute: () => projectRoute,
    path: 'items',
    component: ItemListStub,
  });
  const newItemRoute = createRoute({
    getParentRoute: () => projectRoute,
    path: 'items/new',
    validateSearch: validateNewItemSearch,
    component: NewItemPage,
  });
  const detailRoute = createRoute({
    getParentRoute: () => projectRoute,
    path: 'items/$id',
    component: ItemDetailStub,
  });
  const editRoute = createRoute({
    getParentRoute: () => projectRoute,
    path: 'items/$id/edit',
    component: ItemEditorPage,
  });

  const routeTree = rootRoute.addChildren([
    projectRoute.addChildren([itemsRoute, newItemRoute, detailRoute, editRoute]),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });

  const utils = render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        <RouterProvider router={router} />
      </DataProviderProvider>
    </QueryClientProvider>,
  );

  return { ...utils, router, queryClient };
}
