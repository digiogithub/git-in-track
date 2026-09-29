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
import { Sparkles, SquareX } from 'lucide-react';
import { useEffect, useId, useState } from 'react';

import {
  ProviderError,
  type SearchCodeIndex,
  type SearchManagedInstance,
  type SearchSettings,
} from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
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

  // In managed mode the switch is the machine-local opt-in, not a reindex.
  if (data.mode === 'managed') {
    return (
      <ManagedToggle
        repoId={repoId}
        managed={data.indexed.find((entry) => entry.repo === repoId)?.managed}
      />
    );
  }

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

const MANAGED_TONES: Record<SearchManagedInstance['state'], BadgeProps['variant']> = {
  ready: 'success',
  starting: 'info',
  restarting: 'info',
  stopped: 'outline',
  disabled: 'outline',
  skipped: 'warning',
  failed: 'destructive',
};

/**
 * The opt-in switch of managed mode (GIT-US-0177, ADR-039). Enabling saves
 * `repos[].semanticSearch` and starts the repository's Pando; disabling stops
 * it and asks whether the index goes with it.
 */
function ManagedToggle({
  repoId,
  managed,
}: {
  repoId: string;
  managed: SearchManagedInstance | undefined;
}) {
  const provider = useOptionalProvider();
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const [asking, setAsking] = useState(false);
  const [deleteIndex, setDeleteIndex] = useState(false);
  const deleteId = useId();

  const save = useMutation({
    mutationFn: (opts: { enabled: boolean; deleteIndex?: boolean }) => {
      if (!provider) return Promise.reject(new Error('No data provider.'));
      return provider.setSemanticSearch(repoId, opts);
    },
    onMutate: () => {
      setError(null);
    },
    onSuccess: () => {
      setAsking(false);
      setDeleteIndex(false);
      void queryClient.invalidateQueries({ queryKey: searchSettingsKey });
    },
    onError: (cause: unknown) => {
      setAsking(false);
      setError(cause instanceof Error ? cause.message : String(cause));
    },
  });

  const optedIn = managed?.optedIn ?? false;
  const state = managed?.state ?? 'disabled';

  return (
    <div className="flex flex-col gap-1 text-xs">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={MANAGED_TONES[state]} size="sm" title={managed?.error}>
          Semantic search {state}
        </Badge>
        {optedIn ? (
          <Button
            variant="secondary"
            size="sm"
            disabled={save.isPending}
            onClick={() => {
              setAsking(true);
            }}
          >
            <SquareX aria-hidden="true" className="h-4 w-4" />
            Disable semantic search
          </Button>
        ) : (
          <Button
            variant="secondary"
            size="sm"
            disabled={save.isPending}
            onClick={() => {
              save.mutate({ enabled: true });
            }}
          >
            <Sparkles aria-hidden="true" className="h-4 w-4" />
            Enable semantic search
          </Button>
        )}
      </div>
      {managed?.state === 'skipped' && managed.error ? (
        <p className="text-muted-foreground">{managed.error}</p>
      ) : null}
      {error === null ? null : (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      <Dialog open={asking} onOpenChange={setAsking}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Disable semantic search for {repoId}?</DialogTitle>
            <DialogDescription>
              Its Pando instance stops. The index is kept on disk unless you delete it too, so
              enabling semantic search again is quick.
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2 text-sm">
            <Checkbox
              id={deleteId}
              checked={deleteIndex}
              onChange={(event) => {
                setDeleteIndex(event.target.checked);
              }}
            />
            <Label htmlFor={deleteId}>Also delete the index</Label>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setAsking(false);
              }}
            >
              Cancel
            </Button>
            <Button
              disabled={save.isPending}
              onClick={() => {
                save.mutate({ enabled: false, deleteIndex });
              }}
            >
              Disable
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
