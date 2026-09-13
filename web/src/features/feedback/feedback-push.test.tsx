/**
 * "Send to YouTrack after saving" on a feedback note (task GIT-T-0187).
 *
 * A feedback note is already an ordinary comment (ADR-030), so sending it on is
 * the same push as any other comment's. What is worth pinning down here is
 * where the checkbox is allowed to appear — only for the item destination, and
 * only where a comment could travel at all — that the answer is remembered for
 * the project rather than asked again for every note, and that ticking it makes
 * the saved comment actually get queued.
 */

import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider, sampleItems, type FakeYouTrack } from '@/api/fake-provider';
import type { Item } from '@/api/provider';
import { resetCommentPushes } from '@/features/backlog/comment-sync';
import { renderBacklog } from '@/features/backlog/test-utils';
import {
  feedbackKey,
  feedbackPushKey,
  resetFeedbackCache,
  useFeedbackDraft,
  useFeedbackPushPreference,
} from '@/features/feedback/feedback-store';
import { FeedbackPanel } from '@/features/feedback/FeedbackPanel';

const connected: FakeYouTrack = {
  settings: {
    configured: true,
    url: 'https://yt.example.com/youtrack',
    project: 'ACME',
    hasToken: true,
    tokenSource: 'file',
    pushComments: 'manual',
  },
};

/** The story, mirroring a YouTrack issue: the third half of the gate. */
function linkedItems(): Item[] {
  return sampleItems.map((item) =>
    item.id === 'ACME-US-0042'
      ? { ...item, external: [{ system: 'youtrack', id: 'ACME-42' }] }
      : item,
  );
}

/** One finished note, seeded the way a selection session would have left it. */
function seedDraft(): void {
  localStorage.setItem(
    feedbackKey({ kind: 'item', project: 'ACME', ref: 'ACME-US-0042' }),
    JSON.stringify({
      active: true,
      notes: [
        {
          id: 'n1',
          quote: 'Let tenants sign in',
          startLine: 3,
          endLine: 3,
          note: 'Which providers, exactly?',
          created: '2026-09-02T09:00:00Z',
        },
      ],
    }),
  );
}

beforeEach(() => {
  localStorage.clear();
  resetFeedbackCache();
  resetCommentPushes();
});

// ------------------------------------------------ the panel's own rule

/** Drives `FeedbackPanel` directly, which is where the destination rule lives. */
function Harness({ destination }: { destination: 'comment' | 'page' }) {
  const feedback = useFeedbackDraft({ kind: 'item', project: 'ACME', ref: 'ACME-US-0042' });
  const push = useFeedbackPushPreference('ACME');
  return (
    <FeedbackPanel
      feedback={feedback}
      destination={destination}
      canWrite
      saving={false}
      onSave={() => undefined}
      push={{ enabled: push.enabled, onChange: push.setEnabled }}
    />
  );
}

const checkbox = () => screen.getByLabelText('Send to YouTrack after saving');

describe('the checkbox', () => {
  it('is offered for the item destination', () => {
    render(<Harness destination="comment" />);

    expect(checkbox()).toBeInTheDocument();
  });

  it('is never offered for a knowledge-base page, which has no issue to reach', () => {
    render(<Harness destination="page" />);

    expect(screen.queryByLabelText('Send to YouTrack after saving')).toBeNull();
    // The page destination still saves; only the push option is absent.
    expect(screen.getByRole('button', { name: 'Save feedback' })).toBeInTheDocument();
  });

  it('remembers the answer for the project, not for the note', async () => {
    const user = userEvent.setup();
    const first = render(<Harness destination="comment" />);

    await user.click(checkbox());
    expect(localStorage.getItem(feedbackPushKey('ACME'))).toBe('true');

    // A fresh mount of the same project starts from the remembered answer.
    first.unmount();
    resetFeedbackCache();
    render(<Harness destination="comment" />);
    expect(checkbox()).toBeChecked();
  });

  it('starts unchecked for a project that has never answered', () => {
    localStorage.setItem(feedbackPushKey('OTHER'), 'true');
    render(<Harness destination="comment" />);

    expect(checkbox()).not.toBeChecked();
  });
});

// --------------------------------------------- the gate on the item screen

describe('the gate', () => {
  it('offers no checkbox where a comment could not travel anyway', async () => {
    seedDraft();
    // A connected project, but this item mirrors no issue.
    const provider = new FakeProvider({ items: sampleItems, youtrack: connected });
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    expect(await screen.findByRole('button', { name: 'Save feedback' })).toBeInTheDocument();
    expect(screen.queryByLabelText('Send to YouTrack after saving')).toBeNull();
  });

  it('offers the checkbox once the runtime, the project and the item all qualify', async () => {
    seedDraft();
    const provider = new FakeProvider({ items: linkedItems(), youtrack: connected });
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    expect(await screen.findByLabelText('Send to YouTrack after saving')).toBeInTheDocument();
  });
});

// ------------------------------------------------------- saving the note

describe('saving a note', () => {
  it('queues the saved comment for YouTrack when the box is ticked', async () => {
    seedDraft();
    localStorage.setItem(feedbackPushKey('ACME'), 'true');
    const provider = new FakeProvider({ items: linkedItems(), youtrack: connected });
    const push = vi.spyOn(provider, 'pushCommentToYoutrack');
    const user = userEvent.setup();

    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });
    expect(await screen.findByLabelText('Send to YouTrack after saving')).toBeChecked();
    await user.click(screen.getByRole('button', { name: 'Save feedback' }));

    // The note is saved as a comment first, then that comment is queued.
    await vi.waitFor(() => {
      expect(push).toHaveBeenCalledTimes(1);
    });
    expect(push.mock.calls[0]?.[0]).toMatchObject({ itemId: 'ACME-US-0042' });
    expect(push.mock.calls[0]?.[0].commentPath).toMatch(/ACME-US-0042/);
  });

  it('saves without queuing anything when the box is left unticked', async () => {
    seedDraft();
    const provider = new FakeProvider({ items: linkedItems(), youtrack: connected });
    const push = vi.spyOn(provider, 'pushCommentToYoutrack');
    const user = userEvent.setup();

    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });
    expect(await screen.findByLabelText('Send to YouTrack after saving')).not.toBeChecked();
    await user.click(screen.getByRole('button', { name: 'Save feedback' }));

    expect(await screen.findByText('Feedback saved as a comment')).toBeInTheDocument();
    expect(push).not.toHaveBeenCalled();
  });
});
