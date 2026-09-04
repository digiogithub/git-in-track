import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleBoard, sampleTeam } from '@/api/fake-provider';
import { ToastProvider } from '@/components/ui/toast';
import { renderWithRouter } from '@/test/router';

import { BoardCanvas } from './BoardView';

/** A workspace with the sample team and its board, writable unless said so. */
function boardProvider(opts: { readOnly?: boolean } = {}) {
  return new FakeProvider({ team: sampleTeam }, opts);
}

function renderBoard(provider = boardProvider()) {
  return renderWithRouter({
    index: () => (
      <ToastProvider>
        <BoardCanvas slug="delivery" />
      </ToastProvider>
    ),
    provider,
  });
}

/** The rendered column with that id. */
async function column(id: string): Promise<HTMLElement> {
  return await waitFor(() => {
    const found = document.querySelector<HTMLElement>(`[data-column="${id}"]`);
    if (!found) throw new Error(`no column ${id}`);
    return found;
  });
}

/** The card refs a column currently shows, top to bottom. */
function refsIn(element: HTMLElement): string[] {
  return [...element.querySelectorAll('[data-ref]')].map((node) => node.getAttribute('data-ref')!);
}

describe('BoardView', () => {
  it('renders columns with cards from every referenced project', async () => {
    renderBoard();

    expect(await screen.findByRole('heading', { name: 'Delivery' })).toBeInTheDocument();
    const todo = await column('todo');
    // Listed refs first, then whatever the board shows but does not order.
    expect(refsIn(todo)).toEqual([
      'ACME/ACME-T-0107',
      'WEB/WEB-US-0031',
      'ACME/ACME-US-0043',
    ]);

    const doing = await column('in_progress');
    expect(refsIn(doing)).toEqual(['ACME/ACME-US-0042']);
    expect(within(doing).getByText('Login with SSO')).toBeInTheDocument();
  });

  it('shows id, title, assignees, labels, priority and estimate on a card', async () => {
    renderBoard();

    const doing = await column('in_progress');
    expect(within(doing).getByRole('link', { name: 'ACME-US-0042' })).toBeInTheDocument();
    expect(within(doing).getByText('Login with SSO')).toBeInTheDocument();
    expect(within(doing).getByText('marta')).toBeInTheDocument();
    expect(within(doing).getByText('frontend')).toBeInTheDocument();
    expect(within(doing).getByText('high')).toBeInTheDocument();
    expect(within(doing).getByText('8 pts')).toBeInTheDocument();
  });

  it('marks a card whose project nobody cloned and refuses to drag it', async () => {
    renderBoard();

    const todo = await column('todo');
    const remote = todo.querySelector('[data-ref="WEB/WEB-US-0031"]');
    expect(remote).not.toBeNull();
    expect(remote).toHaveAttribute('data-remote', 'true');
    expect(within(remote as HTMLElement).getByText(/not cloned on this machine/)).toBeInTheDocument();
    // No drag handle and no keyboard move for a card we cannot write.
    expect(within(remote as HTMLElement).queryByLabelText(/^Drag /)).not.toBeInTheDocument();
    expect(within(remote as HTMLElement).queryByLabelText(/^Move .* to$/)).not.toBeInTheDocument();
  });

  it('moves a card with the keyboard alternative and announces it', async () => {
    const user = userEvent.setup();
    renderBoard();

    const todo = await column('todo');
    const select = within(todo).getByLabelText('Move ACME-T-0107 to');
    await user.selectOptions(select, 'in_review');

    await waitFor(async () => {
      expect(refsIn(await column('in_review'))).toEqual(['ACME/ACME-T-0107']);
    });
    expect(refsIn(await column('todo'))).toEqual(['WEB/WEB-US-0031', 'ACME/ACME-US-0043']);
    expect(await screen.findByRole('status', { name: 'Board updates' })).toHaveTextContent(
      /Moved ACME-T-0107 to In Review, position 1 of 1\./,
    );
  });

  it('blocks a move that would exceed a WIP limit and explains why', async () => {
    const user = userEvent.setup();
    renderBoard();

    // "In Progress" has a WIP limit of 1 and already holds ACME-US-0042.
    const todo = await column('todo');
    await user.selectOptions(within(todo).getByLabelText('Move ACME-T-0107 to'), 'in_progress');

    expect(await screen.findByRole('dialog')).toHaveTextContent(/WIP limit reached/);
    expect(screen.getByRole('dialog')).toHaveTextContent(/In Progress is at its WIP limit of 1/);
    // Nothing moved while the question is open.
    expect(refsIn(await column('in_progress'))).toEqual(['ACME/ACME-US-0042']);

    await user.click(screen.getByRole('button', { name: 'Move anyway' }));

    await waitFor(async () => {
      expect(refsIn(await column('in_progress'))).toEqual([
        'ACME/ACME-T-0107',
        'ACME/ACME-US-0042',
      ]);
    });
    const doing = await column('in_progress');
    expect(doing).toHaveAttribute('data-exceeded', 'true');
    expect(within(doing).getByRole('status')).toHaveTextContent(/over its WIP limit of 1/);
  });

  it('cancelling the WIP question leaves the board untouched', async () => {
    const user = userEvent.setup();
    renderBoard();

    const todo = await column('todo');
    await user.selectOptions(within(todo).getByLabelText('Move ACME-T-0107 to'), 'in_progress');
    await user.click(await screen.findByRole('button', { name: 'Cancel' }));

    await waitFor(async () => {
      expect(refsIn(await column('todo'))).toEqual([
        'ACME/ACME-T-0107',
        'WEB/WEB-US-0031',
        'ACME/ACME-US-0043',
      ]);
    });
    expect(refsIn(await column('in_progress'))).toEqual(['ACME/ACME-US-0042']);
  });

  it('filters the board by project, label and assignee', async () => {
    const user = userEvent.setup();
    renderBoard();

    await screen.findByRole('heading', { name: 'Delivery' });
    await user.selectOptions(screen.getByLabelText('Filter by project'), 'WEB');
    await waitFor(async () => {
      expect(refsIn(await column('todo'))).toEqual(['WEB/WEB-US-0031']);
    });

    await user.click(screen.getByRole('button', { name: 'Clear' }));
    await user.selectOptions(screen.getByLabelText('Filter by assignee'), 'marta');
    await waitFor(async () => {
      // The remote card carries the assignees its snapshot published, so it is
      // filtered like any other card (GIT-US-0019).
      expect(refsIn(await column('todo'))).toEqual(['WEB/WEB-US-0031']);
    });
    expect(refsIn(await column('in_progress'))).toEqual(['ACME/ACME-US-0042']);
  });

  it('surfaces an item whose status maps to no column instead of hiding it', async () => {
    const provider = new FakeProvider({
      team: sampleTeam,
      boards: [
        {
          ...sampleBoard,
          columns: sampleBoard.columns.filter((c) => c.id !== 'todo'),
          order: { in_progress: ['ACME/ACME-US-0042'], in_review: [], done: [] },
        },
      ],
    });
    renderBoard(provider);

    expect(await screen.findByText(/items hidden \(unmapped status\)/)).toBeInTheDocument();
    expect(screen.getByText('Logout everywhere')).toBeInTheDocument();
    expect(screen.getAllByText(/maps to no column of this board/).length).toBeGreaterThan(0);
  });

  it('offers no move controls in a read-only workspace', async () => {
    renderBoard(boardProvider({ readOnly: true }));

    await screen.findByRole('heading', { name: 'Delivery' });
    expect(screen.getByText(/read-only, so cards cannot be moved/)).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Move .* to$/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/^Drag /)).not.toBeInTheDocument();
  });
});
