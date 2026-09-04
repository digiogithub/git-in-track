import { render, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleTeam } from '@/api/fake-provider';
import type { BoardCard } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { renderWithRouter } from '@/test/router';

import { BoardCardTile } from './BoardCardTile';
import { BoardCanvas } from './BoardView';
import { snapshotAge, snapshotCaption } from './snapshot-age';

/** The card of the project nobody cloned, as the core renders it. */
const remote: BoardCard = {
  ref: 'WEB/WEB-US-0031',
  project: 'WEB',
  item: 'WEB-US-0031',
  declared: true,
  remote: true,
  source: 'snapshot',
  snapshotAt: '2026-09-03T06:00:00Z',
  stale: false,
  remoteUrl:
    'https://gitlab.com/acme/website/-/blob/main/documentation/.pmngr/stories/WEB-US-0031-rewrite-the-hero-section.md',
  title: 'Rewrite the hero section',
  type: 'story',
  status: 'in_progress',
  priority: 'high',
  assignees: ['marta'],
  labels: ['frontend'],
  estimate: 5,
  reason: 'WEB is not cloned on this machine: this card is read from the index snapshot',
};

/** Renders one tile in isolation, outside a drag context. */
function tile(card: BoardCard) {
  return render(
    <ul>
      <BoardCardTile card={card} project={undefined} show={[]} draggable={false} />
    </ul>,
  );
}

describe('snapshotAge', () => {
  const now = new Date('2026-09-03T12:00:00Z');

  it.each([
    ['2026-09-03T11:59:30Z', 'just now'],
    ['2026-09-03T11:30:00Z', '30 minutes ago'],
    ['2026-09-03T06:00:00Z', '6 hours ago'],
    ['2026-09-01T12:00:00Z', '2 days ago'],
    ['2026-09-02T12:00:00Z', '1 day ago'],
  ])('renders %s as %s', (generated, want) => {
    expect(snapshotAge(generated, now)).toBe(want);
  });

  it('says nothing about a card that is not snapshot-backed', () => {
    expect(snapshotCaption({ source: 'live', snapshotAt: '2026-09-03T06:00:00Z' }, now)).toBe('');
    expect(snapshotAge(undefined, now)).toBe('');
  });

  it('marks a stale snapshot in the caption', () => {
    expect(snapshotCaption({ ...remote, stale: true }, now)).toBe('Stale snapshot, 6 hours ago');
    expect(snapshotCaption(remote, now)).toBe('Snapshot from 6 hours ago');
  });
});

describe('a remote card', () => {
  it('shows the fields the snapshot published', () => {
    tile(remote);

    expect(screen.getByText('Rewrite the hero section')).toBeInTheDocument();
    expect(screen.getByText('marta')).toBeInTheDocument();
    expect(screen.getByText('frontend')).toBeInTheDocument();
    expect(screen.getByText('5 pts')).toBeInTheDocument();
    expect(screen.getByText('remote')).toBeInTheDocument();
  });

  it('links to the item on the git host', () => {
    tile(remote);

    const link = screen.getByRole('link', { name: /Open on the host/ });
    expect(link).toHaveAttribute('href', remote.remoteUrl);
  });

  it('cannot be dragged and says why', () => {
    tile(remote);

    expect(screen.queryByLabelText('Drag WEB-US-0031')).not.toBeInTheDocument();
    expect(screen.getByText(remote.reason!)).toBeInTheDocument();
  });

  it('badges a stale snapshot', () => {
    const { container } = tile({ ...remote, stale: true });
    expect(container.querySelector('[data-stale="true"]')).not.toBeNull();
  });

  it('falls back to the reference alone when no snapshot resolves it', () => {
    const bare: BoardCard = {
      ref: 'WEB/WEB-T-0099',
      project: 'WEB',
      item: 'WEB-T-0099',
      declared: true,
      remote: true,
      reason: 'project WEB is not cloned on this machine and has no index snapshot yet',
    };
    tile(bare);

    expect(screen.getByText(/Title unavailable/)).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /Open on the host/ })).not.toBeInTheDocument();
  });
});

describe('the board refuses to write through a remote card', () => {
  it('offers no move control and says how to make the card editable', async () => {
    renderWithRouter({
      index: () => (
        <ToastProvider>
          <BoardCanvas slug="delivery" />
        </ToastProvider>
      ),
      provider: new FakeProvider({ team: sampleTeam }),
    });

    await screen.findByRole('heading', { name: 'Delivery' });
    const todo = await waitFor(() => {
      const found = document.querySelector<HTMLElement>('[data-column="todo"]');
      if (!found) throw new Error('no todo column');
      return found;
    });

    expect(within(todo).queryByLabelText('Move WEB-US-0031 to')).not.toBeInTheDocument();
    expect(within(todo).queryByLabelText('Drag WEB-US-0031')).not.toBeInTheDocument();
    expect(within(todo).getByText(/is not cloned on this machine/)).toBeInTheDocument();
    // The live card next to it keeps both affordances.
    expect(within(todo).getByLabelText('Move ACME-T-0107 to')).toBeInTheDocument();
  });

  it('refuses the write with repo_not_cloned when a client asks anyway', async () => {
    const provider = new FakeProvider({ team: sampleTeam });
    await expect(
      provider.moveCard({
        board: 'delivery',
        ref: 'WEB/WEB-US-0031',
        toColumn: 'in_progress',
        position: 0,
      }),
    ).rejects.toMatchObject({ code: 'repo_not_cloned' });
  });
});
