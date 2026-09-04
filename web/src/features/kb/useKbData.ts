/**
 * TanStack Query wiring for the KB viewer (docs/05-web-app.md §5).
 *
 * Keys are `['kb','tree',project]` and `['kb','page',project,path]`, and the
 * provider's `kb` change events invalidate exactly the page that changed plus
 * the tree (a write can add or rename a file).
 */

import { useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { useEffect } from 'react';

import type { KbNode, KbPage, KbScope } from '@/api/provider';
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
