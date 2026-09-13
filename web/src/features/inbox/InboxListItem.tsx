import { Clock, CopyCheck, Inbox as InboxIcon, X } from 'lucide-react';

import type { InboxStatus, Item } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { formatDate, typeName } from '@/features/backlog/item-meta';
import { effectiveInboxStatus } from '@/features/inbox/inbox-state';
import { cn } from '@/lib/cn';

export type InboxListItemProps = {
  item: Item;
  selected: boolean;
  /** The day the host resolves a snooze against; an arrived date reads as pending. */
  today: string;
  onSelect: (id: string) => void;
};

const stateBadge: Record<
  InboxStatus,
  { label: string; variant: 'default' | 'info' | 'warning' | 'success' | 'destructive' | 'outline' }
> = {
  pending: { label: 'Waiting', variant: 'info' },
  snoozed: { label: 'Snoozed', variant: 'warning' },
  accepted: { label: 'Accepted', variant: 'success' },
  rejected: { label: 'Rejected', variant: 'outline' },
  duplicate: { label: 'Duplicate', variant: 'outline' },
};

function StateIcon({ state }: { state: InboxStatus }) {
  if (state === 'snoozed') return <Clock aria-hidden="true" className="h-3.5 w-3.5" />;
  if (state === 'rejected') return <X aria-hidden="true" className="h-3.5 w-3.5" />;
  if (state === 'duplicate') return <CopyCheck aria-hidden="true" className="h-3.5 w-3.5" />;
  return <InboxIcon aria-hidden="true" className="h-3.5 w-3.5" />;
}

/** One row of the triage queue: what it is, where it came from and when. */
export function InboxListItem({ item, selected, today, onSelect }: InboxListItemProps) {
  const state = effectiveInboxStatus(item.inbox, today);
  const badge = stateBadge[state];
  const received = item.inbox?.received ?? item.created;

  return (
    <li>
      <button
        type="button"
        // The row is the navigation target of j/k, so the page can move the
        // selection without knowing anything about this component.
        data-inbox-row={item.id}
        aria-current={selected ? 'true' : undefined}
        onClick={() => {
          onSelect(item.id);
        }}
        className={cn(
          'w-full space-y-1 rounded-md border border-transparent px-3 py-2 text-left transition-colors duration-fast hover:bg-secondary',
          selected && 'border-border bg-accent-subtle',
        )}
      >
        <div className="flex items-center gap-2">
          <span className="font-mono text-2xs text-muted-foreground">{item.id}</span>
          <Badge variant={badge.variant} size="sm" className="ml-auto gap-1">
            <StateIcon state={state} />
            {badge.label}
          </Badge>
        </div>
        <p className="line-clamp-2 text-sm font-medium leading-snug">{item.title}</p>
        <p className="text-xs text-muted-foreground">
          {typeName(item.type)}
          {item.inbox?.source ? ` · via ${item.inbox.source}` : ''}
          {received ? ` · ${formatDate(received)}` : ''}
        </p>
      </button>
    </li>
  );
}
