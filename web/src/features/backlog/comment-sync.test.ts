/**
 * The derivation behind a comment's YouTrack badge (task GIT-T-0179).
 *
 * These are the rules the component must not be allowed to re-invent: what
 * counts as sent, what a frame may and may not override, and the fact that a
 * comment file — not a click — is the evidence.
 */

import { beforeEach, describe, expect, it } from 'vitest';

import type { Comment, SyncJobEvent } from '@/api/provider';

import {
  applyCommentJobFrame,
  commentPushRecord,
  commentSyncState,
  COMMENT_PUSH_JOB_KIND,
  hasYoutrackRef,
  rememberQueuedPush,
  resetCommentPushes,
  youtrackRef,
} from './comment-sync';

const PATH = 'docs/.pmngr/comments/ACME-US-0042/20260825T120000Z-jose.md';

function comment(external?: Comment['external']): Pick<Comment, 'path' | 'external'> {
  return { path: PATH, ...(external === undefined ? {} : { external }) };
}

function frame(overrides: Partial<SyncJobEvent> = {}): SyncJobEvent {
  return {
    phase: 'queued',
    id: 'job_1',
    kind: COMMENT_PUSH_JOB_KIND,
    key: PATH,
    state: 'queued',
    attempt: 0,
    processed: 0,
    total: 1,
    error: '',
    errorClass: '',
    ...overrides,
  };
}

beforeEach(() => {
  resetCommentPushes();
});

describe('commentSyncState', () => {
  it('is unsent when nothing has been asked for', () => {
    expect(commentSyncState(comment(), undefined)).toEqual({ state: 'unsent' });
  });

  it('is pending as soon as a push has been queued, before any frame arrives', () => {
    expect(commentSyncState(comment(), { jobId: 'job_1' })).toEqual({ state: 'pending' });
  });

  it('is pending while the job runs', () => {
    const record = { jobId: 'job_1', frame: frame({ phase: 'progress', state: 'running' }) };

    expect(commentSyncState(comment(), record)).toEqual({ state: 'pending' });
  });

  it('is sent with the remote id and link once the comment carries the reference', () => {
    const external = [
      { system: 'youtrack', id: '4-19', url: 'https://yt.example.com/issue/ACME-42#comment=4-19' },
    ];

    expect(commentSyncState(comment(external), { jobId: 'job_1' })).toEqual({
      state: 'sent',
      id: '4-19',
      url: 'https://yt.example.com/issue/ACME-42#comment=4-19',
    });
  });

  it('is failed with the error the job reported', () => {
    const record = {
      jobId: 'job_1',
      frame: frame({ phase: 'failed', state: 'failed', error: '403 Forbidden' }),
    };

    expect(commentSyncState(comment(), record)).toEqual({
      state: 'failed',
      error: '403 Forbidden',
    });
  });

  it('lets the reference win over a late failure, so a sent comment never un-sends', () => {
    // The engine re-delivers a job after a retryable error; the comment is
    // already on the issue for everyone to read.
    const record = {
      jobId: 'job_1',
      frame: frame({ phase: 'failed', state: 'failed', error: '500' }),
    };
    const external = [{ system: 'youtrack', id: '4-19' }];

    expect(commentSyncState(comment(external), record)).toMatchObject({ state: 'sent' });
  });

  it('stays pending in the instant between the job finishing and the file being re-read', () => {
    const record = { jobId: 'job_1', frame: frame({ phase: 'done', state: 'done' }) };

    expect(commentSyncState(comment(), record)).toEqual({ state: 'pending' });
  });
});

describe('the job-frame store', () => {
  it('matches a frame to the comment whose push handed back that job id', () => {
    rememberQueuedPush(PATH, 'job_7');
    applyCommentJobFrame(frame({ id: 'job_7', key: '', phase: 'failed', error: 'boom' }));

    expect(commentSyncState(comment(), commentPushRecord(PATH))).toEqual({
      state: 'failed',
      error: 'boom',
    });
  });

  it('picks up a push this tab never started, by the coalescing key', () => {
    applyCommentJobFrame(frame({ id: 'job_9', key: PATH, phase: 'progress', state: 'running' }));

    expect(commentSyncState(comment(), commentPushRecord(PATH))).toEqual({ state: 'pending' });
  });

  it('ignores frames of every other job kind', () => {
    applyCommentJobFrame(frame({ kind: 'youtrack.import', key: PATH }));

    expect(commentSyncState(comment(), commentPushRecord(PATH))).toEqual({ state: 'unsent' });
  });
});

describe('hasYoutrackRef / youtrackRef', () => {
  it('finds the YouTrack entry and ignores the other systems', () => {
    const external = [
      { system: 'jira', id: 'X-1' },
      { system: 'youtrack', id: 'ACME-42' },
    ];

    expect(hasYoutrackRef(external)).toBe(true);
    expect(youtrackRef({ external })?.id).toBe('ACME-42');
    expect(hasYoutrackRef(undefined)).toBe(false);
    expect(hasYoutrackRef([{ system: 'jira', id: 'X-1' }])).toBe(false);
  });
});
