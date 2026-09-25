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
  CoverageRow,
  GitRefs,
  ImpactQuery,
  ImpactResult,
  Item,
  RequirementDraft,
  RequirementList,
  RequirementPatch,
  RequirementRead,
  RequirementWriteResult,
  SpecTemplates,
  SyncRepoStatus,
  TracedRequirement,
} from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';

export const specKeys = {
  all: (project: string) => ['items', project, 'specs'] as const,
  list: (project: string) => ['items', project, 'specs', 'list'] as const,
  requirements: (project: string) => ['items', project, 'specs', 'requirements'] as const,
  coverage: (project: string) => ['items', project, 'specs', 'coverage'] as const,
  requirement: (project: string, ref: string) =>
    ['items', project, 'specs', 'requirement', ref] as const,
  trace: (project: string, ref: string) => ['items', project, 'specs', 'trace', ref] as const,
  coverageOf: (project: string, ref: string) =>
    ['items', project, 'specs', 'coverage-of', ref] as const,
  impact: (project: string, base: string, head: string) =>
    ['items', project, 'specs', 'impact', base, head] as const,
  templates: (project: string) => ['items', project, 'specs', 'templates'] as const,
};

/** `unavailable` is a state, never retried; anything else gets two more tries. */
function retryUnlessUnavailable(count: number, error: Error): boolean {
  return !(error instanceof ProviderError && error.code === 'unavailable') && count < 2;
}

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
    retry: retryUnlessUnavailable,
  });
}

/** One requirement with its text, its requirement `rev` (the write token) and its `blockRev`. */
export function useRequirement(project: string, ref: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: specKeys.requirement(project, ref),
    queryFn: (): Promise<RequirementRead> => provider.getRequirement(project, ref),
    enabled: project !== '' && ref !== '',
  });
}

/** The computed trace of one requirement; `unavailable` in browser-only mode. */
export function useRequirementTrace(project: string, ref: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: specKeys.trace(project, ref),
    queryFn: (): Promise<TracedRequirement> => provider.traceRequirement(project, ref),
    enabled: project !== '' && ref !== '',
    retry: retryUnlessUnavailable,
  });
}

/** The coverage row of one requirement, `undefined` when the answer names none (`untested`). */
export function useRequirementCoverage(project: string, ref: string) {
  const provider = useProvider();
  return useQuery({
    queryKey: specKeys.coverageOf(project, ref),
    queryFn: async (): Promise<CoverageRow | null> => {
      const list = await provider.listCoverage(project, { refs: [ref] });
      return list.coverage.find((row) => row.ref === ref) ?? null;
    },
    enabled: project !== '' && ref !== '',
    retry: retryUnlessUnavailable,
  });
}

/**
 * Patches one requirement under its requirement `rev` — never its `blockRev`
 * (ADR-037 §6, doc 03 §21.5). The caller handles `stale_revision`.
 */
export function useUpdateRequirement(project: string) {
  const provider = useProvider();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      ref,
      patch,
      rev,
    }: {
      ref: string;
      patch: RequirementPatch;
      rev: string;
    }): Promise<RequirementWriteResult> => provider.updateRequirement(project, ref, patch, rev),
    onSuccess: (result) => {
      queryClient.setQueryData<RequirementRead>(
        specKeys.requirement(project, result.requirement.ref),
        { requirement: result.requirement, specRev: result.specRev },
      );
      void queryClient.invalidateQueries({ queryKey: ['items', project] });
    },
  });
}

/**
 * The templates a new spec and a new requirement of the project start from
 * (ADR-038): the override files of `<docs>/.pmngr/templates/` when valid, the
 * embedded copies otherwise. A template file is not an item, so the answer is
 * re-read whenever a form that uses it mounts rather than kept.
 */
export function useSpecTemplates(project: string, enabled = true) {
  const provider = useProvider();
  return useQuery({
    queryKey: specKeys.templates(project),
    queryFn: (): Promise<SpecTemplates> => provider.getSpecTemplates(project),
    enabled: enabled && project !== '',
    staleTime: 0,
    retry: retryUnlessUnavailable,
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

/**
 * The requirements a diff affects (doc 03 §21.11). `head` undefined is the
 * working tree. `unavailable` — browser-only mode, or a repository without
 * git history — is a state, never retried.
 */
export function useImpact(project: string, query: Pick<ImpactQuery, 'base' | 'head'>) {
  const provider = useProvider();
  return useQuery({
    queryKey: specKeys.impact(project, query.base ?? '', query.head ?? ''),
    queryFn: (): Promise<ImpactResult> => provider.queryImpact(project, query),
    enabled: project !== '',
    retry: retryUnlessUnavailable,
  });
}

/**
 * The sync status of the project's repository, read only to seed the ref
 * pickers with its branch and upstream. A failure leaves the static
 * suggestions; it is never shown.
 */
export function useRefStatus(repoId: string | undefined, enabled: boolean) {
  const provider = useProvider();
  return useQuery({
    queryKey: ['sync', 'status', 'impact-refs', repoId ?? ''] as const,
    queryFn: (): Promise<SyncRepoStatus[]> => provider.getSyncStatus(repoId),
    enabled,
    retry: false,
    staleTime: 30_000,
  });
}

/**
 * The branches and recent commits of the project's repository (GIT-US-0149),
 * which the ref pickers offer. `unavailable` — browser-only mode, or a
 * repository without git history — leaves the pickers on their fallback
 * suggestions; it is a state, never retried, and never shown.
 */
export function useGitRefs(repoId: string | undefined, enabled: boolean) {
  const provider = useProvider();
  return useQuery({
    queryKey: ['git', 'refs', repoId ?? ''] as const,
    queryFn: (): Promise<GitRefs> => provider.listGitRefs(repoId ?? '', { limit: 20 }),
    enabled: enabled && repoId !== undefined && repoId !== '',
    retry: false,
    staleTime: 30_000,
  });
}
