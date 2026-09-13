/* eslint-disable react-refresh/only-export-components -- test harness, not a rendered module */
/**
 * Rendering harness for the import dialog.
 *
 * The dialog links to items it created, so it needs a router; it renders
 * tooltips and toasts, so it needs their hosts. The route tree is the thin
 * slice the dialog can reach and is built here rather than imported, so these
 * tests exercise the feature and not the assembled application router.
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
import { useState, type ReactNode } from 'react';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import type { FakeProvider } from '@/api/fake-provider';
import { ToastProvider } from '@/components/ui/toast';
import { TooltipProvider } from '@/components/ui/tooltip';
import { ImportDialog } from '@/features/youtrack/ImportDialog';

// The dialog waits on a fake provider and on TanStack Query; the 1s default is
// tight when the whole suite runs in parallel.
configure({ asyncUtilTimeout: 5_000 });

function Passthrough() {
  return <Outlet />;
}

function Placeholder() {
  return <div data-testid="placeholder" />;
}

/** Mounts `children` under a memory router the dialog's links can resolve. */
export function withRouter(children: ReactNode, path = '/p/GIT/items') {
  const rootRoute = createRootRoute({ component: Passthrough });
  const projectRoute = createRoute({ getParentRoute: () => rootRoute, path: '/p/$project' });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: '/', component: Placeholder }),
    createRoute({ getParentRoute: () => rootRoute, path: '/settings', component: Placeholder }),
    projectRoute.addChildren([
      createRoute({
        getParentRoute: () => projectRoute,
        path: 'items',
        component: () => <>{children}</>,
      }),
      createRoute({
        getParentRoute: () => projectRoute,
        path: 'items/$id',
        component: Placeholder,
      }),
    ]),
  ]);

  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  return <RouterProvider router={router} />;
}

/** The dialog, open, with its own close state so "Done" is exercisable. */
function OpenDialog({ projectKey }: { projectKey: string }) {
  const [open, setOpen] = useState(true);
  return <ImportDialog open={open} onOpenChange={setOpen} projectKey={projectKey} />;
}

export function renderImportDialog(
  provider: FakeProvider,
  projectKey = 'GIT',
): RenderResult & { provider: FakeProvider } {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        <TooltipProvider delayDuration={0}>
          <ToastProvider>{withRouter(<OpenDialog projectKey={projectKey} />)}</ToastProvider>
        </TooltipProvider>
      </DataProviderProvider>
    </QueryClientProvider>,
  );
  return { ...result, provider };
}
