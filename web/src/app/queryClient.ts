import { QueryClient } from '@tanstack/react-query';

/**
 * Query defaults follow docs/05-web-app.md §5: short stale times because the
 * companion WebSocket (or the worker change stream) invalidates precisely, and
 * focus refetching reserved for git status.
 */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 5_000,
        gcTime: 15 * 60_000,
        refetchOnWindowFocus: false,
        retry: 1,
      },
    },
  });
}
