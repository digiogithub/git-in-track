/**
 * TanStack Query hooks for sprints (docs/04-team-repository.md §8).
 *
 * Every sprint write is one write to the sprint file in the team repository —
 * planning never touches an item — so a mutation invalidates the sprint, the
 * board it scopes and, when a closing decision sent an item back to the
 * backlog, the backlog of the project that item belongs to.
 */

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from '@tanstack/react-query';
import { useEffect } from 'react';

import type {
  SprintCloseInput,
  SprintDraft,
  SprintFilter,
  SprintPatch,
  SprintResult,
  SprintSummary,
  SprintTransferInput,
  SprintView,
} from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { backlogKeys } from '@/features/backlog/queries';
import { boardKeys } from '@/features/boards/queries';
import { useActiveTeamKey } from '@/features/workspace/active-team';

/** Key factory. Every sprint key lives under the `sprints` prefix. */
export const sprintKeys = {
  all: () => ['sprints'] as const,
  list: (filter: SprintFilter = {}, team = '') =>
    ['sprints', 'list', filter.board ?? '', filter.state ?? '', team] as const,
  detail: (id: string, team = '') => ['sprints', 'detail', id, team] as const,
  /** One close preview. The destination is part of the key: change it, re-run it. */
  closePreview: (id: string, mode: string, target: string, team = '') =>
    ['sprints', 'close-preview', id, mode, target, team] as const,
};

export function useSprints(filter: SprintFilter = {}) {
  const provider = useProvider();
  const team = useActiveTeamKey();
  return useQuery<SprintSummary[]>({
    queryKey: sprintKeys.list(filter, team),
    queryFn: () => provider.listSprints(filter, team),
  });
}

export function useSprint(id: string | undefined) {
  const provider = useProvider();
  const team = useActiveTeamKey();
  return useQuery<SprintView>({
    queryKey: sprintKeys.detail(id ?? '', team),
    queryFn: () => provider.getSprint(id ?? '', team),
    enabled: Boolean(id),
  });
}

/** Everything a sprint write invalidates: the sprint, the boards and the items. */
function useSprintInvalidation(): (result: SprintResult) => void {
  const queryClient = useQueryClient();
  const team = useActiveTeamKey();
  return (result) => {
    queryClient.setQueryData(sprintKeys.detail(result.sprint.sprint.id, team), result.sprint);
    void queryClient.invalidateQueries({ queryKey: sprintKeys.all() });
    void queryClient.invalidateQueries({ queryKey: boardKeys.all() });
    for (const carried of result.report?.carried ?? []) {
      if (carried.status === undefined) continue;
      void queryClient.invalidateQueries({
        queryKey: backlogKeys.project(carried.ref.split('/')[0] ?? ''),
      });
    }
  };
}

export function useCreateSprint(): UseMutationResult<SprintResult, Error, SprintDraft> {
  const provider = useProvider();
  const settle = useSprintInvalidation();
  const team = useActiveTeamKey();
  return useMutation<SprintResult, Error, SprintDraft>({
    mutationFn: (draft) => provider.createSprint(draft, team),
    onSuccess: settle,
  });
}

/** One planning edit: the goal, the dates or a reference in or out of the scope. */
export type SprintEdit = { id: string; patch: SprintPatch; rev?: string | undefined };

export function useUpdateSprint(): UseMutationResult<SprintResult, Error, SprintEdit> {
  const provider = useProvider();
  const settle = useSprintInvalidation();
  const team = useActiveTeamKey();
  return useMutation<SprintResult, Error, SprintEdit>({
    mutationFn: (edit) => provider.updateSprint(edit.id, edit.patch, edit.rev, team),
    onSuccess: settle,
  });
}

export type SprintStart = { id: string; rev?: string | undefined; force?: boolean | undefined };

export function useStartSprint(): UseMutationResult<SprintResult, Error, SprintStart> {
  const provider = useProvider();
  const settle = useSprintInvalidation();
  const team = useActiveTeamKey();
  return useMutation<SprintResult, Error, SprintStart>({
    mutationFn: (input) => provider.startSprint(input.id, input.rev, input.force, team),
    onSuccess: settle,
  });
}

/**
 * One close. `dryRun` is what the confirmation dialog runs first: it computes
 * the whole report and writes nothing, so the cache must not be touched by it.
 */
export type SprintClose = { id: string } & SprintCloseInput;

export function useCloseSprint(): UseMutationResult<SprintResult, Error, SprintClose> {
  const provider = useProvider();
  const settle = useSprintInvalidation();
  const team = useActiveTeamKey();
  return useMutation<SprintResult, Error, SprintClose>({
    mutationFn: ({ id, ...input }) => provider.closeSprint(id, input, team),
    onSuccess: (result) => {
      // A dry run changed nothing on disk; invalidating on it would throw away
      // a perfectly good cache and, worse, suggest something happened.
      if (result.dryRun === true) return;
      settle(result);
    },
  });
}

/**
 * The dry run behind the confirmation dialog.
 *
 * It is modelled as a *query* rather than as a mutation on purpose: a dry run
 * writes nothing — not even a write set — and publishes no event, so it is a
 * read that happens to compute a report. Keying it by the destination is what
 * makes changing "next sprint" to "backlog" re-run the preview, so what the
 * dialog shows is always the plan the confirm button would commit.
 */
export function useCloseSprintPreview(
  id: string | undefined,
  input: SprintCloseInput,
  enabled: boolean,
): UseQueryResult<SprintResult, Error> {
  const provider = useProvider();
  const team = useActiveTeamKey();
  const mode = input.transfer?.mode ?? 'none';
  const target = input.transfer?.target ?? '';
  return useQuery<SprintResult, Error>({
    queryKey: sprintKeys.closePreview(id ?? '', mode, target, team),
    queryFn: () => provider.closeSprint(id ?? '', { ...input, dryRun: true }, team),
    enabled: enabled && Boolean(id),
    // The scope moves while the dialog is open in another tab; a cached preview
    // that no longer matches the sprint would be worse than a second read.
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
}

/** One transfer: move the unfinished scope of a sprint without closing anything. */
export type SprintTransferVariables = { id: string } & SprintTransferInput;

export function useTransferSprintItems(): UseMutationResult<
  SprintResult,
  Error,
  SprintTransferVariables
> {
  const provider = useProvider();
  const settle = useSprintInvalidation();
  const team = useActiveTeamKey();
  return useMutation<SprintResult, Error, SprintTransferVariables>({
    mutationFn: ({ id, ...input }) => provider.transferSprintItems(id, input, team),
    onSuccess: (result) => {
      if (result.dryRun === true) return;
      settle(result);
    },
  });
}

/**
 * Bridges `sprint.changed` into the query cache.
 *
 * A close or a transfer performed elsewhere — another tab, an agent over MCP —
 * moves scope this tab is looking at, so the sprint, its board and the backlog
 * of every project that received an item have to be re-read. A dry run
 * publishes no frame at all, which is exactly why this can invalidate on every
 * one it sees.
 */
export function useSprintEvents(): void {
  const provider = useProvider();
  const queryClient = useQueryClient();
  useEffect(
    () =>
      provider.subscribe((event) => {
        if (event.kind !== 'sprint') return;
        void queryClient.invalidateQueries({ queryKey: sprintKeys.all() });
        void queryClient.invalidateQueries({ queryKey: boardKeys.all() });
        if (event.carried > 0) {
          void queryClient.invalidateQueries({ queryKey: backlogKeys.all() });
        }
      }),
    [provider, queryClient],
  );
}
