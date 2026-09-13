import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import {
  FakeProvider,
  sampleBoard,
  sampleScrumBoard,
  sampleTeam,
  type FakeSprint,
} from '@/api/fake-provider';
import { ToastProvider } from '@/components/ui/toast';
import { renderWithRouter } from '@/test/router';

import { CloseSprintDialog } from './CloseSprintDialog';
import { useSprint } from './sprint-queries';

/**
 * The close dialog (story GIT-US-0089, task GIT-T-0165).
 *
 * Everything here is about the promise the dialog makes: what it shows is a dry
 * run that has written nothing, the destination picker only offers sprints that
 * can legally take work, and a refusal is on screen before the confirm button
 * can be used.
 */

const TODAY = '2026-09-02';

const current: FakeSprint = {
  id: 'ACME-TEAM-S-0007',
  title: 'Sprint 7',
  board: 'acme-scrum',
  state: 'active',
  start: '2026-08-24',
  end: '2026-09-06',
  // ACME-US-0042 is in the clone; WEB/WEB-US-0031 is a project nobody cloned.
  items: ['ACME/ACME-US-0042', 'WEB/WEB-US-0031'],
  committed: ['ACME/ACME-US-0042', 'WEB/WEB-US-0031'],
  rev: 'sha256:00000000000000c1',
};

const upcoming: FakeSprint = {
  id: 'ACME-TEAM-S-0008',
  title: 'Sprint 8',
  board: 'acme-scrum',
  state: 'planned',
  start: '2026-09-07',
  end: '2026-09-20',
  items: [],
  rev: 'sha256:00000000000000c2',
};

const over: FakeSprint = {
  id: 'ACME-TEAM-S-0006',
  title: 'Sprint 6',
  board: 'acme-scrum',
  state: 'closed',
  start: '2026-08-10',
  end: '2026-08-23',
  items: [],
  rev: 'sha256:00000000000000c0',
};

/** A sprint of a different board, which must never be offered as a target. */
const elsewhere: FakeSprint = {
  id: 'ACME-TEAM-S-0100',
  title: 'Website refresh',
  board: 'delivery',
  state: 'planned',
  start: '2026-09-07',
  end: '2026-09-20',
  items: [],
  rev: 'sha256:00000000000000d0',
};

function Harness({ store }: { store: FakeProvider }) {
  const sprint = useSprint('ACME-TEAM-S-0007');
  if (!sprint.data) return <p>Loading…</p>;
  return (
    <CloseSprintDialog
      sprint={sprint.data.sprint}
      open
      onOpenChange={() => {
        void store;
      }}
    />
  );
}

function render(sprints: FakeSprint[] = [over, current, upcoming, elsewhere]) {
  const store = new FakeProvider({
    team: sampleTeam,
    boards: [sampleBoard, sampleScrumBoard],
    sprints,
    today: TODAY,
  });
  renderWithRouter({
    index: () => (
      <ToastProvider>
        <Harness store={store} />
      </ToastProvider>
    ),
    provider: store,
  });
  return store;
}

