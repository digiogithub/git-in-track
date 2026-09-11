/**
 * TanStack Query wiring for the KB viewer (docs/05-web-app.md §5).
 *
 * Keys are `['kb','tree',project]` and `['kb','page',project,path]`, and the
 * provider's `kb` change events invalidate exactly the page that changed plus
 * the tree (a write can add or rename a file).
 */

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import { useEffect } from 'react';

import type { KbFeedbackNoteDraft, KbNode, KbPage, KbScope } from '@/api/provider';
import { useProvider } from '@/api/provider-context';

export function kbTreeKey(project: string) {
  return ['kb', 'tree', project] as const;
}

export function kbPageKey(project: string, path: string) {
  return ['kb', 'page', project, path] as const;
}

export function useKbTree(project: string, scope: KbScope): UseQueryResult<KbNode[], Error> {
  const provider = useProvider();
  return useQuery({
    queryKey: kbTreeKey(project),
    queryFn: () => provider.listKbTree(scope),
    enabled: project !== '',
  });
}

export function useKbPage(
  project: string,
  scope: KbScope,
  path: string,
): UseQueryResult<KbPage, Error> {
  const provider = useProvider();
  return useQuery({
    queryKey: kbPageKey(project, path),
    queryFn: () => provider.getPage(scope, path),
    enabled: project !== '' && path !== '',
    // A missing page is a 404 screen, not something to retry.
    retry: false,
  });
}

export type AddPageFeedbackVariables = {
  /** The page path as the provider returned it. */
  path: string;
  /** The path the page query is keyed by (the route's). */
  viewPath: string;
  notes: KbFeedbackNoteDraft[];
  rev?: string;
};

/** Saves feedback notes into the feedback block of a page. */
export function useAddPageFeedback(
  project: string,
  scope: KbScope,
): UseMutationResult<KbPage, Error, AddPageFeedbackVariables> {
  const provider = useProvider();
  const queryClient = useQueryClient();
  return useMutation<KbPage, Error, AddPageFeedbackVariables>({
    mutationFn: ({ path, notes, rev }) => provider.addPageFeedback(scope, path, notes, rev),
    onSuccess: (page, { viewPath }) => {
      queryClient.setQueryData(kbPageKey(project, viewPath), page);
    },
    onError: (_error, { viewPath }) => {
      // A stale revision means the page moved on: show what it holds now.
      void queryClient.invalidateQueries({ queryKey: kbPageKey(project, viewPath) });
    },
  });
}

/** Keeps the viewer in step with file-system and companion change events. */
export function useKbInvalidation(project: string): void {
  const provider = useProvider();
  const queryClient = useQueryClient();

  useEffect(() => {
    return provider.subscribe((event) => {
      if (event.kind !== 'kb') return;
      void queryClient.invalidateQueries({ queryKey: kbTreeKey(project) });
      for (const path of event.paths) {
        void queryClient.invalidateQueries({ queryKey: kbPageKey(project, path) });
      }
    });
  }, [provider, queryClient, project]);
}
