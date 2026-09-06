import { useEffect, useRef } from 'react';

import type { Item, ItemReference } from '@/api/provider';
import { Button } from '@/components/ui/button';
import { ItemLink } from '@/features/backlog/Badges';
import { useItemReferences } from '@/features/backlog/queries';

export type DeleteItemDialogProps = {
  item: Item;
  projectKey: string;
  busy?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
};

/** "board delivery (order.in_progress)" — where a reference sits, in words. */
function describe(reference: ItemReference): string {
  const name = reference.title ? `${reference.id} — ${reference.title}` : reference.id;
  return `${reference.kind} ${name} (${reference.field})`;
}

/**
 * Confirms a delete, after showing what still points at the item: children,
 * milestones, typed links, board cards, sprint scopes and promoted retro
 * actions (docs/05-web-app.md §8.4).
 *
 * The delete itself is soft (ADR-026): the file keeps its id and its history
 * with `deleted: true`, so nothing that referenced it dangles — a child's
 * `parent` still resolves, to an item marked deleted. That is why the list is a
 * warning and not a refusal.
 */
export function DeleteItemDialog({
  item,
  projectKey,
  busy = false,
  error = null,
  onConfirm,
  onCancel,
}: DeleteItemDialogProps) {
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const references = useItemReferences(projectKey, item.id, true);

  useEffect(() => {
    cancelRef.current?.focus();
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onCancel();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [onCancel]);

  const inbound = references.data?.references ?? [];
  const children = references.data?.children ?? [];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="delete-item-title"
        aria-describedby="delete-item-description"
        className="max-h-[80vh] w-full max-w-lg space-y-4 overflow-y-auto rounded-lg border border-border bg-card p-5 text-card-foreground shadow-overlay"
      >
        <h2 id="delete-item-title" className="text-base font-semibold">
          Delete {item.id}?
        </h2>
        <p id="delete-item-description" className="text-sm text-muted-foreground">
          The file keeps its id and its history and is marked <code>deleted: true</code>, so the id
          is never reused and nothing that points at it is left dangling. It disappears from lists,
          boards and search.
        </p>

        {references.isPending ? (
          <p className="text-sm text-muted-foreground">Looking for inbound references…</p>
        ) : references.isError ? (
          <p className="text-sm text-destructive" role="alert">
            The inbound references could not be read:{' '}
            {references.error instanceof Error ? references.error.message : 'unknown error'}. Delete
            anyway only if you know what points at this item.
          </p>
        ) : inbound.length === 0 ? (
          <p className="text-sm">Nothing else points at this item.</p>
        ) : (
          <div className="space-y-2">
            <p className="text-sm font-medium">
              {inbound.length} reference{inbound.length === 1 ? '' : 's'} point
              {inbound.length === 1 ? 's' : ''} at {item.id}:
            </p>
            <ul aria-label="Inbound references" className="space-y-1 text-sm">
              {inbound.map((reference) => (
                <li key={`${reference.kind}-${reference.id}-${reference.field}`}>
                  {reference.kind === 'item' ? (
                    <span>
                      <ItemLink project={projectKey} id={reference.id} />{' '}
                      <span className="text-muted-foreground">
                        {reference.title} ({reference.field})
                      </span>
                    </span>
                  ) : (
                    <span className="text-muted-foreground">{describe(reference)}</span>
                  )}
                </li>
              ))}
            </ul>
            {children.length > 0 ? (
              <p className="text-sm text-destructive">
                {children.length} item{children.length === 1 ? '' : 's'} name{' '}
                {children.length === 1 ? 's' : ''} this one as their parent. Re-parent them, or they
                stay under a deleted item.
              </p>
            ) : null}
          </div>
        )}

        {error ? (
          <p className="text-sm text-destructive" role="alert">
            {error}
          </p>
        ) : null}

        <div className="flex flex-wrap justify-end gap-2">
          <Button ref={cancelRef} variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="destructive" disabled={busy} onClick={onConfirm}>
            {busy ? 'Deleting…' : 'Delete item'}
          </Button>
        </div>
      </div>
    </div>
  );
}
