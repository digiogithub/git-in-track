import { useState } from 'react';

import type { Item, ProjectSummary } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Select } from '@/components/ui/select';
import { useToast } from '@/components/ui/toast';
import { useMoveItem } from '@/features/backlog/queries';

export type BulkMoveBarProps = {
  projectKey: string;
  project: ProjectSummary | undefined;
  selected: Item[];
  canWrite: boolean;
  onDone: () => void;
};

/**
 * Bulk status change for the selected rows.
 *
 * Each row is moved with the revision the table is showing, so an item edited
 * elsewhere fails with `stale_revision` and is reported instead of being
 * silently overwritten.
 */
export function BulkMoveBar({ projectKey, project, selected, canWrite, onDone }: BulkMoveBarProps) {
  const [status, setStatus] = useState('');
  const [confirming, setConfirming] = useState(false);
  const [running, setRunning] = useState(false);
  const move = useMoveItem(projectKey);
  const { toast } = useToast();

  if (selected.length === 0) return null;

  const statuses = project?.statuses ?? [];

  const apply = async () => {
    if (!status) return;
    setRunning(true);
    const stale: string[] = [];
    let applied = 0;

    for (const item of selected) {
      try {
        await move.mutateAsync({ id: item.id, status, rev: item.rev });
        applied += 1;
      } catch (error) {
        if (error instanceof ProviderError && error.code === 'stale_revision') stale.push(item.id);
        else
          toast({
            variant: 'destructive',
            title: `Could not move ${item.id}`,
            description: error instanceof Error ? error.message : String(error),
          });
      }
    }

    setRunning(false);
    setConfirming(false);

    if (stale.length > 0) {
      toast({
        variant: 'destructive',
        title: 'Changed on disk',
        description: `${stale.join(', ')} changed since this list was loaded. The list has been refreshed — try again.`,
      });
    }
    if (applied > 0) {
      toast({ title: `Moved ${applied} item${applied === 1 ? '' : 's'}` });
    }
    onDone();
  };

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-secondary/50 px-3 py-2">
      <p className="text-sm font-medium">
        {selected.length} selected
        <span className="sr-only"> items</span>
      </p>
      <label htmlFor="bulk-status" className="text-sm text-muted-foreground">
        Move status to
      </label>
      <Select
        id="bulk-status"
        className="w-44"
        value={status}
        disabled={!canWrite}
        onChange={(event) => {
          setStatus(event.target.value);
        }}
      >
        <option value="">Choose a status…</option>
        {statuses.map((entry) => (
          <option key={entry.id} value={entry.id}>
            {entry.name}
          </option>
        ))}
      </Select>
      <Button
        size="sm"
        disabled={!canWrite || status === ''}
        onClick={() => {
          setConfirming(true);
        }}
      >
        Move
      </Button>
      <Button variant="ghost" size="sm" onClick={onDone}>
        Clear selection
      </Button>
      {!canWrite ? (
        <p className="text-xs text-muted-foreground">This workspace is read-only.</p>
      ) : null}

      <Dialog
        open={confirming}
        onOpenChange={(open) => {
          if (!running) setConfirming(open);
        }}
      >
        <DialogContent aria-describedby="bulk-move-description">
          <DialogHeader>
            <DialogTitle>Move {selected.length} items</DialogTitle>
            <DialogDescription id="bulk-move-description">
              Each item is written with the revision currently shown. Items changed on disk in the
              meantime are skipped and reported.
            </DialogDescription>
          </DialogHeader>
          <ul className="max-h-48 space-y-1 overflow-y-auto text-sm">
            {selected.map((item) => (
              <li key={item.id} className="font-mono text-xs">
                {item.id} <span className="font-sans text-muted-foreground">{item.title}</span>
              </li>
            ))}
          </ul>
          <DialogFooter>
            <Button
              variant="outline"
              size="sm"
              disabled={running}
              onClick={() => {
                setConfirming(false);
              }}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              disabled={running}
              onClick={() => {
                void apply();
              }}
            >
              {running ? 'Moving…' : 'Move items'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
