/**
 * Background job notifications (story GIT-US-0081, task GIT-T-0169).
 *
 * Three promises: a job that ends is announced wherever the user is, a burst is
 * one notification rather than eleven, and a failure offers the queue.
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
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import type { SyncJobEvent } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';
import { ToastProvider } from '@/components/ui/toast';
import { SyncJobToasts, TOAST_COALESCE_MS } from '@/features/sync/SyncJobToasts';

function frame(over: Partial<SyncJobEvent> = {}): SyncJobEvent {
  return {
    phase: 'done',
    id: 'job_000021',
    kind: 'youtrack.import',
    key: 'ACME',
    state: 'done',
    attempt: 1,
    processed: 20,
    total: 20,
    error: '',
    errorClass: '',
    ...over,
  };
}

/** The shell, rendered on a route that is not Settings. */
function renderToasts() {
  const provider = new FakeProvider({ syncEngine: { jobs: [] } });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const rootRoute = createRootRoute({
    component: () => (
      <>
        <SyncJobToasts />
        <Outlet />
      </>
    ),
  });
  const routeTree = rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: '/',
      component: () => <div data-testid="home" />,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: '/settings',
      component: () => <div data-testid="settings" />,
    }),
  ]);
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  });

  render(
    <QueryClientProvider client={queryClient}>
      <ProviderContext.Provider value={provider}>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </ProviderContext.Provider>
    </QueryClientProvider>,
  );
  return { provider, router };
}

function emit(provider: FakeProvider, job: SyncJobEvent) {
  act(() => {
    provider.emitEvent({ kind: 'syncJob', job });
  });
}

function settle() {
  act(() => {
    vi.advanceTimersByTime(TOAST_COALESCE_MS + 10);
  });
}

describe('SyncJobToasts', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('announces a finished job from any route', async () => {
    const { provider } = renderToasts();
    await act(async () => {
      await Promise.resolve();
    });

    emit(provider, frame());
    settle();

    expect(screen.getByText('A background job finished')).toBeInTheDocument();
  });

  it('coalesces a burst into one summary toast', async () => {
    const { provider } = renderToasts();
    await act(async () => {
      await Promise.resolve();
    });

    for (let i = 0; i < 5; i += 1) {
      emit(provider, frame({ id: `job_00002${String(i)}` }));
    }
    settle();

    expect(screen.getByText('5 background jobs finished')).toBeInTheDocument();
    expect(screen.queryByText('A background job finished')).toBeNull();
  });

  it('says nothing about a job someone cancelled', async () => {
    const { provider } = renderToasts();
    await act(async () => {
      await Promise.resolve();
    });

    emit(provider, frame({ state: 'cancelled' }));
    settle();

    expect(screen.queryByText(/background job/)).toBeNull();
  });

  it('offers the queue on a failure, and renders the engine text as it is', async () => {
    vi.useRealTimers();
    const user = userEvent.setup();
    const { provider, router } = renderToasts();
    await act(async () => {
      await Promise.resolve();
    });

    emit(
      provider,
      frame({ phase: 'failed', state: 'failed', error: '403 Forbidden', errorClass: 'terminal' }),
    );
    await screen.findByText('A background job failed');
    expect(screen.getByText('403 Forbidden')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Show the queue' }));

    expect(router.state.location.pathname).toBe('/settings');
  });

  it('counts failures apart from successes in the same burst', async () => {
    const { provider } = renderToasts();
    await act(async () => {
      await Promise.resolve();
    });

    emit(provider, frame({ id: 'job_1' }));
    emit(provider, frame({ id: 'job_2' }));
    emit(provider, frame({ id: 'job_3', phase: 'failed', state: 'failed', error: 'boom' }));
    settle();

    expect(screen.getByText('A background job failed')).toBeInTheDocument();
    expect(screen.getByText('2 background jobs finished')).toBeInTheDocument();
  });
});
