/* eslint-disable react-refresh/only-export-components -- test harness, not a rendered module */
/**
 * Rendering harness for the triage screens.
 *
 * The route tree mirrors the inbox slice of the real one, including the
 * validated search params, so these tests exercise the feature — filter state
 * really does round-trip through the URL here — and not the assembled
 * application router.
 */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  type AnyRouter,
} from '@tanstack/react-router';
import { configure, render, type RenderResult } from '@testing-library/react';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import {
  FakeProvider,
  sampleInboxItems,
  sampleItems,
  sampleTriageProject,
} from '@/api/fake-provider';
import type { DataProvider } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { InboxAcceptPage } from '@/features/inbox/InboxAcceptPage';
import { InboxPage } from '@/features/inbox/InboxPage';
import { validateInboxSearch } from '@/features/inbox/search';

// The Markdown pipeline and the provider reads can take longer than the 1s
// default when the whole suite runs in parallel.
configure({ asyncUtilTimeout: 5_000 });

function Passthrough() {
  return <Outlet />;
}

function Placeholder() {
  return <div data-testid="placeholder" />;
}

/** A provider holding a project that has an inbox, and a queue in it. */
export function inboxProvider(overrides: ConstructorParameters<typeof FakeProvider>[0] = {}) {
  return new FakeProvider({
    projects: [sampleTriageProject],
    items: [...sampleItems, ...sampleInboxItems],
    today: '2026-09-02',
    ...overrides,
  });
}

export type RenderInboxOptions = {
  /** Initial location, e.g. `/p/ACME/inbox?filter=snoozed`. */
  path?: string;
  provider?: DataProvider;
};

export function renderInbox(
  options: RenderInboxOptions = {},
): RenderResult & { provider: DataProvider; router: AnyRouter } {
  const provider = options.provider ?? inboxProvider();
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
        path: 'inbox',
        validateSearch: validateInboxSearch,
        component: InboxPage,
      }),
      createRoute({
        getParentRoute: () => projectRoute,
        path: 'inbox/$id/accept',
        component: InboxAcceptPage,
      }),
      createRoute({ getParentRoute: () => projectRoute, path: 'items', component: Placeholder }),
      createRoute({
        getParentRoute: () => projectRoute,
        path: 'items/$id',
        component: Placeholder,
      }),
    ]),
  ]);

  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [options.path ?? '/p/ACME/inbox'] }),
  });

  const result = render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        {/* The real host lives in `AppShell`; triage actions announce
            themselves through it, so the tests need one too. */}
        <ToastProvider>
          <RouterProvider router={router as never} />
        </ToastProvider>
      </DataProviderProvider>
    </QueryClientProvider>,
  );

  return { ...result, provider, router: router as AnyRouter };
}
