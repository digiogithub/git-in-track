/**
 * Background job notifications (story GIT-US-0081, task GIT-T-0169).
 *
 * An import is deliberately not something a person waits for: they start it,
 * close the dialog and go back to work. That is only true if finishing is
 * announced wherever they happen to be, so this component lives in the app
 * shell rather than in any one route.
 *
 * It coalesces. A batch of jobs finishing within a second of each other is one
 * event to a person — "the import is done" — and a stack of eleven toasts is
 * how a notification surface teaches people to ignore it. So terminal frames
 * are collected for a short window and raised as a single summary, with
 * failures counted apart from successes because only failures need doing
 * something about. That window is a *notification* concern and has nothing to
 * do with the engine's own 500 ms progress coalescing: progress frames are not
 * read here at all.
 *
 * A failure toast carries the one useful follow-up, which is the queue itself:
 * the table in Settings holds the job, its attempts and the redacted error.
 */

import { useNavigate } from '@tanstack/react-router';
import { useEffect, useRef } from 'react';

import type { SyncJobEvent } from '@/api/provider';
import { useToast } from '@/components/ui/toast';
import { useSyncJobEvents } from '@/features/sync/queries';

/**
 * How long terminal frames are gathered before one toast is raised. Long
 * enough that a batch finishing together is one notification, short enough
 * that a single job still feels immediate.
 */
export const TOAST_COALESCE_MS = 1_000;

/** Where the failure toast sends the user. */
const QUEUE_ROUTE = '/settings';

export function SyncJobToasts() {
  const { toast } = useToast();
  const navigate = useNavigate();
  const pending = useRef<{ done: number; failed: number; lastError: string }>({
    done: 0,
    failed: 0,
    lastError: '',
  });
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // The flush reads the buffer through a ref, so the window never restarts
  // just because the component re-rendered.
  const flush = useRef(() => undefined as void);
  flush.current = () => {
    const { done, failed, lastError } = pending.current;
    pending.current = { done: 0, failed: 0, lastError: '' };
    timer.current = null;
    if (done === 0 && failed === 0) return;

    if (failed > 0) {
      toast({
        title:
          failed === 1 ? 'A background job failed' : `${String(failed)} background jobs failed`,
        // Third-party text, already redacted by the engine: plain, never markup.
        description:
          lastError === '' ? 'The queue in Settings holds the attempts and the error.' : lastError,
        variant: 'destructive',
        action: {
          label: 'Show the queue',
          onClick: () => {
            void navigate({ to: QUEUE_ROUTE });
          },
        },
      });
    }
    if (done > 0) {
      toast({
        title:
          done === 1 ? 'A background job finished' : `${String(done)} background jobs finished`,
      });
    }
  };

  useSyncJobEvents((event: SyncJobEvent) => {
    // Only the two terminal topics are notifications; `queued`, `started`,
    // `progress` and the synthetic `resync` are the table's business.
    if (event.phase === 'failed') {
      pending.current.failed += 1;
      if (event.error !== '') pending.current.lastError = event.error;
    } else if (event.phase === 'done') {
      // A cancelled job stopped without failing, and the person who cancelled
      // it does not need to be told that it stopped.
      if (event.state === 'cancelled') return;
      pending.current.done += 1;
    } else {
      return;
    }

    timer.current ??= setTimeout(() => {
      flush.current();
    }, TOAST_COALESCE_MS);
  });

  useEffect(
    () => () => {
      if (timer.current !== null) clearTimeout(timer.current);
    },
    [],
  );

  return null;
}
