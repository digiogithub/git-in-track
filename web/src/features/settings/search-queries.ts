/**
 * The semantic-search settings as a query rather than as card state.
 *
 * The settings card edits these values, but the workspace list reads them too:
 * every repository row states whether semantic search is on for it
 * (GIT-US-0101). One cached query is what keeps the rows from each issuing its
 * own read — and a read here is not free, because the companion probes Pando
 * while answering it.
 *
 * It is gated on `searchSettings`: browser-only mode has no process to reach
 * Pando, and the provider there fails loudly by design.
 */

import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import type { SearchSettings } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';

export const searchSettingsKey = ['search', 'settings'] as const;

export function useSearchSettings(): UseQueryResult<SearchSettings | null, Error> {
  const provider = useOptionalProvider();
  const supported = provider?.capabilities.searchSettings === true;

  return useQuery<SearchSettings | null, Error>({
    queryKey: searchSettingsKey,
    queryFn: () => provider?.getSearchSettings() ?? Promise.resolve(null),
    enabled: supported,
    // An unreachable Pando is an answer the rows state, not a transient
    // failure to retry.
    retry: false,
  });
}
