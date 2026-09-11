import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import { feedbackKey, resetFeedbackCache } from '@/features/feedback/feedback-store';

import { renderBacklog } from './test-utils';

const KEY = feedbackKey({ kind: 'item', project: 'ACME', ref: 'ACME-US-0042' });

/** Selects the whole text of an element and lets go of the mouse over it. */
function selectText(element: HTMLElement) {
  const range = document.createRange();
  range.selectNodeContents(element);
  const selection = document.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
  fireEvent.mouseUp(element);
}

afterEach(() => {
  localStorage.clear();
  resetFeedbackCache();
  document.getSelection()?.removeAllRanges();
});

describe('ItemDetail feedback', () => {
  it('turns selected description text into one feedback comment', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    const addComment = vi.spyOn(provider, 'addComment');
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    selectText(await screen.findByText('As an employee, I want SSO.'));

    // Outside feedback mode, a selection offers to switch it on.
    const ask = await screen.findByRole('dialog', { name: 'Enable feedback mode' });
    await user.click(within(ask).getByRole('button', { name: /enable feedback mode/i }));

    const overlay = await screen.findByRole('dialog', { name: 'Add a feedback note' });
    expect(within(overlay).getByText('Line 3')).toBeInTheDocument();
    await user.type(within(overlay).getByRole('textbox', { name: 'Feedback note' }), 'Which IdP?');
    await user.click(within(overlay).getByRole('button', { name: 'Add note' }));

    // The note waits in this browser until it is saved.
    expect(localStorage.getItem(KEY)).toContain('Which IdP?');
    expect(screen.getByRole('button', { name: 'Feedback (1)' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );

    const panel = screen.getByRole('region', { name: 'Feedback notes' });
    await user.click(within(panel).getByRole('button', { name: 'Save feedback' }));

    await waitFor(() => expect(addComment).toHaveBeenCalledTimes(1));
    const [id, body, author] = addComment.mock.calls[0] ?? [];
    expect(id).toBe('ACME-US-0042');
    expect(body).toContain('**1.** Description, line 3');
    expect(body).toContain('> As an employee, I want SSO.');
    expect(body).toContain('Which IdP?');
    // No author: the provider attributes it to the repository's git identity.
    expect(author).toBeUndefined();

    await waitFor(() => expect(localStorage.getItem(KEY)).toBeNull());
    expect(screen.queryByRole('region', { name: 'Feedback notes' })).toBeNull();
  });

  it('restores feedback that was never saved', async () => {
    localStorage.setItem(
      KEY,
      JSON.stringify({
        active: true,
        notes: [{ id: 'n1', quote: 'SSO', note: 'Written before a reload', created: '' }],
      }),
    );
    resetFeedbackCache();
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042' });

    const panel = await screen.findByRole('region', { name: 'Feedback notes' });
    expect(within(panel).getByDisplayValue('Written before a reload')).toBeInTheDocument();
  });

  it('keeps the notes when the comment cannot be saved', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider();
    vi.spyOn(provider, 'addComment').mockRejectedValue(new Error('disk full'));
    localStorage.setItem(
      KEY,
      JSON.stringify({
        active: true,
        notes: [{ id: 'n1', quote: 'SSO', note: 'Keep me', created: '' }],
      }),
    );
    resetFeedbackCache();
    renderBacklog({ path: '/p/ACME/items/ACME-US-0042', provider });

    const panel = await screen.findByRole('region', { name: 'Feedback notes' });
    await user.click(within(panel).getByRole('button', { name: 'Save feedback' }));

    expect(await within(panel).findByRole('alert')).toHaveTextContent('disk full');
    expect(localStorage.getItem(KEY)).toContain('Keep me');
  });
});
