/**
 * The sidebar entry.
 *
 * It is the one place where "this project has no inbox" has to be silent: a
 * project that declares no triage status gets no link at all, rather than a
 * link to a page explaining why there is nothing on it.
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
import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider, sampleItems, sampleProject } from '@/api/fake-provider';
import type { DataProvider } from '@/api/provider';
import { InboxNavLink } from '@/features/inbox/InboxNavLink';
import { inboxProvider } from '@/features/inbox/test-utils';

function Placeholder() {
  return <div data-testid="placeholder" />;
}

function renderNav(provider: DataProvider) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    component: () => (
      <>
        <InboxNavLink project="ACME" />
        <Outlet />
      </>
    ),
  });
  const routeTree = rootRoute.addChildren([
    createRoute({ getParentRoute: () => rootRoute, path: '/', component: Placeholder }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: '/p/$project/inbox',
      component: Placeholder,
    }),
  ]);
  const router = createRouter({ routeTree, history: createMemoryHistory() });

  return render(
    <QueryClientProvider client={queryClient}>
      <DataProviderProvider provider={provider}>
        <RouterProvider router={router as never} />
      </DataProviderProvider>
    </QueryClientProvider>,
  );
}

describe('InboxNavLink', () => {
  it('links to the queue and shows how many submissions are waiting', async () => {
    renderNav(inboxProvider());

    const link = await screen.findByRole('link', { name: /ACME inbox/ });
    expect(link).toHaveAttribute('href', '/p/ACME/inbox');
    // Two: one pending, plus one whose snooze date has already passed.
    expect(await screen.findByLabelText('2 waiting')).toHaveTextContent('2');
  });

  it('renders nothing for a project that declares no triage status', async () => {
    renderNav(new FakeProvider({ projects: [sampleProject], items: sampleItems }));

    await waitFor(() => {
      expect(screen.getByTestId('placeholder')).toBeInTheDocument();
    });
    expect(screen.queryByRole('link', { name: /inbox/i })).toBeNull();
  });

  it('drops the badge once the queue is clear', async () => {
    renderNav(inboxProvider({ items: sampleItems }));

    await screen.findByRole('link', { name: /ACME inbox/ });
    expect(screen.queryByLabelText(/waiting/)).toBeNull();
  });
});
