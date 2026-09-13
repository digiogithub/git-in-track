/**
 * Accepting a submission into the backlog.
 *
 * The two things worth asserting are the two the flow exists for: the status
 * field arrives on a real workflow status instead of the triage one, and the
 * save commits the acceptance itself rather than a plain patch that would leave
 * the item in the queue.
 */

import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { ProviderError } from '@/api/provider';
import { inboxProvider, renderInbox } from '@/features/inbox/test-utils';

const ACCEPT_PATH = '/p/ACME/inbox/ACME-US-0200/accept';

describe('InboxAcceptPage', () => {
  it('opens the edit form with the initial non-triage status preselected', async () => {
    renderInbox({ path: ACCEPT_PATH });

    expect(await screen.findByText('Accept into the backlog')).toBeInTheDocument();
    // The submission is sitting in `triage`; the form offers `backlog`, which is
    // the workflow's declared initial status.
    await waitFor(() => {
      expect(screen.getByLabelText(/status/i)).toHaveValue('backlog');
    });
  });

  it('never offers a type picker, because an id carries its type for life', async () => {
    renderInbox({ path: ACCEPT_PATH });

    await screen.findByText('Accept into the backlog');
    expect(screen.queryByLabelText(/^type$/i)).toBeNull();
  });

  it('commits the acceptance in the same save as the form', async () => {
    const user = userEvent.setup();
    const provider = inboxProvider();
    const triage = vi.spyOn(provider, 'triageInboxItem');
    const { router } = renderInbox({ path: ACCEPT_PATH, provider });

    await screen.findByText('Accept into the backlog');
    await user.click(screen.getByRole('button', { name: 'Accept' }));

    await waitFor(() => {
      expect(triage).toHaveBeenCalledWith(
        expect.objectContaining({
          id: 'ACME-US-0200',
          action: 'accept',
          status: 'backlog',
          rev: 'sha256:0000000000000201',
        }),
      );
    });
    // It returns to the queue, which is where the pass continues.
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/p/ACME/inbox');
    });
  });

  it('sends the parent the person picked along with the acceptance', async () => {
    const user = userEvent.setup();
    const provider = inboxProvider();
    const triage = vi.spyOn(provider, 'triageInboxItem');
    renderInbox({ path: ACCEPT_PATH, provider });

    await screen.findByText('Accept into the backlog');
    await user.type(screen.getByLabelText(/parent/i), 'ACME-EP-0001');
    await user.click(screen.getByRole('button', { name: 'Accept' }));

    await waitFor(() => {
      expect(triage).toHaveBeenCalledWith(
        expect.objectContaining({ action: 'accept', parent: 'ACME-EP-0001' }),
      );
    });
  });

  it('opens the conflict dialog when the submission changed under the form', async () => {
    const user = userEvent.setup();
    const provider = inboxProvider();
    vi.spyOn(provider, 'triageInboxItem').mockRejectedValue(
      new ProviderError('stale_revision', 'ACME-US-0200 changed since it was read'),
    );
    renderInbox({ path: ACCEPT_PATH, provider });

    await screen.findByText('Accept into the backlog');
    await user.click(screen.getByRole('button', { name: 'Accept' }));

    expect(await screen.findByText('ACME-US-0200 changed on disk')).toBeInTheDocument();
  });

  it('reports a refusal that is not a conflict rather than failing silently', async () => {
    const user = userEvent.setup();
    const provider = inboxProvider();
    vi.spyOn(provider, 'triageInboxItem').mockRejectedValue(
      new ProviderError('permission_denied', 'this workspace is read-only'),
    );
    renderInbox({ path: ACCEPT_PATH, provider });

    await screen.findByText('Accept into the backlog');
    await user.click(screen.getByRole('button', { name: 'Accept' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('this workspace is read-only');
  });
});
