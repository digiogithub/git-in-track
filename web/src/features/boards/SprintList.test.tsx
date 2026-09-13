import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import {
  FakeProvider,
  sampleBoard,
  sampleScrumBoard,
  sampleTeam,
  type FakeSprint,
} from '@/api/fake-provider';
import type { SprintSummary } from '@/api/provider';
import { ToastProvider } from '@/components/ui/toast';
import { renderWithRouter } from '@/test/router';

import { groupSprints, SPRINT_STATUS_ORDER } from './sprint-grouping';
import { SprintList } from './SprintList';

/**
 * The sprint index (story GIT-US-0089, task GIT-T-0168).
 *
 * The grouping is the feature: a reader opens this page to find out what is
 * running, and the answer has to come from the status the core derived rather
 * than from the stored lifecycle state, which says something else entirely.
 */

const TODAY = '2026-09-02';

const sprints: FakeSprint[] = [
  {
    id: 'ACME-TEAM-S-0006',
    title: 'Sprint 6',
    board: 'acme-scrum',
    state: 'closed',
    start: '2026-08-10',
    end: '2026-08-23',
    items: [],
    rev: 'sha256:00000000000000c0',
  },
  {
    id: 'ACME-TEAM-S-0007',
    title: 'Sprint 7',
    board: 'acme-scrum',
    state: 'active',
    start: '2026-08-24',
    end: '2026-09-06',
    items: [],
    rev: 'sha256:00000000000000c1',
  },
  {
    id: 'ACME-TEAM-S-0008',
    title: 'Sprint 8',
    board: 'acme-scrum',
    state: 'planned',
    start: '2026-09-07',
    end: '2026-09-20',
    items: [],
    rev: 'sha256:00000000000000c2',
  },
  // No dates: a draft, whatever its stored state says.
  {
    id: 'ACME-TEAM-S-0009',
    title: 'Someday',
    board: 'acme-scrum',
    state: 'planned',
    items: [],
    rev: 'sha256:00000000000000c3',
  },
];

function render(rows: FakeSprint[] = sprints) {
  const store = new FakeProvider({
    team: sampleTeam,
    boards: [sampleBoard, sampleScrumBoard],
    sprints: rows,
    today: TODAY,
  });
  renderWithRouter({
    index: () => (
      <ToastProvider>
        <SprintList />
      </ToastProvider>
    ),
    provider: store,
  });
  return store;
}

/** A minimal summary, for the pure grouping test. */
function summary(id: string, status: SprintSummary['status']): SprintSummary {
  return {
    id,
    title: id,
    board: 'acme-scrum',
    state: 'planned',
    status,
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
}

describe('groupSprints', () => {
  it('orders the groups current → upcoming → draft → completed', () => {
    const grouped = groupSprints([
      summary('d', 'completed'),
      summary('c', 'draft'),
      summary('b', 'upcoming'),
      summary('a', 'current'),
    ]);

    expect(grouped.map(([status]) => status)).toEqual(SPRINT_STATUS_ORDER);
    expect(grouped.map(([, group]) => group[0]?.id)).toEqual(['a', 'b', 'c', 'd']);
  });

  it('drops the groups nothing falls into', () => {
    expect(groupSprints([summary('a', 'current')]).map(([status]) => status)).toEqual(['current']);
    expect(groupSprints([])).toEqual([]);
  });
});

describe('SprintList', () => {
  it('groups the sprints by derived status, in order', async () => {
    render();

    await screen.findByRole('region', { name: 'Current' });
    const headings = screen
      .getAllByRole('region')
      .map((section) => section.getAttribute('aria-label'));
    expect(headings).toEqual(['Current', 'Upcoming', 'Draft', 'Completed']);

    expect(
      within(screen.getByRole('region', { name: 'Current' })).getByText('Sprint 7'),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole('region', { name: 'Completed' })).getByText('Sprint 6'),
    ).toBeInTheDocument();
  });

  it('marks a dateless sprint as a draft and says what would schedule it', async () => {
    render();

    const drafts = await screen.findByRole('region', { name: 'Draft' });
    expect(within(drafts).getByText('Someday')).toBeInTheDocument();
    // The heading says "Draft" too, so the badge is asserted on the row itself.
    const row = within(drafts).getByRole('listitem');
    expect(within(row).getByText('Draft')).toBeInTheDocument();
    expect(within(drafts).getByText(/adding a start and an end date is what schedules it/i))
      .toBeInTheDocument();
  });

  it('has an empty state when the team runs no sprint at all', async () => {
    render([]);

    expect(await screen.findByText('No sprint yet')).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Current' })).not.toBeInTheDocument();
  });

  it('creates a draft when the dates are left empty', async () => {
    const user = userEvent.setup();
    const store = render();

    await screen.findByRole('region', { name: 'Current' });
    await user.selectOptions(screen.getByLabelText('Filter by board'), 'acme-scrum');
    await user.click(screen.getByRole('button', { name: 'New sprint' }));
    await user.type(screen.getByLabelText('Title'), 'Next quarter');
    expect(screen.getByText(/created as a draft/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Create sprint' }));

    const created = await screen
      .findByRole('region', { name: 'Draft' })
      .then((region) => within(region).findByText('Next quarter'));
    expect(created).toBeInTheDocument();

    const rows = await store.listSprints({ board: 'acme-scrum' });
    const draft = rows.find((row) => row.title === 'Next quarter');
    expect(draft?.status).toBe('draft');
    expect(draft?.start).toBeUndefined();
  });

  it('refuses one date without the other', async () => {
    const user = userEvent.setup();
    render();

    await screen.findByRole('region', { name: 'Current' });
    await user.selectOptions(screen.getByLabelText('Filter by board'), 'acme-scrum');
    await user.click(screen.getByRole('button', { name: 'New sprint' }));
    await user.type(screen.getByLabelText('Start'), '2026-10-01');

    expect(await screen.findByRole('alert')).toHaveTextContent(/both dates or neither/);
    expect(screen.getByRole('button', { name: 'Create sprint' })).toBeDisabled();
  });
});
