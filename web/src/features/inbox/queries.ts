/**
 * TanStack Query hooks for the triage queue (ADR-033), modelled on
 * `features/boards/sprint-queries.ts`.
 *
 * Two things are worth knowing before reading the code.
 *
 * First, the counts are never computed here. `listInbox` answers with
 * whole-queue `counts` and `pending`, resolved by the host against its own
 * clock, so an expired snooze is already counted as pending. A badge that
 * re-derived them from the page it happens to be holding would disagree with
 * the list as soon as the queue was longer than one page.
 *
 * Second, `inbox` change events arrive in both runtimes. The companion raises
 * them from the `inbox.changed` frame; browser-only mode has no WebSocket at
 * all, so the browser provider raises the very same event after each local
 * decision. That is why there is one subscription and no second code path for
 * the runtime without a server.
 */

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import { useEffect } from 'react';

import type { InboxFilter, InboxPage, InboxTriageInput, InboxTriageResult } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { backlogKeys } from '@/features/backlog/queries';

/** Key factory. Every inbox key lives under the project prefix. */
export const inboxKeys = {
  all: () => ['inbox'] as const,
  project: (project: string) => ['inbox', project] as const,
  lists: (project: string) => ['inbox', project, 'list'] as const,
  list: (project: string, filter: InboxFilter) =>
    ['inbox', project, 'list', stableInboxFilterKey(filter)] as const,
  pending: (project: string) => ['inbox', project, 'pending'] as const,
};

/** Deterministic key for a filter object: property order must not matter. */
export function stableInboxFilterKey(filter: InboxFilter): string {
  const entries = Object.entries(filter)
    .filter(([, value]) => value !== undefined)
    .sort(([a], [b]) => a.localeCompare(b));
  return JSON.stringify(entries);
}

type InboxPages = InfiniteData<InboxPage, string | undefined>;

/** Cursor-paginated triage queue. `fetchNextPage` is the "Load more" action. */
export function useInbox(filter: InboxFilter, enabled = true) {
  const provider = useProvider();
  const project = filter.project ?? '';
  return useInfiniteQuery({
    queryKey: inboxKeys.list(project, filter),
    queryFn: ({ pageParam }) =>
      provider.listInbox(pageParam ? { ...filter, cursor: pageParam } : filter),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: InboxPage) => last.nextCursor,
    enabled: enabled && project !== '',
  });
}

/**
 * The number the sidebar badge renders.
 *
 * It asks for one row and reads `pending` off the answer: the count is over the
 * whole queue, so a page of one is enough and the sidebar never pays for a
 * listing it does not show.
 */
export function useInboxPending(project: string, enabled = true): UseQueryResult<number, Error> {
  const provider = useProvider();
  return useQuery({
    queryKey: inboxKeys.pending(project),
    queryFn: () => provider.listInbox({ project, limit: 1 }).then((page) => page.pending),
    enabled: enabled && project !== '',
  });
}

/** What an optimistic triage has to be able to put back when the write fails. */
type TriageContext = { pages: InboxPages | undefined; pending: number | undefined };

/**
 * One triage decision, applied optimistically against the slice on screen.
 *
 * The row is taken out of the list the moment the button is pressed, because a
 * triage pass is a rhythm and waiting for a round trip between every keystroke
 * breaks it. Everything is snapshotted first and put back verbatim on failure,
 * so a refused write leaves the queue exactly as the person last saw it.
 */
export function useTriageInboxItem(
  project: string,
  filter: InboxFilter,
): UseMutationResult<InboxTriageResult, Error, InboxTriageInput, TriageContext> {
  const provider = useProvider();
  const queryClient = useQueryClient();
  const listKey = inboxKeys.list(project, filter);
  const pendingKey = inboxKeys.pending(project);

  return useMutation<InboxTriageResult, Error, InboxTriageInput, TriageContext>({
    mutationFn: (input) => provider.triageInboxItem(input),
    onMutate: async (input) => {
      await queryClient.cancelQueries({ queryKey: listKey });
      const pages = queryClient.getQueryData<InboxPages>(listKey);
      const pending = queryClient.getQueryData<number>(pendingKey);

      // Under `all` every decision keeps the row — it only changes state — so
      // the list is left alone and only the badge moves.
      if (filter.status !== undefined) {
        queryClient.setQueryData<InboxPages>(listKey, (current) =>
          current === undefined ? current : removeItem(current, input.id),
        );
      }
      // The badge only moves optimistically while the pending slice is on
      // screen, where the row visibly leaves the queue. Deciding about a
      // snoozed or an already-rejected row changes nothing about how many
      // submissions are waiting, and guessing otherwise would make the badge
      // flicker to a number that was never true.
      if (pending !== undefined && isPendingSlice(filter)) {
        queryClient.setQueryData<number>(pendingKey, Math.max(0, pending - 1));
      }
      return { pages, pending };
    },
    onError: (_error, _input, context) => {
      if (context?.pages !== undefined) queryClient.setQueryData(listKey, context.pages);
      if (context?.pending !== undefined) queryClient.setQueryData(pendingKey, context.pending);
    },
    onSuccess: (result) => {
      // The answer carries the queue behind the decision, so the badge is
      // corrected from the host's own count rather than from the arithmetic
      // above (they differ whenever someone else triaged at the same time).
      queryClient.setQueryData(pendingKey, result.pending);
    },
    onSettled: (_result, _error, input) => {
      void queryClient.invalidateQueries({ queryKey: inboxKeys.project(project) });
      // Accepting writes a workflow status onto the item, so the backlog views
      // of that project are stale too.
      void queryClient.invalidateQueries({ queryKey: backlogKeys.detail(project, input.id) });
      void queryClient.invalidateQueries({ queryKey: backlogKeys.lists(project) });
    },
  });
}

/** Whether the slice on screen is the pending queue itself. */
function isPendingSlice(filter: InboxFilter): boolean {
  return filter.status?.length === 1 && filter.status[0] === 'pending';
}

/** Drops one row from every cached page, keeping the counts honest. */
function removeItem(data: InboxPages, id: string): InboxPages {
  return {
    ...data,
    pages: data.pages.map((page) => {
      const items = page.items.filter((item) => item.id !== id);
      if (items.length === page.items.length) return page;
      return { ...page, items, total: Math.max(0, page.total - 1) };
    }),
  };
}

/**
 * Bridges `inbox` change events into the cache.
 *
 * The list is invalidated and the badge is written straight from the frame's
 * own pending count, so the sidebar is right immediately and the open detail
 * pane is not refetched for a decision taken on a different row.
 */
export function useInboxEvents(project: string): void {
  const provider = useProvider();
  const queryClient = useQueryClient();

  useEffect(
    () =>
      provider.subscribe((event) => {
        if (event.kind !== 'inbox') return;
        if (project !== '' && event.project !== '' && event.project !== project) return;
        queryClient.setQueryData(inboxKeys.pending(project), event.pending);
        void queryClient.invalidateQueries({ queryKey: inboxKeys.lists(project) });
      }),
    [provider, queryClient, project],
  );
}
