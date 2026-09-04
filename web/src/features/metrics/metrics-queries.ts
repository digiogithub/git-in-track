/**
 * TanStack Query hooks for sprint metrics (docs/04-team-repository.md §12).
 *
 * Metrics are derived and read-only: there is nothing to mutate and therefore
 * nothing to invalidate here. The series itself is reconstructed on the host —
 * from git in the companion, from the `updated` stamps in the browser — and the
 * answer always carries the provenance that says which of the two it is.
 */

import { useQuery } from '@tanstack/react-query';

import type { SprintMetricsView } from '@/api/provider';
import { useProvider } from '@/api/provider-context';

/** Key factory. Every metrics key lives under the `metrics` prefix. */
export const metricsKeys = {
  all: () => ['metrics'] as const,
  sprint: (id: string) => ['metrics', 'sprint', id] as const,
};

export function useSprintMetrics(id: string | undefined) {
  const provider = useProvider();
  return useQuery<SprintMetricsView>({
    queryKey: metricsKeys.sprint(id ?? ''),
    queryFn: () => provider.getSprintMetrics(id ?? ''),
    enabled: Boolean(id),
  });
}
