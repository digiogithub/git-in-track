import { Check, ChevronDown, ChevronUp, Clock, CopyCheck, X } from 'lucide-react';
import { useEffect, useState } from 'react';

import type { InboxTriageAction, Item } from '@/api/provider';
import { ItemPicker } from '@/components/editor/ItemPicker';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

export type InboxActionsProps = {
  item: Item;
  projectKey: string;
  /** Disabled while a decision is in flight, and in a read-only workspace. */
  busy: boolean;
  canWrite: boolean;
  hasPrevious: boolean;
  hasNext: boolean;
  /**
   * The open dialog, owned by the page: the `s` shortcut has to be able to open
   * the snooze dialog, and a keyboard handler that reached into a child's state
   * through the DOM would be a second source of truth.
   */
  dialog: InboxDialog;
  onDialogChange: (dialog: InboxDialog) => void;
  onAccept: () => void;
  onReject: () => void;
  onSnooze: (until: string) => void;
  onDuplicate: (target: string) => void;
  onMove: (delta: 1 | -1) => void;
};

/** Which small dialog is open, if any. The page owns it, so `s` can open one. */
export type InboxDialog = Exclude<InboxTriageAction, 'accept'> | null;

/** Tomorrow, as the default a snooze offers: the smallest useful "not now". */
function defaultSnoozeDate(): string {
  return new Date(Date.now() + 86_400_000).toISOString().slice(0, 10);
}

/**
 * The triage action bar.
 *
 * Accept is the one accent button on the screen, because accepting is what the
 * queue is for; the three ways of saying "not into the backlog" are quieter and
 * each asks for the one thing it needs before it writes.
 */
export function InboxActions({
  item,
  projectKey,
  busy,
  canWrite,
  hasPrevious,
  hasNext,
  dialog,
  onDialogChange,
  onAccept,
  onReject,
  onSnooze,
  onDuplicate,
  onMove,
}: InboxActionsProps) {
  const [until, setUntil] = useState(defaultSnoozeDate);
  const [duplicateOf, setDuplicateOf] = useState<string | null>(null);

  // Every submission is decided about on its own terms: a date typed for one
  // row must not still be sitting in the dialog when the next row opens it.
  useEffect(() => {
    setUntil(defaultSnoozeDate());
    setDuplicateOf(null);
  }, [item.id]);

  const disabled = busy || !canWrite;

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button variant="accent" disabled={disabled} onClick={onAccept}>
        <Check aria-hidden="true" className="h-4 w-4" />
        Accept
        <kbd aria-hidden="true" className="ml-1 font-mono text-2xs opacity-70">
          a
        </kbd>
      </Button>
      <Button
        variant="outline"
        disabled={disabled}
        onClick={() => {
          onDialogChange('reject');
        }}
      >
        <X aria-hidden="true" className="h-4 w-4" />
        Reject
        <kbd aria-hidden="true" className="ml-1 font-mono text-2xs opacity-70">
          r
        </kbd>
      </Button>
      <Button
        variant="outline"
        disabled={disabled}
        onClick={() => {
          onDialogChange('snooze');
        }}
      >
        <Clock aria-hidden="true" className="h-4 w-4" />
        Snooze
        <kbd aria-hidden="true" className="ml-1 font-mono text-2xs opacity-70">
          s
        </kbd>
      </Button>
      <Button
        variant="outline"
        disabled={disabled}
        onClick={() => {
          onDialogChange('duplicate');
        }}
      >
        <CopyCheck aria-hidden="true" className="h-4 w-4" />
        Duplicate
      </Button>

      <div className="ml-auto flex items-center gap-1">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Previous submission"
          disabled={!hasPrevious}
          onClick={() => {
            onMove(-1);
          }}
        >
          <ChevronUp aria-hidden="true" className="h-4 w-4" />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Next submission"
          disabled={!hasNext}
          onClick={() => {
            onMove(1);
          }}
        >
          <ChevronDown aria-hidden="true" className="h-4 w-4" />
        </Button>
      </div>

      <Dialog
        open={dialog === 'reject'}
        onOpenChange={(next) => {
          if (!next) onDialogChange(null);
        }}
      >
        <DialogContent aria-describedby="reject-description">
          <DialogHeader>
            <DialogTitle>Reject {item.id}?</DialogTitle>
            <DialogDescription id="reject-description">
              The submission stays in the repository with its decision recorded — nothing is
              deleted, so the answer can be looked up later.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                onDialogChange(null);
              }}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => {
                onDialogChange(null);
                onReject();
              }}
            >
              Reject submission
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={dialog === 'snooze'}
        onOpenChange={(next) => {
          if (!next) onDialogChange(null);
        }}
      >
        <DialogContent aria-describedby="snooze-description">
          <DialogHeader>
            <DialogTitle>Snooze {item.id}</DialogTitle>
            <DialogDescription id="snooze-description">
              It leaves the queue until this date and comes back on its own — there is no
              scheduler, the date is simply compared when the queue is read.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-1.5">
            <Label htmlFor="snooze-until">Comes back on</Label>
            <Input
              id="snooze-until"
              type="date"
              value={until}
              onChange={(event) => {
                setUntil(event.target.value);
              }}
            />
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                onDialogChange(null);
              }}
            >
              Cancel
            </Button>
            <Button
              disabled={busy || until === ''}
              onClick={() => {
                onDialogChange(null);
                onSnooze(until);
              }}
            >
              Snooze until this date
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={dialog === 'duplicate'}
        onOpenChange={(next) => {
          if (!next) onDialogChange(null);
        }}
      >
        <DialogContent aria-describedby="duplicate-description">
          <DialogHeader>
            <DialogTitle>Mark {item.id} a duplicate</DialogTitle>
            <DialogDescription id="duplicate-description">
              Point it at the item it repeats. This is also the answer to “it should have been an
              epic”: create the right item, then mark this one a duplicate of it — an item id
              carries its type for life and accepting can never change it.
            </DialogDescription>
          </DialogHeader>
          <ItemPicker
            id="duplicate-of"
            label="Duplicate of"
            value={duplicateOf}
            onChange={setDuplicateOf}
            projectKey={projectKey}
            types={['epic', 'story', 'task']}
          />
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                onDialogChange(null);
              }}
            >
              Cancel
            </Button>
            <Button
              disabled={busy || duplicateOf === null}
              onClick={() => {
                if (duplicateOf === null) return;
                onDialogChange(null);
                onDuplicate(duplicateOf);
              }}
            >
              Mark duplicate
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
