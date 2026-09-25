/**
 * TanStack Query hooks for the specs screens (docs/05-web-app.md §3.1).
 *
 * Every key lives under `['items', <project>, 'specs', …]`: a requirement write
 * and a spec edited on disk both arrive as an `items` change carrying the spec
 * id, and `useBacklogEvents` already invalidates that whole project prefix.
 */

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import type {
  CoverageList,
  Item,
  RequirementDraft,
  RequirementList,
  RequirementWriteResult,
} from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';

export const specKeys = {
  all: (project: string) => ['items', project, 'specs'] as const,
  list: (project: string) => ['items', project, 'specs', 'list'] as const,
  requirements: (project: string) => ['items', project, 'specs', 'requirements'] as const,
  coverage: (project: string) => ['items', project, 'specs', 'coverage'] as const,
};

/** Specs are few; the page reads every one, walking the cursor. */
const SPEC_PAGE = 500;

export function useSpecs(project: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: specKeys.list(project),
    queryFn: async (): Promise<Item[]> => {
      const specs: Item[] = [];
      let cursor: string | undefined;
      do {
        const page = await provider.listSpecs(project, {
          limit: SPEC_PAGE,
          ...(cursor ? { cursor } : {}),
        });
        specs.push(...page.items);
        cursor = page.nextCursor;
      } while (cursor);
      return specs;
    },
    enabled: project !== '',
  });
}

export function useRequirements(project: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: specKeys.requirements(project),
    queryFn: (): Promise<RequirementList> => provider.listRequirements(project),
    enabled: project !== '',
  });
}

/**
 * Coverage rows. `unavailable` — browser-only mode — is a state, not an error
 * (docs/05 §4): it is never retried and the page renders it as a hint.
 */
export function useCoverage(project: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: specKeys.coverage(project),
    queryFn: (): Promise<CoverageList> => provider.listCoverage(project),
    enabled: project !== '',
    retry: (count, error) =>
      !(error instanceof ProviderError && error.code === 'unavailable') && count < 2,
  });
}

/** Appends one requirement to a spec; the core allocates `R<n>`. */
export function useCreateRequirement(project: string) {
  const provider = useProvider();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (draft: RequirementDraft): Promise<RequirementWriteResult> =>
      provider.createRequirement(project, draft),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['items', project] });
    },
  });
}
