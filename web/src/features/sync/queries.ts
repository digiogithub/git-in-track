/**
 * TanStack Query hooks over the companion's background job engine
 * (epic GIT-EP-0015, stories GIT-US-0074 and GIT-US-0081).
 *
 * The queue has two sources and only one of them is authoritative. `GET
 * /api/v1/sync/jobs` is the engine's own consistent snapshot; the `sync.job.*`
 * stream is a live hint that tells the cache *when* to read it again. A client
 * can miss frames — the hub disconnects one that fills its 256-event buffer —
 * so every frame, including the synthetic `resync` the provider raises after a
 * reconnect, a `stream.overflow` or a `resume.gap`, does the same thing here:
 * it invalidates the listing.
 *
 * That is also why there is no polling. The stream already exists, and a timer
 * on top of it would fight with it for no new information.
 */

import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { useEffect, useRef } from 'react';

import type { SyncJob, SyncJobEvent, SyncJobFilter, SyncJobPage } from '@/api/provider';
import { useProvider } from '@/api/provider-context';

/** Key factory. Everything the queue owns lives under one prefix. */
export const syncJobKeys = {
  all: () => ['syncJobs'] as const,
  lists: () => ['syncJobs', 'list'] as const,
  list: (filter: SyncJobFilter) => ['syncJobs', 'list', stableJobFilterKey(filter)] as const,
  detail: (id: string) => ['syncJobs', 'detail', id] as const,
};

/** Deterministic key for a filter object: property order must not matter. */
export function stableJobFilterKey(filter: SyncJobFilter): string {
  const entries = Object.entries(filter)
    .filter(([, value]) => value !== undefined)
    .sort(([a], [b]) => a.localeCompare(b));
  return JSON.stringify(entries);
}

/**
 * One page of the queue. `enabled` is how a caller keeps the query out of a
 * runtime that has no engine, where the provider fails loudly by design.
 */
export function useSyncJobs(
  filter: SyncJobFilter = {},
  enabled = true,
): UseQueryResult<SyncJobPage> {
  const provider = useProvider();
  return useQuery({
    queryKey: syncJobKeys.list(filter),
    queryFn: () => provider.listSyncJobs(filter),
    enabled,
  });
}

/** One job, read on demand — after a terminal event, to get its error text. */
export function useSyncJob(id: string, enabled = true): UseQueryResult<SyncJob> {
  const provider = useProvider();
  return useQuery({
    queryKey: syncJobKeys.detail(id),
    queryFn: () => provider.getSyncJob(id),
    enabled: enabled && id !== '',
  });
}

/**
 * Bridges the `sync.job.*` stream into the query cache, the way
 * `useBacklogEvents` bridges the write stream.
 *
 * Progress is coalesced server-side to at most one frame every 500 ms per
 * coalescing group and a terminal frame is never throttled, so this hook adds
 * **no** throttling of its own: a second layer would only delay the truth. A
 * missing intermediate frame is normal — counts jump — and is not an error.
 *
 * `onEvent` is called for every frame before the invalidation, so a caller that
 * is following one job (an import's progress strip) reads the counts straight
 * from the frame instead of waiting for a refetch. It is held in a ref, so
 * passing an inline closure does not resubscribe on every render.
 */
export function useSyncJobEvents(onEvent?: (event: SyncJobEvent) => void): void {
  const provider = useProvider();
  const queryClient = useQueryClient();
  const handler = useRef(onEvent);
  handler.current = onEvent;

  useEffect(
    () =>
      provider.subscribe((event) => {
        if (event.kind !== 'syncJob') return;
        handler.current?.(event.job);
        void queryClient.invalidateQueries({ queryKey: syncJobKeys.all() });
      }),
    [provider, queryClient],
  );
}

/**
 * Re-queues a job. A failed one keeps its id; a cancelled one comes back as a
 * new job, because the engine's state machine has no edge out of `cancelled` —
 * so the answer, not the id that was asked for, is what a caller follows.
 */
export function useRetrySyncJob() {
  const provider = useProvider();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => provider.retrySyncJob(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: syncJobKeys.all() });
    },
  });
}

/** Withdraws a queued or running job. */
export function useCancelSyncJob() {
  const provider = useProvider();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => provider.cancelSyncJob(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: syncJobKeys.all() });
    },
  });
}
