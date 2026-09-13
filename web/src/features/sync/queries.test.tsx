/**
 * The `sync.job.*` bridge (story GIT-US-0074, task GIT-T-0137).
 *
 * The hook has one job — turn a frame into an invalidation — and three
 * properties that matter more than that: it lets go of its subscription when it
 * unmounts, it survives a reconnect (the provider's synthetic `resync` frame is
 * an ordinary frame to it), and it adds no throttling of its own on top of the
 * engine's, so a burst of frames is a burst of invalidations rather than a
 * queue of stale renders.
 */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render } from '@testing-library/react';
import { describe, expect, it, vi, type Mock } from 'vitest';

import type { ChangeEvent, DataProvider, SyncJobEvent, Unsubscribe } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';
import { syncJobKeys, useSyncJobEvents } from '@/features/sync/queries';

/** One frame, with the bookkeeping every topic shares. */
function frame(over: Partial<SyncJobEvent> = {}): SyncJobEvent {
  return {
    phase: 'progress',
    id: 'job_000021',
    kind: 'youtrack.import',
    key: 'ACME',
    state: 'running',
    attempt: 1,
    processed: 3,
    total: 20,
    error: '',
    errorClass: '',
    ...over,
  };
}

/**
 * A provider that is nothing but an event source: the hook reads no data, so
 * everything else on the interface is beside the point here.
 */
function eventProvider() {
  const handlers = new Set<(event: ChangeEvent) => void>();
  const unsubscribe = vi.fn();
  const provider = {
    kind: 'companion',
    capabilities: {},
    subscribe: vi.fn((handler: (event: ChangeEvent) => void): Unsubscribe => {
      handlers.add(handler);
      return () => {
        handlers.delete(handler);
        unsubscribe();
      };
    }),
  } as unknown as DataProvider;
  const emit = (event: ChangeEvent) => {
    act(() => {
      for (const handler of [...handlers]) handler(event);
    });
  };
  return { provider, emit, unsubscribe, handlers };
}

function Probe({ onEvent }: { onEvent?: (event: SyncJobEvent) => void }) {
  useSyncJobEvents(onEvent);
  return null;
}

function renderProbe(provider: DataProvider, onEvent?: (event: SyncJobEvent) => void) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
  const view = render(
    <QueryClientProvider client={queryClient}>
      <ProviderContext.Provider value={provider}>
        <Probe {...(onEvent === undefined ? {} : { onEvent })} />
      </ProviderContext.Provider>
    </QueryClientProvider>,
  );
  return { ...view, invalidate };
}

describe('useSyncJobEvents', () => {
  it('subscribes once and invalidates the jobs query on every frame', () => {
    const source = eventProvider();
    const { invalidate } = renderProbe(source.provider);

    expect(source.provider.subscribe).toHaveBeenCalledTimes(1);

    source.emit({ kind: 'syncJob', job: frame({ phase: 'queued' }) });
    source.emit({ kind: 'syncJob', job: frame({ processed: 7 }) });

    expect(invalidate).toHaveBeenCalledTimes(2);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: syncJobKeys.all() });
  });

  it('ignores the events of every other feature', () => {
    const source = eventProvider();
    const { invalidate } = renderProbe(source.provider);

    source.emit({ kind: 'items', repoId: 'ACME', ids: ['ACME-T-0311'] });
    source.emit({ kind: 'repo', repoId: 'ACME' });

    expect(invalidate).not.toHaveBeenCalled();
  });

  it('hands every frame to the caller before invalidating, unthrottled', () => {
    const source = eventProvider();
    const seen: number[] = [];
    renderProbe(source.provider, (event) => seen.push(event.processed));

    // Progress is already coalesced to one frame per 500 ms per group at the
    // source, so three frames in a row are three real steps and all three must
    // arrive: a second layer of throttling here would hide the truth.
    source.emit({ kind: 'syncJob', job: frame({ processed: 1 }) });
    source.emit({ kind: 'syncJob', job: frame({ processed: 9 }) });
    source.emit({ kind: 'syncJob', job: frame({ phase: 'done', state: 'done', processed: 20 }) });

    expect(seen).toEqual([1, 9, 20]);
  });

  it('treats the reconnect resync as an ordinary frame, so the listing is re-read', () => {
    const source = eventProvider();
    const seen: string[] = [];
    const { invalidate } = renderProbe(source.provider, (event) => seen.push(event.phase));

    // The provider raises this after a reconnect, a stream.overflow or a
    // resume.gap: the live counts are no longer trustworthy and only
    // `GET /sync/jobs` is.
    source.emit({ kind: 'syncJob', job: frame({ phase: 'resync', id: '', state: '' }) });

    expect(seen).toEqual(['resync']);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: syncJobKeys.all() });
  });

  it('unsubscribes on unmount and stops invalidating', () => {
    const source = eventProvider();
    const { unmount, invalidate } = renderProbe(source.provider);

    unmount();

    expect(source.unsubscribe).toHaveBeenCalledTimes(1);
    expect(source.handlers.size).toBe(0);
    source.emit({ kind: 'syncJob', job: frame() });
    expect(invalidate).not.toHaveBeenCalled();
  });

  it('does not resubscribe when the caller passes a new inline closure', () => {
    const source = eventProvider();
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const view = render(
      <QueryClientProvider client={queryClient}>
        <ProviderContext.Provider value={source.provider}>
          <Probe onEvent={() => undefined} />
        </ProviderContext.Provider>
      </QueryClientProvider>,
    );
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <ProviderContext.Provider value={source.provider}>
          <Probe onEvent={() => undefined} />
        </ProviderContext.Provider>
      </QueryClientProvider>,
    );

    expect((source.provider.subscribe as unknown as Mock).mock.calls).toHaveLength(1);
  });
});
