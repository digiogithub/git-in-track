/**
 * TanStack Query hooks for sprints (docs/04-team-repository.md §8).
 *
 * Every sprint write is one write to the sprint file in the team repository —
 * planning never touches an item — so a mutation invalidates the sprint, the
 * board it scopes and, when a closing decision sent an item back to the
 * backlog, the backlog of the project that item belongs to.
 */

import { useMutation, useQuery, useQueryClient, type UseMutationResult } from '@tanstack/react-query';

import type {
  SprintCarry,
  SprintDraft,
  SprintFilter,
  SprintPatch,
  SprintResult,
  SprintSummary,
  SprintView,
} from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { backlogKeys } from '@/features/backlog/queries';
import { boardKeys } from '@/features/boards/queries';

/** Key factory. Every sprint key lives under the `sprints` prefix. */
export const sprintKeys = {
  all: () => ['sprints'] as const,
  list: (filter: SprintFilter = {}) => ['sprints', 'list', filter.board ?? '', filter.state ?? ''] as const,
  detail: (id: string) => ['sprints', 'detail', id] as const,
};

export function useSprints(filter: SprintFilter = {}) {
  const provider = useProvider();
  return useQuery<SprintSummary[]>({
    queryKey: sprintKeys.list(filter),
    queryFn: () => provider.listSprints(filter),
  });
}

export function useSprint(id: string | undefined) {
  const provider = useProvider();
  return useQuery<SprintView>({
    queryKey: sprintKeys.detail(id ?? ''),
    queryFn: () => provider.getSprint(id ?? ''),
    enabled: Boolean(id),
  });
}

/** Everything a sprint write invalidates: the sprint, the boards and the items. */
function useSprintInvalidation(): (result: SprintResult) => void {
  const queryClient = useQueryClient();
  return (result) => {
    queryClient.setQueryData(sprintKeys.detail(result.sprint.sprint.id), result.sprint);
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
  return useMutation<SprintResult, Error, SprintDraft>({
    mutationFn: (draft) => provider.createSprint(draft),
    onSuccess: settle,
  });
}

/** One planning edit: the goal, the dates or a reference in or out of the scope. */
export type SprintEdit = { id: string; patch: SprintPatch; rev?: string | undefined };

export function useUpdateSprint(): UseMutationResult<SprintResult, Error, SprintEdit> {
  const provider = useProvider();
  const settle = useSprintInvalidation();
  return useMutation<SprintResult, Error, SprintEdit>({
    mutationFn: (edit) => provider.updateSprint(edit.id, edit.patch, edit.rev),
    onSuccess: settle,
  });
}

export type SprintStart = { id: string; rev?: string | undefined; force?: boolean | undefined };

export function useStartSprint(): UseMutationResult<SprintResult, Error, SprintStart> {
  const provider = useProvider();
  const settle = useSprintInvalidation();
  return useMutation<SprintResult, Error, SprintStart>({
    mutationFn: (input) => provider.startSprint(input.id, input.rev, input.force),
    onSuccess: settle,
  });
}

export type SprintClose = { id: string; carry: SprintCarry[]; rev?: string | undefined };

export function useCloseSprint(): UseMutationResult<SprintResult, Error, SprintClose> {
  const provider = useProvider();
  const settle = useSprintInvalidation();
  return useMutation<SprintResult, Error, SprintClose>({
    mutationFn: (input) => provider.closeSprint(input.id, input.carry, input.rev),
    onSuccess: settle,
  });
}