describe('CloseSprintDialog', () => {
  it('renders the dry-run counts without writing anything', async () => {
    const store = render();

    const counts = await screen.findByRole('group', { name: 'What this close would grade' });
    expect(within(counts).getByText('Finished').nextSibling).toHaveTextContent('0');
    expect(within(counts).getByText('Unfinished').nextSibling).toHaveTextContent('2');
    expect(within(counts).getByText('Unresolved').nextSibling).toHaveTextContent('0');

    // The preview is a preview: the sprint is untouched on the way in.
    const sprint = await store.getSprint('ACME-TEAM-S-0007');
    expect(sprint.sprint.state).toBe('active');
  });

  it('offers only sprints of this board that are not over', async () => {
    const user = userEvent.setup();
    render();

    await screen.findByRole('group', { name: 'What this close would grade' });
    await user.click(screen.getByRole('radio', { name: /Carry it into another sprint/ }));
    await user.click(await screen.findByRole('combobox', { name: 'Target sprint' }));

    const options = await screen.findAllByRole('option');
    const titles = options.map((option) => option.textContent);
    expect(titles).toEqual(['Sprint 8upcoming']);
    // Sprint 6 is over and Sprint 100 belongs to another board.
    expect(titles.join(' ')).not.toContain('Sprint 6');
    expect(titles.join(' ')).not.toContain('Website refresh');
  });

  it('lists every per-item refusal and gates the confirm button on it', async () => {
    const user = userEvent.setup();
    render();

    await screen.findByRole('group', { name: 'What this close would grade' });
    const confirm = screen.getByRole('button', { name: 'Close sprint' });
    expect(confirm).toBeEnabled();

    await user.click(screen.getByRole('radio', { name: /Send it back to the backlog/ }));

    const refusals = await screen.findByRole('alert');
    const listed = within(refusals).getByRole('list', { name: 'Refused items' });
    expect(within(listed).getAllByRole('listitem')).toHaveLength(1);
    expect(listed).toHaveTextContent('WEB/WEB-US-0031');
    expect(listed).toHaveTextContent(/project WEB is not cloned/);

    // Until the refusal has been read, the close cannot be confirmed.
    expect(screen.getByRole('button', { name: 'Close sprint' })).toBeDisabled();
    await user.click(within(refusals).getByRole('checkbox'));
    expect(screen.getByRole('button', { name: 'Close sprint' })).toBeEnabled();
  });

  it('re-runs the preview when the destination changes', async () => {
    const user = userEvent.setup();
    render();

    await screen.findByRole('group', { name: 'What this close would grade' });
    // "Leave it here" moves nothing, so there is nothing to refuse.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    await user.click(screen.getByRole('radio', { name: /Send it back to the backlog/ }));
    expect(await screen.findByRole('alert')).toHaveTextContent('WEB/WEB-US-0031');
  });

  it('closes the sprint and reports the outcome with a toast', async () => {
    const user = userEvent.setup();
    const store = render();

    await screen.findByRole('group', { name: 'What this close would grade' });
    await user.click(screen.getByRole('radio', { name: /Carry it into another sprint/ }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Close sprint' })).toBeEnabled();
    });
    await user.click(screen.getByRole('button', { name: 'Close sprint' }));

    await waitFor(async () => {
      const sprint = await store.getSprint('ACME-TEAM-S-0007');
      expect(sprint.sprint.state).toBe('closed');
    });
    expect(await screen.findByText('Sprint 7 is closed')).toBeInTheDocument();
    // The unfinished work moved into the only sprint that could take it.
    const target = await store.getSprint('ACME-TEAM-S-0008');
    expect(target.sprint.items).toContain('ACME/ACME-US-0042');
  });

  it('says so when the preview itself cannot be computed', async () => {
    // A sprint that is not in the store at all: the preview fails, and the
    // dialog reports the reason rather than showing an empty report.
    const store = new FakeProvider({
      team: sampleTeam,
      boards: [sampleBoard, sampleScrumBoard],
      sprints: [current],
      today: TODAY,
    });
    renderWithRouter({
      index: () => (
        <ToastProvider>
          <CloseSprintDialog
            sprint={{ ...missingSummary }}
            open
            onOpenChange={() => {
              /* the dialog stays open for the assertion */
            }}
          />
        </ToastProvider>
      ),
      provider: store,
    });

    expect(await screen.findByRole('alert')).toHaveTextContent(/could not be previewed/);
    expect(screen.getByRole('button', { name: 'Close sprint' })).toBeDisabled();
  });
});

/** A sprint the workspace does not hold, for the failure path. */
const missingSummary = {
  id: 'ACME-TEAM-S-9999',
  title: 'Ghost',
  board: 'acme-scrum',
  state: 'active' as const,
  status: 'current' as const,
  items: [],
  totalDays: 0,
  remainingDays: 0,
  metrics: {
    items: 0,
    resolved: 0,
    done: 0,
    points: 0,
    committedPoints: 0,
    donePoints: 0,
    added: 0,
    unresolved: 0,
  },
};
