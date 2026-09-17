/**
 * Semantic search for one repository row of the workspace list (GIT-US-0101).
 *
 * The row states whether Pando indexes the repository and, when it does not,
 * offers to switch it on right there. "Switch on" is a reindex scoped to this
 * repository — `POST /search/reindex` with `{repo}` — which registers it with
 * Pando as a code project and refreshes the knowledge base; it never reindexes
 * the rest of the workspace.
 *
 * A companion with no Pando at all has nothing to switch on, so the control is
 * a link to the settings card instead. Browser-only mode has no settings
 * surface, and there the control is absent rather than disabled.
 *
 * Progress arrives through the provider's event seam, exactly as in the
 * settings card: the row never polls, and a terminal frame of the job it
 * started is the cue to re-read the settings the badge is drawn from.
 */

import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Sparkles } from 'lucide-react';
import { useEffect, useState } from 'react';

import { ProviderError, type SearchCodeIndex, type SearchSettings } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { searchSettingsKey, useSearchSettings } from '@/features/settings/search-queries';

type RowState = 'on' | 'indexing' | 'off' | 'unavailable';

const TONES: Record<RowState, BadgeProps['variant']> = {
  on: 'success',
  indexing: 'info',
  off: 'outline',
  unavailable: 'destructive',
};

/** What the badge says for a registration; `registered` is simply "on". */
function stateOf(code: SearchCodeIndex['status']): RowState {
  return code === 'registered' ? 'on' : code;
}

function enableMessage(error: unknown): string {
  if (error instanceof ProviderError && error.code === 'search_reindex_running') {
    return 'A reindex is already running; try again when it finishes.';
  }
  return error instanceof Error ? error.message : String(error);
}

export function RepoSemanticSearch({ repoId }: { repoId: string }) {
  const provider = useOptionalProvider();
  if (!provider?.capabilities.searchSettings) return null;
  return <RepoSemanticSearchControl repoId={repoId} />;
}

function RepoSemanticSearchControl({ repoId }: { repoId: string }) {
  const provider = useOptionalProvider();
  const queryClient = useQueryClient();
  const settings = useSearchSettings();
  /** The job this row started, while it runs. */
  const [jobId, setJobId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!provider || jobId === null) return undefined;
    return provider.subscribe((event) => {
      if (event.kind !== 'searchProgress' || event.operationId !== jobId) return;
      if (event.phase === 'completed' || event.phase === 'failed') {
        setJobId(null);
        void queryClient.invalidateQueries({ queryKey: searchSettingsKey });
      }
    });
  }, [provider, jobId, queryClient]);

  const enable = useMutation({
    mutationFn: () => {
      if (!provider) return Promise.reject(new Error('No data provider.'));
      return provider.reindexSearch(repoId);
    },
    onMutate: () => {
      setError(null);
    },
    onSuccess: (job) => {
      setJobId(job.jobId);
      queryClient.setQueryData<SearchSettings | null>(searchSettingsKey, (current) =>
        current ? { ...current, reindex: job } : current,
      );
    },
    onError: (cause: unknown) => {
      setError(enableMessage(cause));
    },
  });

  const data = settings.data;
  if (!data) return null;

  if (!data.configured) {
    return (
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <Badge variant={TONES.off} size="sm">
          Semantic search off
        </Badge>
        <Link to="/settings" hash="semantic-search" className="text-accent underline">
          Set up semantic search
        </Link>
      </div>
    );
  }

  const row = data.indexed.find((entry) => entry.repo === repoId);
  // A repository the registration has not reached yet has no state to show.
  if (row?.code === undefined && jobId === null) return null;

  const running = jobId !== null || enable.isPending;
  const state: RowState = running ? 'indexing' : stateOf(row?.code?.status ?? 'off');
  const canEnable = !running && (state === 'off' || state === 'unavailable');

  return (
    <div className="flex flex-col gap-1 text-xs">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={TONES[state]} size="sm" title={row?.code?.note}>
          Semantic search {state}
        </Badge>
        {canEnable ? (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              enable.mutate();
            }}
          >
            <Sparkles aria-hidden="true" className="h-4 w-4" />
            Enable semantic search
          </Button>
        ) : null}
      </div>
      {error === null ? null : (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
