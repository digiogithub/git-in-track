/**
 * TanStack Query hooks for the backlog (docs/05-web-app.md §5).
 *
 * Every item-shaped key lives under `['items', <projectKey>, …]` so one
 * `ChangeEvent` can invalidate exactly the affected project, and every write is
 * revision-checked with an optimistic update plus rollback.
 */

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
  type UseMutationResult,
} from '@tanstack/react-query';
import { useEffect } from 'react';

import type {
  Comment,
  Item,
  ItemFilter,
  ItemPage,
  ItemStatus,
  ProjectSummary,
} from '@/api/provider';
import { useProvider } from '@/api/provider-context';

/** Key factory. Keep every backlog key under the project prefix. */
export const backlogKeys = {
  projects: () => ['projects'] as const,
  project: (project: string) => ['items', project] as const,
  lists: (project: string) => ['items', project, 'list'] as const,
  list: (project: string, filter: ItemFilter) =>
    ['items', project, 'list', stableFilterKey(filter)] as const,
  detail: (project: string, id: string) => ['items', project, 'detail', id] as const,
  children: (project: string, id: string) => ['items', project, 'children', id] as const,
  comments: (project: string, id: string) => ['items', project, 'comments', id] as const,
};

/** Deterministic key for a filter object: property order must not matter. */
export function stableFilterKey(filter: ItemFilter): string {
  const entries = Object.entries(filter)
    .filter(([, value]) => value !== undefined)
    .sort(([a], [b]) => a.localeCompare(b));
  return JSON.stringify(entries);
}

export function useProjects() {
  const provider = useProvider();
  return useQuery({
    queryKey: backlogKeys.projects(),
    queryFn: () => provider.listProjects(),
  });
}

/** The project workflow (statuses, labels, priorities) behind every filter. */
export function useProject(key: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: backlogKeys.projects(),
    queryFn: () => provider.listProjects(),
    select: (projects: ProjectSummary[]) => projects.find((p) => p.key === key),
  });
}

/** Cursor-paginated item list. `fetchNextPage` is the "Load more" action. */
export function useItems(filter: ItemFilter) {
  const provider = useProvider();
  const project = filter.project ?? '';
  return useInfiniteQuery({
    queryKey: backlogKeys.list(project, filter),
    queryFn: ({ pageParam }) =>
      provider.listItems(pageParam ? { ...filter, cursor: pageParam } : filter),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last: ItemPage) => last.nextCursor,
  });
}

export function useItem(project: string, id: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: backlogKeys.detail(project, id),
    queryFn: () => provider.getItem(id),
    enabled: id.length > 0,
  });
}

export function useChildren(project: string, id: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: backlogKeys.children(project, id),
    queryFn: () => provider.getChildren(id),
    enabled: id.length > 0,
  });
}

export function useComments(project: string, id: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: backlogKeys.comments(project, id),
    queryFn: () => provider.listComments(id),
    enabled: id.length > 0,
  });
}

/**
 * Bridges the provider change stream into the query cache: an `items` event
 * invalidates the project subtree, an index/repo event also refreshes the
 * project workflow.
 */
export function useBacklogEvents(project: string): void {
  const provider = useProvider();
  const queryClient = useQueryClient();

  useEffect(
    () =>
      provider.subscribe((event) => {
        if (event.kind === 'items') {
          void queryClient.invalidateQueries({ queryKey: backlogKeys.project(project) });
          return;
        }
        if (event.kind === 'index' || event.kind === 'repo') {
          void queryClient.invalidateQueries({ queryKey: backlogKeys.projects() });
          void queryClient.invalidateQueries({ queryKey: backlogKeys.project(project) });
        }
      }),
    [provider, queryClient, project],
  );
}

function isItem(value: unknown): value is Item {
  return typeof value === 'object' && value !== null && 'id' in value && 'rev' in value;
}

function isItemPage(value: unknown): value is ItemPage {
  return typeof value === 'object' && value !== null && Array.isArray((value as ItemPage).items);
}

function isInfiniteItemPages(value: unknown): value is InfiniteData<ItemPage, string | undefined> {
  return (
    typeof value === 'object' &&
    value !== null &&
    Array.isArray((value as InfiniteData<ItemPage>).pages)
  );
}

/** Applies a status change to whatever item-shaped payload a cache entry holds. */
function patchCachedStatus(data: unknown, id: string, status: ItemStatus): unknown {
  const patchItem = (item: Item): Item => (item.id === id ? { ...item, status } : item);

  if (isInfiniteItemPages(data)) {
    return {
      ...data,
      pages: data.pages.map((page) => ({ ...page, items: page.items.map(patchItem) })),
    };
  }
  if (isItemPage(data)) {
    return { ...data, items: data.items.map(patchItem) };
  }
  if (Array.isArray(data)) {
    return data.map((entry: unknown) => (isItem(entry) ? patchItem(entry) : entry));
  }
  if (isItem(data)) {
    return patchItem(data);
  }
  return data;
}

export type MoveItemVariables = {
  id: string;
  status: ItemStatus;
  /** The revision the user was looking at; a mismatch is a `stale_revision`. */
  rev: string;
};

type MoveContext = { previous: [readonly unknown[], unknown][] };

/**
 * Rev-checked status move with an optimistic cache update. On failure the
 * snapshot is restored and the project subtree refetched, so a
 * `stale_revision` never leaves the UI showing a change that was rejected.
 */
export function useMoveItem(
  project: string,
): UseMutationResult<Item, Error, MoveItemVariables, MoveContext> {
  const provider = useProvider();
  const queryClient = useQueryClient();

  return useMutation<Item, Error, MoveItemVariables, MoveContext>({
    mutationFn: ({ id, status, rev }) => provider.moveItem(id, status, rev),
    onMutate: async ({ id, status }) => {
      await queryClient.cancelQueries({ queryKey: backlogKeys.project(project) });
      const previous = queryClient.getQueriesData({ queryKey: backlogKeys.project(project) });
      queryClient.setQueriesData({ queryKey: backlogKeys.project(project) }, (data: unknown) =>
        patchCachedStatus(data, id, status),
      );
      return { previous };
    },
    onError: (_error, _variables, context) => {
      restoreSnapshot(queryClient, context);
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: backlogKeys.project(project) });
    },
  });
}

function restoreSnapshot(queryClient: QueryClient, context: MoveContext | undefined): void {
  for (const [key, data] of context?.previous ?? []) {
    queryClient.setQueryData(key, data);
  }
}

export type AddCommentVariables = { id: string; body: string };

/** Comment composer write. Posting is gated on `capabilities.write` by the UI. */
export function useAddComment(
  project: string,
): UseMutationResult<Comment, Error, AddCommentVariables> {
  const provider = useProvider();
  const queryClient = useQueryClient();

  return useMutation<Comment, Error, AddCommentVariables>({
    mutationFn: ({ id, body }) => provider.addComment(id, body),
    onSuccess: (_comment, { id }) => {
      void queryClient.invalidateQueries({ queryKey: backlogKeys.comments(project, id) });
    },
  });
}
