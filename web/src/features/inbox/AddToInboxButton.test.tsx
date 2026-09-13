/**
 * The capture form.
 *
 * Three things are worth pinning. It files with `source: web`, because that is
 * how a triager later tells a form submission from an agent's or an importer's.
 * It refuses an empty title without asking the host. And it renders nothing at
 * all for a project that declares no triage status, the same silence the
 * sidebar entry keeps (ADR-033).
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
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { DataProviderProvider } from '@/api/DataProviderProvider';
import { FakeProvider, sampleItems, sampleProject } from '@/api/fake-provider';
import type { DataProvider } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { AddToInboxButton } from '@/features/inbox/AddToInboxButton';
import { inboxProvider } from '@/features/inbox/test-utils';

function Placeholder() {
  return <div data-testid="placeholder" />;
}

function renderButton(provider: DataProvider) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const rootRoute = createRootRoute({
    component: () => (
      <>
        <AddToInboxButton project="ACME" variant="bar" />
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
        <ToastProvider>
          <RouterProvider router={router as never} />
        </ToastProvider>
      </DataProviderProvider>
    </QueryClientProvider>,
  );
}

describe('AddToInboxButton', () => {
  it('files a pending submission with source web and links to the queue', async () => {
    const user = userEvent.setup();
    const provider = inboxProvider();
    const filed = vi.spyOn(provider, 'createInboxItem');
    renderButton(provider);

    await user.click(await screen.findByRole('button', { name: /add to inbox/i }));

    const dialog = await screen.findByRole('dialog');
    await user.type(
      await screen.findByLabelText(/^title$/i),
      'Checkout hangs on Safari after the address step',
    );
    await user.type(screen.getByLabelText(/what happened/i), 'Reproduced on 17.6.');
    await user.click(within(dialog).getByRole('button', { name: /^add to inbox$/i }));

    await waitFor(() => expect(filed).toHaveBeenCalledTimes(1));
    const draft = filed.mock.calls[0]?.[0];
    expect(draft).toMatchObject({
      project: 'ACME',
      title: 'Checkout hangs on Safari after the address step',
      source: 'web',
      body: 'Reproduced on 17.6.',
    });
    // The capture form asks no planning question: nothing places the work.
    expect(draft).not.toHaveProperty('parent');
    expect(draft).not.toHaveProperty('status');

    // The confirmation names the id and offers the queue.
    const link = await screen.findByRole('link', { name: /open the inbox/i });
    expect(link).toHaveAttribute('href', '/p/ACME/inbox');

    const created = await filed.mock.results[0]?.value;
    expect(created.inbox).toMatchObject({ status: 'pending', source: 'web' });
  });

  it('refuses an empty title without asking the host', async () => {
    const user = userEvent.setup();
    const provider = inboxProvider();
    const filed = vi.spyOn(provider, 'createInboxItem');
    renderButton(provider);

    await user.click(await screen.findByRole('button', { name: /add to inbox/i }));
    const dialog = await screen.findByRole('dialog');
    const submit = within(dialog).getByRole('button', { name: /^add to inbox$/i });
    expect(submit).toBeDisabled();

    // Whitespace is not a title either.
    await user.type(await screen.findByLabelText(/^title$/i), '   ');
    expect(within(dialog).getByRole('button', { name: /^add to inbox$/i })).toBeDisabled();
    expect(filed).not.toHaveBeenCalled();
  });

  it('renders nothing for a project that declares no triage status', async () => {
    const provider = new FakeProvider({ projects: [sampleProject], items: sampleItems });
    renderButton(provider);

    await screen.findByTestId('placeholder');
    await waitFor(() => expect(screen.queryByRole('button', { name: /add to inbox/i })).toBeNull());
  });
});
