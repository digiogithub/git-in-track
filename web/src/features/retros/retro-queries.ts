/**
 * TanStack Query hooks for retrospectives (docs/04-team-repository.md §9).
 *
 * Every retro write is one write to the retro file in the team repository, so a
 * mutation invalidates the retro and the retro index. Promoting an improvement
 * action also creates a task in a project repository, so it invalidates that
 * project's backlog too — the whole point of promotion is that the action stops
 * being a note and becomes work the board can see.
 */

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query';

import type {
  RetroFilter,
  RetroListing,
  RetroPatch,
  RetroDraft,
  RetroPromotion,
  RetroResult,
  RetroView,
} from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { backlogKeys } from '@/features/backlog/queries';
import { useActiveTeamKey } from '@/features/workspace/active-team';

/** Key factory. Every retro key lives under the `retros` prefix. */
export const retroKeys = {
  all: () => ['retros'] as const,
  list: (filter: RetroFilter = {}, team = '') =>
    ['retros', 'list', filter.sprint ?? '', filter.board ?? '', filter.state ?? '', team] as const,
  detail: (id: string, team = '') => ['retros', 'detail', id, team] as const,
};

export function useRetros(filter: RetroFilter = {}) {
  const provider = useProvider();
  const team = useActiveTeamKey();
  return useQuery<RetroListing>({
    queryKey: retroKeys.list(filter, team),
    queryFn: () => provider.listRetros(filter, team),
  });
}

export function useRetro(id: string | undefined) {
  const provider = useProvider();
  const team = useActiveTeamKey();
  return useQuery<RetroView>({
    queryKey: retroKeys.detail(id ?? '', team),
    queryFn: () => provider.getRetro(id ?? '', team),
    enabled: Boolean(id),
  });
}

/** Everything a retro write invalidates: the retro, the index and the tasks. */
function useRetroInvalidation(): (result: RetroResult) => void {
  const queryClient = useQueryClient();
  const team = useActiveTeamKey();
  return (result) => {
    queryClient.setQueryData(retroKeys.detail(result.retro.retro.id, team), result.retro);
    void queryClient.invalidateQueries({ queryKey: retroKeys.all() });
    if (result.task) {
      void queryClient.invalidateQueries({ queryKey: backlogKeys.all() });
    }
  };
}

export function useCreateRetro(): UseMutationResult<RetroResult, Error, RetroDraft> {
  const provider = useProvider();
  const settle = useRetroInvalidation();
  const team = useActiveTeamKey();
  return useMutation<RetroResult, Error, RetroDraft>({
    mutationFn: (draft) => provider.createRetro(draft, team),
    onSuccess: settle,
  });
}

/** One session edit: a note, a grouping, a ballot or an improvement action. */
export type RetroEdit = { id: string; patch: RetroPatch; rev?: string | undefined };

export function useUpdateRetro(): UseMutationResult<RetroResult, Error, RetroEdit> {
  const provider = useProvider();
  const settle = useRetroInvalidation();
  const team = useActiveTeamKey();
  return useMutation<RetroResult, Error, RetroEdit>({
    mutationFn: (edit) => provider.updateRetro(edit.id, edit.patch, edit.rev, team),
    onSuccess: settle,
  });
}

export function usePromoteRetroAction(): UseMutationResult<RetroResult, Error, RetroPromotion> {
  const provider = useProvider();
  const settle = useRetroInvalidation();
  const team = useActiveTeamKey();
  return useMutation<RetroResult, Error, RetroPromotion>({
    mutationFn: (input) => provider.promoteRetroAction(input, team),
    onSuccess: settle,
  });
}
