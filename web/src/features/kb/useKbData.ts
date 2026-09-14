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
import { useEffect, useState } from 'react';

import type {
  KbFeedbackNoteDraft,
  KbNode,
  KbPage,
  KbScope,
  KbSyncJobResult,
  KbSyncSelector,
  KbSyncStatusResult,
  KbUnlinkResult,
} from '@/api/provider';
import { useProvider } from '@/api/provider-context';

export function kbTreeKey(project: string) {
  return ['kb', 'tree', project] as const;
}

export function kbPageKey(project: string, path: string) {
  return ['kb', 'page', project, path] as const;
}

/**
 * Synchronization state is keyed by what was asked for, `remote` included.
 *
 * The local answer and the checked-against-the-article answer are two
 * different facts — "in sync as far as this clone knows" is not "in sync,
 * verified" — so they must never share a cache entry, or pressing "check" once
 * would make every later local read claim to have been verified.
 */
export function kbSyncKey(project: string, path: string, remote: boolean) {
  return ['kb', 'sync', project, path, remote] as const;
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

/**
 * The synchronization state of the selected pages.
 *
 * `remote` is off by default and that is the point: a tree view asks about
 * every page at once, and the local answer — the page's own `external` entry
 * against the content it would publish now — costs no request at all. Reading
 * the articles is reserved for the explicit "check" the badge offers.
 *
 * `enabled` is how a caller keeps the query out of a runtime that cannot reach
 * YouTrack, where the provider fails loudly by design.
 */
export function useKbSyncStatus(
  project: string,
  selector: KbSyncSelector,
  enabled = true,
): UseQueryResult<KbSyncStatusResult, Error> {
  const provider = useProvider();
  const path = selector.path ?? '';
  const remote = selector.remote === true;
  return useQuery({
    queryKey: kbSyncKey(project, path, remote),
    queryFn: () => provider.kbSyncStatus({ ...selector, project }),
    enabled: enabled && project !== '',
    // A remote check is a question about another system; a failed one is a
    // state the badge renders, not something to hammer the instance over.
    retry: false,
  });
}

/**
 * Queues a publish or a pull.
 *
 * Neither happens inline: a handbook is hundreds of articles, so both answer
 * with a job id and the pages the job selected, and the outcome arrives over
 * the `sync.job.*` stream that `useKbSyncInvalidation` already listens to.
 */
export function useKbSyncJob(
  project: string,
  direction: 'publish' | 'pull',
): UseMutationResult<KbSyncJobResult, Error, KbSyncSelector> {
  const provider = useProvider();
  const queryClient = useQueryClient();
  return useMutation<KbSyncJobResult, Error, KbSyncSelector>({
    mutationFn: (selector) =>
      direction === 'publish'
        ? provider.publishKbPage({ ...selector, project })
        : provider.pullKbPage({ ...selector, project }),
    onSuccess: () => {
      // The job has only been queued, so nothing has changed yet. The refetch
      // is still worth it: queuing is when the local side last told the truth
      // about itself, and the job's own events invalidate again as it runs.
      void queryClient.invalidateQueries({ queryKey: ['kb', 'sync', project] });
    },
  });
}

/**
 * Forgetting the article a page mirrors (GIT-US-0095).
 *
 * Unlike the two directions above this is not a job: the `external:` entry
 * leaves the page's front matter and the call is done, so the page's state is
 * refetched immediately rather than waited for on the event stream. The page
 * itself is invalidated too — its front matter is what changed.
 */
export function useKbUnlink(
  project: string,
): UseMutationResult<KbUnlinkResult, Error, KbSyncSelector> {
  const provider = useProvider();
  const queryClient = useQueryClient();
  return useMutation<KbUnlinkResult, Error, KbSyncSelector>({
    mutationFn: (selector) => provider.unlinkKbPage({ ...selector, project }),
    onSuccess: (_result, selector) => {
      void queryClient.invalidateQueries({ queryKey: ['kb', 'sync', project] });
      void queryClient.invalidateQueries({ queryKey: kbPageKey(project, selector.path ?? '') });
    },
  });
}

/**
 * One conflict as the `youtrack.kb.conflict` event reports it: the page that
 * diverged and the file the incoming content was written to instead.
 */
export type KbConflict = {
  path: string;
  conflictPath: string;
  articleId?: string;
  direction: 'publish' | 'pull';
};

/**
 * Conflicts that arrived while this screen was open, by page path.
 *
 * The status query alone would be enough to *know* a page is in conflict, but
 * not to link to the file the incoming content went to: only the event carries
 * `conflictPath`. Holding the frames here is also what makes the notice appear
 * the moment a background job finds a divergence, rather than at the next
 * refetch.
 */
export function useKbConflicts(project: string): Map<string, KbConflict> {
  const provider = useProvider();
  const [conflicts, setConflicts] = useState<Map<string, KbConflict>>(() => new Map());

  useEffect(
    () =>
      provider.subscribe((event) => {
        if (event.kind !== 'kbConflict') return;
        if (project !== '' && event.project !== '' && event.project !== project) return;
        setConflicts((previous) => {
          const next = new Map(previous);
          next.set(event.path, {
            path: event.path,
            conflictPath: event.conflictPath,
            ...(event.articleId === undefined ? {} : { articleId: event.articleId }),
            direction: event.direction,
          });
          return next;
        });
      }),
    [provider, project],
  );

  return conflicts;
}

/**
 * Keeps the synchronization state fresh without a manual refresh.
 *
 * Two event families move it and both are bridged here. A `sync.job.*` frame
 * for a knowledge-base job means a publish or a pull has changed what the two
 * sides hold; a `youtrack.kb.conflict` frame means a page and its article have
 * diverged and a `<page>.conflict.md` now sits beside it, so the tree has to be
 * re-listed as well — that file is new.
 *
 * A frame with no `kind` is the synthetic resync the provider raises after a
 * reconnect or an overflow: its whole purpose is "you may have missed
 * something", so it invalidates too.
 */
export function useKbSyncInvalidation(project: string): void {
  const provider = useProvider();
  const queryClient = useQueryClient();

  useEffect(
    () =>
      provider.subscribe((event) => {
        if (event.kind === 'syncJob') {
          const kind = event.job.kind;
          if (kind !== '' && !kind.startsWith('youtrack.kb.')) return;
          void queryClient.invalidateQueries({ queryKey: ['kb', 'sync', project] });
          return;
        }
        if (event.kind === 'kbConflict') {
          void queryClient.invalidateQueries({ queryKey: ['kb', 'sync', project] });
          void queryClient.invalidateQueries({ queryKey: kbTreeKey(project) });
          void queryClient.invalidateQueries({ queryKey: kbPageKey(project, event.path) });
        }
      }),
    [provider, queryClient, project],
  );
}

/**
 * Whether this runtime and this project can synchronize a knowledge base at
 * all.
 *
 * The two capabilities answer different questions and both have to hold.
 * `youtrackSupported` is about the *runtime*: false in browser-only mode, where
 * there is no process to hold a credential. `youtrack` is about a *project*
 * being linked, and pushing a page to an instance nothing is linked to is
 * meaningless. The settings card deliberately shows on the first alone; this
 * toolbar needs both.
 */
export function useKbSyncEnabled(): boolean {
  const provider = useProvider();
  const capabilities = provider.capabilities;
  return capabilities.youtrackSupported && capabilities.youtrack;
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
