import { useEffect, useRef } from 'react';

import { Button } from '@/components/ui/button';

export type ConflictDialogProps = {
  itemId: string;
  busy?: boolean;
  onReload: () => void;
  onOverwrite: () => void;
  onCancel: () => void;
};

/**
 * Shown when the provider rejects a write with `stale_revision`: the file
 * changed on disk since the buffer was opened (docs/06-git-sync.md §3,
 * docs/05-web-app.md §8). Nothing is overwritten without an explicit choice.
 */
export function ConflictDialog({
  itemId,
  busy = false,
  onReload,
  onOverwrite,
  onCancel,
}: ConflictDialogProps) {
  const cancelRef = useRef<HTMLButtonElement | null>(null);

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

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="conflict-title"
        aria-describedby="conflict-description"
        className="w-full max-w-md space-y-4 rounded-lg border border-border bg-card p-5 text-card-foreground shadow-lg"
      >
        <h2 id="conflict-title" className="text-base font-semibold">
          {itemId} changed on disk
        </h2>
        <p id="conflict-description" className="text-sm text-muted-foreground">
          Someone (or something) wrote this file after you opened it, so the revision check
          failed. Nothing has been saved yet.
        </p>
        <div className="flex flex-wrap justify-end gap-2">
          <Button ref={cancelRef} variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="outline" disabled={busy} onClick={onReload}>
            Reload theirs
          </Button>
          <Button variant="destructive" disabled={busy} onClick={onOverwrite}>
            Overwrite with mine
          </Button>
        </div>
      </div>
    </div>
  );
}
