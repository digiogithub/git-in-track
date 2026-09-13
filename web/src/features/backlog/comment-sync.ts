/**
 * What happened to a comment on its way to YouTrack (story GIT-US-0076).
 *
 * The state of a comment is never a fact this screen owns. It is derived from
 * two things that outlive the component:
 *
 *  1. the comment's own `external` list — written into the comment file by the
 *     push job once the remote comment exists. That is the durable evidence
 *     that it *arrived*, and it is why a reload shows the truth rather than a
 *     button that has forgotten it was ever clicked;
 *  2. the `sync.job.*` frames for the job that is carrying it. A push answers
 *     with a job id and nothing more: the answer says what was **queued**, not
 *     what landed, so "pending" is the honest word until the job ends.
 *
 * The job frames live in a module-level store rather than in component state,
 * for the same reason the feedback drafts do: two panels looking at the same
 * thread must agree, and a remount must not lose what the stream already said.
 */

import { useCallback, useSyncExternalStore } from 'react';

import type { Comment, External, SyncJobEvent } from '@/api/provider';

/** The job kind that carries a comment upstream (docs/07 §the job engine). */
export const COMMENT_PUSH_JOB_KIND = 'youtrack.comment.push';

/** The system name of a YouTrack `external:` entry. Lower-cased by the core. */
export const YOUTRACK_SYSTEM = 'youtrack';

/** What the store remembers about one comment's in-flight push. */
export type CommentPushRecord = {
  jobId: string;
  /** The last frame seen for that job, absent until the stream says something. */
  frame?: SyncJobEvent;
};

export type CommentSyncState =
  /** No push has been asked for, and none is in flight. */
  | { state: 'unsent' }
  /** Queued or running: the tracker has not confirmed anything yet. */
  | { state: 'pending' }
  /** The remote comment exists; `id` is its id there. */
  | { state: 'sent'; id: string; url?: string }
  /** The job gave up. The comment is untouched locally and can be retried. */
  | { state: 'failed'; error: string };

/** The YouTrack entry of a comment, when a push has already placed it upstream. */
export function youtrackRef(comment: Pick<Comment, 'external'>): External | undefined {
  return (comment.external ?? []).find((entry) => entry.system === YOUTRACK_SYSTEM);
}

/** Whether an item is linked to a YouTrack issue at all — the action's gate. */
export function hasYoutrackRef(external: External[] | undefined): boolean {
  return (external ?? []).some((entry) => entry.system === YOUTRACK_SYSTEM);
}

// --------------------------------------------------------------- the store

/**
 * One record per comment path. An entry is always replaced, never mutated, so
 * `useSyncExternalStore` sees a new reference exactly when something changed
 * and a stable one when nothing did.
 */
const records = new Map<string, CommentPushRecord>();
const listeners = new Set<() => void>();

function announce(): void {
  for (const listener of [...listeners]) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Records that a comment was queued, from the answer the push returned.
 *
 * This is the server's own statement — the same fact `sync.job.queued` carries —
 * and not a guess the button made, which is why it is allowed to drive the
 * pending badge before any frame has arrived.
 */
export function rememberQueuedPush(commentPath: string, jobId: string): void {
  records.set(commentPath, { jobId });
  announce();
}

/**
 * Folds one `sync.job.*` frame in. A frame is matched to a comment by the job
 * id a push handed back, and also by `key`, which the engine coalesces comment
 * pushes on — so a push started in another tab, or by an agent, lands here too.
 */
export function applyCommentJobFrame(frame: SyncJobEvent): void {
  if (frame.kind !== COMMENT_PUSH_JOB_KIND) return;
  let touched = false;
  for (const [path, record] of records) {
    if (record.jobId !== frame.id) continue;
    records.set(path, { ...record, frame });
    touched = true;
  }
  if (!touched && frame.key !== '') {
    // The coalescing key of this job kind is the comment path, so a frame for a
    // push this tab never started still names the comment it is carrying.
    records.set(frame.key, { jobId: frame.id, frame });
    touched = true;
  }
  if (touched) announce();
}

/**
 * What the store holds for one comment. It is the hook's own read, exposed so
 * that the derivation can be exercised without a render.
 */
export function commentPushRecord(commentPath: string): CommentPushRecord | undefined {
  return records.get(commentPath);
}

/** Test seam, and the reset a signed-out or switched workspace deserves. */
export function resetCommentPushes(): void {
  records.clear();
  announce();
}

// ---------------------------------------------------------- the derivation

/**
 * The state of one comment, given what the stream has said about it.
 *
 * `external` wins over every frame. Once the remote comment exists the job is
 * only bookkeeping, and a late `failed` frame from a retried delivery must not
 * un-send a comment that is sitting on the issue for everyone to read.
 */
export function commentSyncState(
  comment: Pick<Comment, 'path' | 'external'>,
  record: CommentPushRecord | undefined,
): CommentSyncState {
  const external = youtrackRef(comment);
  if (external) {
    return {
      state: 'sent',
      id: external.id,
      ...(external.url === undefined ? {} : { url: external.url }),
    };
  }
  if (!record) return { state: 'unsent' };
  const frame = record.frame;
  if (!frame) return { state: 'pending' };
  if (frame.phase === 'failed' || frame.state === 'failed') {
    return { state: 'failed', error: frame.error === '' ? 'The push failed.' : frame.error };
  }
  if (frame.phase === 'done') {
    // The job finished but the comment does not carry a reference yet: the
    // file is being re-read. Pending is the honest answer for that instant.
    return { state: 'pending' };
  }
  return { state: 'pending' };
}

/**
 * The live state of one comment.
 *
 * It reads the comment that came from the query cache and the job frames from
 * the store — never anything this component put there — so unmounting the panel,
 * switching items and reloading the tab all show the same thing.
 */
export function useCommentSyncState(comment: Pick<Comment, 'path' | 'external'>): CommentSyncState {
  const getRecord = useCallback(() => records.get(comment.path), [comment.path]);
  const record = useSyncExternalStore(subscribe, getRecord, () => undefined);
  return commentSyncState(comment, record);
}
