/**
 * The triage screen as a person meets it: the queue, the filters, the four
 * decisions, and the three states a screen is usually wrong about — empty, in
 * error, and gated away entirely.
 */

import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { FakeProvider, sampleItems, sampleProject } from '@/api/fake-provider';
import type { Item } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { inboxProvider, renderInbox } from '@/features/inbox/test-utils';

/** The rows of the queue, in the order they are listed. */
function rowIds(): string[] {
  return screen
    .queryAllByRole('button')
    .map((node) => node.getAttribute('data-inbox-row'))
    .filter((id): id is string => id !== null);
}

/** Waits for the queue to have loaded; the title appears twice, once per pane. */
async function waitForQueue(): Promise<void> {
  await waitFor(() => {
    expect(rowIds().length).toBeGreaterThan(0);
  });
}

/** A queue longer than one page, so "Load more" has something to load. */
function longQueue(count: number): Item[] {
  return Array.from({ length: count }, (_, index) => ({
    id: `ACME-US-${String(300 + index).padStart(4, '0')}`,
    type: 'story' as const,
    title: `Submission ${index}`,
    status: 'triage',
    created: `2026-09-01T${String(index % 24).padStart(2, '0')}:00:00Z`,
    inbox: { status: 'pending' as const, source: 'web' },
    body: '',
    path: `docs/.pmngr/stories/ACME-US-${String(300 + index).padStart(4, '0')}.md`,
    rev: `sha256:${String(index).padStart(16, '0')}`,
  }));
}

/** What the detail pane is showing. */
function detailTitle(): string {
  // Queried by its own hook rather than by role: a submission's body is
  // Markdown and routinely contains headings of its own.
  return document.querySelector('[data-inbox-title]')?.textContent ?? '';
}

