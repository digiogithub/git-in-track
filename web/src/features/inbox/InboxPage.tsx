import { useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import type { InboxTriageInput, Item } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import { useProject } from '@/features/backlog/queries';
import { ConflictDialog } from '@/features/editor/ConflictDialog';
import { InboxActions, type InboxDialog } from '@/features/inbox/InboxActions';
import { InboxDetail } from '@/features/inbox/InboxDetail';
import { InboxList } from '@/features/inbox/InboxList';
import { useInbox, useInboxEvents, useTriageInboxItem } from '@/features/inbox/queries';
import {
  hasTriageStatus,
  inboxFilterName,
  inboxFilters,
  neighbour,
  nextAfterTriage,
  parseInboxSearch,
  toInboxFilter,
  type InboxFilterName,
  type InboxSearchInput,
} from '@/features/inbox/search';
import { cn } from '@/lib/cn';

const EMPTY_ITEMS: Item[] = [];

const filterLabel: Record<InboxFilterName, string> = {
  pending: 'Waiting',
  snoozed: 'Snoozed',
  all: 'All',
};

type SearchRecord = Record<string, unknown>;

type NavigateWithSearch = (options: {
  search: (prev: SearchRecord) => SearchRecord;
  replace?: boolean;
}) => void;

/**
 * The triage queue: the list on the left, the submission on the right, and one
 * decision per row.
 *
 * Two rules shape the whole screen. The first is that the next row is computed
 * *before* the decision is sent, so the pane never blinks empty between the
 * write and the refetch. The second is that everything a person can see —
 * which slice, which row — is in the URL, so a half-finished pass is a link.
 */
export function InboxPage() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const rawSearch = useSearch({ strict: false });
  const search = useMemo(() => parseInboxSearch(rawSearch), [rawSearch]);
  const navigate = useNavigate() as unknown as NavigateWithSearch;
  const provider = useProvider();
  const { toast } = useToast();

  const projectQuery = useProject(projectKey);
  const project = projectQuery.data;
  // The project list is what says whether this project has an inbox at all, so
  // nothing is asked of the provider until it has answered.
  const gated = projectQuery.isSuccess && !hasTriageStatus(project);

  const filterName = inboxFilterName(search);
  const filter = useMemo(() => toInboxFilter(search, { project: projectKey }), [search, projectKey]);
  const queue = useInbox(filter, projectQuery.isSuccess && !gated);
  const triage = useTriageInboxItem(projectKey, filter);
  useInboxEvents(projectKey);

  const items = useMemo(
    () => queue.data?.pages.flatMap((page) => page.items) ?? EMPTY_ITEMS,
    [queue.data],
  );
  const ids = useMemo(() => items.map((item) => item.id), [items]);
  const counts = queue.data?.pages[0]?.counts ?? {};
  const total = queue.data?.pages[0]?.total ?? 0;

  const [conflictId, setConflictId] = useState<string | null>(null);
  const [dialog, setDialog] = useState<InboxDialog>(null);

  const setSearch = useCallback(
    (patch: InboxSearchInput) => {
      navigate({
        search: (prev: SearchRecord) => {
          const next: SearchRecord = { ...prev, ...patch };
          for (const [key, value] of Object.entries(next)) {
            if (value === undefined || value === '') delete next[key];
          }
          return next;
        },
        replace: true,
      });
    },
    [navigate],
  );

  // The selected row is URL state, but a queue that has moved on must not leave
  // the pane pointing at a row that is no longer listed.
  const selectedId =
    search.selected && ids.includes(search.selected) ? search.selected : (ids[0] ?? null);
  const selected = items.find((item) => item.id === selectedId) ?? null;

  const select = useCallback(
    (id: string | null) => {
      setSearch({ selected: id ?? undefined });
    },
    [setSearch],
  );

  const move = useCallback(
    (delta: 1 | -1) => {
      if (selectedId === null) return;
      const target = neighbour(ids, selectedId, delta);
      if (target !== null) select(target);
    },
    [ids, selectedId, select],
  );

  const decide = useCallback(
    (input: Omit<InboxTriageInput, 'id' | 'rev'>) => {
      if (!selected) return;
      setDialog(null);
      // Computed first, from the list as it stands: after the write this row is
      // gone and there is no neighbour left to find.
      const after = nextAfterTriage(ids, selected.id);
      triage.mutate(
        { id: selected.id, rev: selected.rev, ...input },
        {
          onSuccess: (result) => {
            toast({ title: decisionTitle(result.action, selected.id) });
            select(after);
          },
          onError: (error) => {
            if (error instanceof ProviderError && error.code === 'stale_revision') {
              setConflictId(selected.id);
              return;
            }
            toast({
              variant: 'destructive',
              title: `${selected.id} was not triaged`,
              description: error.message,
            });
          },
        },
      );
    },
    [ids, selected, triage, toast, select],
  );

  // Accepting is an edit, not a one-click state change: the person still has to
  // pick a status and a parent, so it opens the form rather than writing here.
  const openAccept = useNavigateToAccept(projectKey);
  const onAccept = useCallback(() => {
    if (!selected) return;
    openAccept(selected.id);
  }, [openAccept, selected]);

  useKeyboardTriage({
    enabled: selected !== null && !triage.isPending,
    onMove: move,
    onAccept,
    onReject: () => {
      setDialog('reject');
    },
    onSnooze: () => {
      setDialog('snooze');
    },
  });

  if (projectQuery.isPending) {
    return <p className="text-sm text-muted-foreground">Loading {projectKey}…</p>;
  }

  if (gated) {
    return (
      <div className="space-y-3">
        <h1 className="page-title">Inbox</h1>
        <p className="empty-state">
          {projectKey} declares no status in the <code className="font-mono">triage</code>{' '}
          category, so it has no inbox. Add one to <code className="font-mono">project.yaml</code>{' '}
          to start collecting submissions.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="page-title">Inbox</h1>
          <p className="text-sm text-muted-foreground">
            {counts.pending ?? 0} waiting · {counts.snoozed ?? 0} snoozed · {total} in this view
          </p>
        </div>
        <nav aria-label="Triage filter" className="flex gap-1">
          {inboxFilters.map((name) => (
            <Button
              key={name}
              size="sm"
              variant={name === filterName ? 'secondary' : 'ghost'}
              aria-pressed={name === filterName}
              onClick={() => {
                // Changing the slice drops the selection: the row that was open
                // may not be in the new one, and the first row of the new slice
                // is where a pass continues.
                setSearch({ filter: name === 'pending' ? undefined : name, selected: undefined });
              }}
            >
              {filterLabel[name]}
            </Button>
          ))}
        </nav>
      </header>

      <div className="grid gap-5 lg:grid-cols-[minmax(16rem,22rem)_minmax(0,1fr)]">
        <section
          aria-label="Triage queue"
          className={cn(
            'max-h-[calc(100vh-12rem)] overflow-y-auto rounded-lg border border-border bg-surface p-2 shadow-card',
          )}
        >
          <InboxList
            items={items}
            selectedId={selectedId}
            filter={filterName}
            today={today()}
            status={queue.status}
            error={queue.error}
            hasNextPage={queue.hasNextPage}
            isFetchingNextPage={queue.isFetchingNextPage}
            onSelect={select}
            onLoadMore={() => {
              void queue.fetchNextPage();
            }}
          />
        </section>

        <section
          aria-label="Submission"
          className="space-y-4 rounded-lg border border-border bg-surface p-5 shadow-card"
        >
          {selected === null ? (
            <p className="empty-state">
              {queue.isPending ? 'Loading the queue…' : 'Pick a submission to triage.'}
            </p>
          ) : (
            <>
              <InboxActions
                item={selected}
                projectKey={projectKey}
                busy={triage.isPending}
                canWrite={provider.capabilities.write}
                hasPrevious={neighbour(ids, selected.id, -1) !== null}
                hasNext={neighbour(ids, selected.id, 1) !== null}
                dialog={dialog}
                onDialogChange={setDialog}
                onAccept={onAccept}
                onReject={() => {
                  decide({ action: 'reject' });
                }}
                onSnooze={(until) => {
                  decide({ action: 'snooze', snoozedUntil: until });
                }}
                onDuplicate={(target) => {
                  decide({ action: 'duplicate', duplicateOf: target });
                }}
                onMove={move}
              />
              <InboxDetail
                item={selected}
                projectKey={projectKey}
                project={project}
                today={today()}
              />
            </>
          )}
        </section>
      </div>

      {conflictId !== null ? (
        <ConflictDialog
          itemId={conflictId}
          onReload={() => {
            void queue.refetch();
            setConflictId(null);
          }}
          onOverwrite={() => {
            // Triage is not a text edit, so "mine" is simply the same decision
            // taken against whatever the file holds now.
            setConflictId(null);
            void queue.refetch();
          }}
          onCancel={() => {
            setConflictId(null);
          }}
        />
      ) : null}
    </div>
  );
}

/** Today, as the host's snooze comparison sees it. */
function today(): string {
  return new Date().toISOString().slice(0, 10);
}

function decisionTitle(action: string, id: string): string {
  if (action === 'accept') return `${id} accepted`;
  if (action === 'reject') return `${id} rejected`;
  if (action === 'snooze') return `${id} snoozed`;
  return `${id} marked a duplicate`;
}

/** Navigates to the accept form for one submission. */
function useNavigateToAccept(projectKey: string): (id: string) => void {
  const navigate = useNavigate();
  return useCallback(
    (id: string) => {
      void navigate({ to: '/p/$project/inbox/$id/accept', params: { project: projectKey, id } });
    },
    [navigate, projectKey],
  );
}

type KeyboardTriageOptions = {
  enabled: boolean;
  onMove: (delta: 1 | -1) => void;
  onAccept: () => void;
  onReject: () => void;
  onSnooze: () => void;
};

/**
 * `j`/`k` to walk the queue, `a`/`r`/`s` to decide.
 *
 * Keys are ignored while the focus is in a field or a dialog: someone typing a
 * snooze date into the picker is not asking to reject the row behind it.
 */
function useKeyboardTriage({ enabled, onMove, onAccept, onReject, onSnooze }: KeyboardTriageOptions) {
  const handlers = useRef({ onMove, onAccept, onReject, onSnooze });
  handlers.current = { onMove, onAccept, onReject, onSnooze };

  useEffect(() => {
    if (!enabled) return undefined;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]')) {
        return;
      }
      const api = handlers.current;
      switch (event.key) {
        case 'j':
          event.preventDefault();
          api.onMove(1);
          return;
        case 'k':
          event.preventDefault();
          api.onMove(-1);
          return;
        case 'a':
          event.preventDefault();
          api.onAccept();
          return;
        case 'r':
          event.preventDefault();
          api.onReject();
          return;
        case 's':
          event.preventDefault();
          api.onSnooze();
          return;
        default:
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [enabled]);
}
