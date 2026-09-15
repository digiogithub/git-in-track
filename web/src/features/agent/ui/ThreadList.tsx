/**
 * The conversation list (task GIT-T-0062).
 *
 * The rows are a merge of the adapter's thread list and the titles kept
 * locally; `mergeThreadRows` in `model.ts` does that, so it can be tested
 * without a DOM and this file stays presentation.
 *
 * Deleting is destructive and unrecoverable — the transcript lives on the
 * server and `DELETE /threads/{id}` is the end of it — so the row asks once.
 */

import { MessageSquare, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { Button } from '@/components/ui/button';
import type { ThreadRow } from '@/features/agent/ui/model';
import { cn } from '@/lib/cn';

export type ThreadListProps = {
  rows: readonly ThreadRow[];
  activeId: string | null;
  onNew: () => void;
  onSelect: (id: string) => void;
  onDelete: (id: string) => void;
};

export function ThreadList({ rows, activeId, onNew, onSelect, onDelete }: ThreadListProps) {
  const [confirming, setConfirming] = useState<string | null>(null);

  return (
    <nav aria-label="Conversations" className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between gap-2 px-3 pb-2 pt-3">
        <h2 className="section-label">Conversations</h2>
        <Button variant="ghost" size="icon-sm" aria-label="New conversation" onClick={onNew}>
          <Plus aria-hidden="true" className="h-4 w-4" />
        </Button>
      </div>

      {rows.length === 0 ? (
        <p className="px-3 text-xs text-muted-foreground">No conversation yet.</p>
      ) : (
        <ul className="min-h-0 flex-1 space-y-0.5 overflow-y-auto px-2 pb-3">
          {rows.map((row) => {
            const active = row.id === activeId;
            return (
              <li key={row.id} className="group relative">
                <button
                  type="button"
                  aria-current={active ? 'true' : undefined}
                  onClick={() => onSelect(row.id)}
                  className={cn(
                    'flex w-full items-center gap-2 rounded-md py-1.5 pl-2.5 pr-8 text-left text-sm text-muted-foreground transition-colors duration-fast hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                    active && 'bg-accent-subtle font-medium text-foreground',
                  )}
                >
                  <MessageSquare
                    aria-hidden="true"
                    className={cn('h-3.5 w-3.5 shrink-0', active && 'text-accent')}
                  />
                  <span className="min-w-0 flex-1 truncate" title={row.title}>
                    {row.title}
                  </span>
                  {row.running === true ? (
                    <span
                      aria-label="Running"
                      title="A run is still attached"
                      className="h-1.5 w-1.5 shrink-0 rounded-full bg-accent"
                    />
                  ) : null}
                </button>
                <button
                  type="button"
                  aria-label={
                    confirming === row.id ? `Confirm deleting ${row.title}` : `Delete ${row.title}`
                  }
                  onClick={() => {
                    if (confirming === row.id) {
                      setConfirming(null);
                      onDelete(row.id);
                      return;
                    }
                    setConfirming(row.id);
                  }}
                  onBlur={() => setConfirming((id) => (id === row.id ? null : id))}
                  className={cn(
                    'absolute right-1 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground transition-opacity duration-fast hover:bg-secondary hover:text-destructive focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                    confirming === row.id
                      ? 'text-destructive opacity-100'
                      : 'opacity-0 group-hover:opacity-100',
                  )}
                >
                  <Trash2 aria-hidden="true" className="h-3.5 w-3.5" />
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </nav>
  );
}