describe('InboxPage — the queue', () => {
  it('lists what is waiting, with an expired snooze back among it', async () => {
    renderInbox();

    // ACME-US-0201 was snoozed until a date that has already passed against the
    // provider's clock, so the host lists and counts it as pending again.
    await waitForQueue();
    expect(rowIds()).toEqual(['ACME-US-0200', 'ACME-US-0201']);
    expect(screen.getByText(/2 waiting/)).toBeInTheDocument();
  });

  it('keeps the filter in the URL and lists the slice it names', async () => {
    renderInbox({ path: '/p/ACME/inbox?filter=snoozed' });

    await waitForQueue();
    expect(rowIds()).toEqual(['ACME-T-0202']);
    expect(screen.getByRole('button', { name: 'Snoozed' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('writes the chosen slice into the URL so the view can be shared', async () => {
    const user = userEvent.setup();
    const { router } = renderInbox();

    await waitForQueue();
    await user.click(screen.getByRole('button', { name: 'All' }));

    await waitFor(() => {
      expect(router.state.location.search).toMatchObject({ filter: 'all' });
    });
    await waitFor(() => {
      expect(rowIds()).toHaveLength(4);
    });
  });

  it('opens the row named by the URL rather than the first one', async () => {
    renderInbox({ path: '/p/ACME/inbox?selected=ACME-US-0201' });

    await waitForQueue();
    await waitFor(() => {
      expect(detailTitle()).toBe('Bulk import of members');
    });
  });

  it('pages the queue incrementally behind a "Load more"', async () => {
    const user = userEvent.setup();
    renderInbox({ provider: inboxProvider({ items: longQueue(30) }) });

    // A page is 25 rows, so a queue of 30 arrives in two and the second one is
    // asked for rather than scrolled into.
    await screen.findByRole('button', { name: 'Load more' });
    expect(rowIds()).toHaveLength(25);

    await user.click(screen.getByRole('button', { name: 'Load more' }));
    await waitFor(() => {
      expect(rowIds()).toHaveLength(30);
    });
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('says the queue is clear rather than showing an empty list', async () => {
    renderInbox({ provider: inboxProvider({ items: sampleItems }) });

    expect(await screen.findByText(/Nothing is waiting/)).toBeInTheDocument();
  });

  it('reports a failing read instead of pretending the queue is empty', async () => {
    const provider = inboxProvider();
    vi.spyOn(provider, 'listInbox').mockRejectedValue(
      new ProviderError('internal', 'the index is rebuilding'),
    );

    renderInbox({ provider });

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('the index is rebuilding');
  });

  it('disappears for a project that declares no triage status', async () => {
    renderInbox({
      provider: new FakeProvider({ projects: [sampleProject], items: sampleItems }),
    });

    expect(await screen.findByText(/declares no status in the/)).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Triage queue' })).toBeNull();
  });
});

describe('InboxPage — deciding', () => {
  it('rejects a submission and advances to the next row', async () => {
    const user = userEvent.setup();
    renderInbox();

    await waitForQueue();
    await user.click(screen.getByRole('button', { name: 'Reject' }));
    await user.click(await screen.findByRole('button', { name: 'Reject submission' }));

    // The decided row leaves the pending slice and the pane lands on the next
    // one, rather than on nothing.
    await waitFor(() => {
      expect(rowIds()).toEqual(['ACME-US-0201']);
    });
    expect(detailTitle()).toBe('Bulk import of members');
    expect(await screen.findByText('ACME-US-0200 rejected')).toBeInTheDocument();
  });

  it('snoozes until the date the dialog asks for', async () => {
    const user = userEvent.setup();
    const { provider } = renderInbox();
    const triage = vi.spyOn(provider, 'triageInboxItem');

    await waitForQueue();
    await user.click(screen.getByRole('button', { name: 'Snooze' }));
    const date = await screen.findByLabelText('Comes back on');
    await user.clear(date);
    await user.type(date, '2026-12-24');
    await user.click(screen.getByRole('button', { name: 'Snooze until this date' }));

    await waitFor(() => {
      expect(triage).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 'ACME-US-0200',
          action: 'snooze',
          snoozedUntil: '2026-12-24',
        }),
      );
    });
  });

  it('marks a duplicate through the item picker, never through a type picker', async () => {
    const user = userEvent.setup();
    const { provider } = renderInbox();
    const triage = vi.spyOn(provider, 'triageInboxItem');

    await waitForQueue();
    await user.click(screen.getByRole('button', { name: 'Duplicate' }));

    const picker = await screen.findByLabelText('Duplicate of');
    await user.type(picker, 'ACME-US-0042');
    await user.click(screen.getByRole('button', { name: 'Mark duplicate' }));

    await waitFor(() => {
      expect(triage).toHaveBeenCalledWith(
        expect.objectContaining({ action: 'duplicate', duplicateOf: 'ACME-US-0042' }),
      );
    });
    // "It should have been an epic" is answered by the duplicate flow, so no
    // type control is offered anywhere on this screen.
    expect(screen.queryByLabelText(/type/i)).toBeNull();
  });

  it('puts the row back and says so when the write is refused', async () => {
    const user = userEvent.setup();
    const provider = inboxProvider();
    vi.spyOn(provider, 'triageInboxItem').mockRejectedValue(
      new ProviderError('permission_denied', 'the repository is read-only right now'),
    );

    renderInbox({ provider });

    await waitForQueue();
    await user.click(screen.getByRole('button', { name: 'Reject' }));
    await user.click(await screen.findByRole('button', { name: 'Reject submission' }));

    // The optimistic removal is rolled back: the row is listed again and the
    // failure is announced rather than swallowed.
    expect(
      await screen.findByText('the repository is read-only right now'),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(rowIds()).toEqual(['ACME-US-0200', 'ACME-US-0201']);
    });
  });

  it('opens the conflict dialog when the revision has moved on', async () => {
    const user = userEvent.setup();
    const provider = inboxProvider();
    vi.spyOn(provider, 'triageInboxItem').mockRejectedValue(
      new ProviderError('stale_revision', 'ACME-US-0200 changed since it was read'),
    );

    renderInbox({ provider });

    await waitForQueue();
    await user.click(screen.getByRole('button', { name: 'Reject' }));
    await user.click(await screen.findByRole('button', { name: 'Reject submission' }));

    expect(await screen.findByText('ACME-US-0200 changed on disk')).toBeInTheDocument();
  });
});

describe('InboxPage — keyboard', () => {
  it('walks the queue with j and k', async () => {
    const user = userEvent.setup();
    renderInbox();

    await waitForQueue();
    await user.keyboard('j');

    await waitFor(() => {
      expect(detailTitle()).toBe('Bulk import of members');
    });

    await user.keyboard('k');
    await waitFor(() => {
      expect(detailTitle()).toBe('Reset password by email');
    });
  });

  it('decides with r and opens the snooze dialog with s', async () => {
    const user = userEvent.setup();
    const { provider } = renderInbox();
    const triage = vi.spyOn(provider, 'triageInboxItem');

    await waitForQueue();
    await user.keyboard('s');
    expect(await screen.findByLabelText('Comes back on')).toBeInTheDocument();
    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(screen.queryByLabelText('Comes back on')).toBeNull();
    });
    await user.keyboard('r');
    await user.click(await screen.findByRole('button', { name: 'Reject submission' }));

    await waitFor(() => {
      expect(triage).toHaveBeenCalledWith(
        expect.objectContaining({ id: 'ACME-US-0200', action: 'reject' }),
      );
    });
  });

  it('opens the accept form with a, rather than accepting in one keystroke', async () => {
    const user = userEvent.setup();
    const { router } = renderInbox();

    await waitForQueue();
    await user.keyboard('a');

    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/p/ACME/inbox/ACME-US-0200/accept');
    });
  });
});
