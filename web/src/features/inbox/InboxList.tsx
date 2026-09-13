import type { Item } from '@/api/provider';
import { Button } from '@/components/ui/button';
import { InboxListItem } from '@/features/inbox/InboxListItem';
import type { InboxFilterName } from '@/features/inbox/search';

export type InboxListProps = {
  items: Item[];
  selectedId: string | null;
  filter: InboxFilterName;
  today: string;
  status: 'pending' | 'error' | 'success';
  error: Error | null;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onSelect: (id: string) => void;
  onLoadMore: () => void;
};

/** What an empty slice means, which is not the same sentence in all three. */
const emptyMessage: Record<InboxFilterName, string> = {
  pending: 'Nothing is waiting. The queue is clear.',
  snoozed: 'Nothing is snoozed. A snoozed submission comes back on its own date.',
  all: 'No submission has ever reached this inbox.',
};

/**
 * The queue itself: one page at a time, with an explicit "Load more".
 *
 * Pagination is incremental rather than infinite-scrolling on purpose — a
 * triage pass is a list you work down, and a list that grows under the cursor
 * is a list you lose your place in.
 */
export function InboxList({
  items,
  selectedId,
  filter,
  today,
  status,
  error,
  hasNextPage,
  isFetchingNextPage,
  onSelect,
  onLoadMore,
}: InboxListProps) {
  if (status === 'pending') {
    return <p className="px-3 py-2 text-sm text-muted-foreground">Loading the queue…</p>;
  }

  if (status === 'error') {
    return (
      <p role="alert" className="px-3 py-2 text-sm text-destructive">
        The queue could not be read: {error?.message ?? 'unknown error'}
      </p>
    );
  }

  if (items.length === 0) {
    return <p className="empty-state">{emptyMessage[filter]}</p>;
  }

  return (
    <div className="space-y-2">
      <ul className="space-y-1">
        {items.map((item) => (
          <InboxListItem
            key={item.id}
            item={item}
            today={today}
            selected={item.id === selectedId}
            onSelect={onSelect}
          />
        ))}
      </ul>
      {hasNextPage ? (
        <Button
          variant="outline"
          size="sm"
          className="w-full"
          disabled={isFetchingNextPage}
          onClick={onLoadMore}
        >
          {isFetchingNextPage ? 'Loading…' : 'Load more'}
        </Button>
      ) : null}
    </div>
  );
}
