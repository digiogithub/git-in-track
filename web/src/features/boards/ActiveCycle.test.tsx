import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import {
  FakeProvider,
  sampleBoard,
  sampleScrumBoard,
  sampleTeam,
  type FakeSprint,
} from '@/api/fake-provider';
import { renderWithRouter } from '@/test/router';

import { ActiveCycle } from './ActiveCycle';
import { useSprint } from './sprint-queries';

/**
 * The active-cycle panel (story GIT-US-0089, task GIT-T-0161).
 *
 * Three things are worth being strict about: a draft never shows a progress bar,
 * the provenance note sits above the chart whatever the source, and a
 * snapshot-backed panel says the numbers were frozen instead of implying they
 * are still moving.
 */

const TODAY = '2026-09-02';

/** The running sprint of the sample scrum board: one committed 8-point story. */
const current: FakeSprint = {
  id: 'ACME-TEAM-S-0007',
  title: 'Sprint 7',
  board: 'acme-scrum',
  state: 'active',
  start: '2026-08-24',
  end: '2026-09-06',
  goal: 'SSO end to end',
  items: ['ACME/ACME-US-0042'],
  committed: ['ACME/ACME-US-0042'],
  rev: 'sha256:00000000000000c1',
};

const upcoming: FakeSprint = {
  ...current,
  id: 'ACME-TEAM-S-0008',
  title: 'Sprint 8',
  state: 'planned',
  start: '2026-09-07',
  end: '2026-09-20',
  committed: [],
};

/** No dates at all: a draft until someone schedules it. */
const draft: FakeSprint = {
  id: 'ACME-TEAM-S-0009',
  title: 'Someday',
  board: 'acme-scrum',
  state: 'planned',
  items: [],
  rev: 'sha256:00000000000000c3',
};

function provider(sprints: FakeSprint[]) {
  return new FakeProvider({
    team: sampleTeam,
    boards: [sampleBoard, sampleScrumBoard],
    sprints,
    today: TODAY,
  });
}

/**
 * The summary comes from the provider through the ordinary hook, because the
 * derived status is the core's answer and this component must never compute it.
 */
function Panel({ id }: { id: string }) {
  const sprint = useSprint(id);
  if (!sprint.data) return <p>Loading…</p>;
  return <ActiveCycle sprint={sprint.data.sprint} />;
}

function render(id: string, store: FakeProvider) {
  renderWithRouter({ index: () => <Panel id={id} />, provider: store });
  return store;
}

describe('ActiveCycle', () => {
  it('shows a current sprint with its dates, days left and progress', async () => {
    render('ACME-TEAM-S-0007', provider([current]));

    const panel = await screen.findByTestId('active-cycle');
    expect(within(panel).getByText('Current')).toBeInTheDocument();
    expect(within(panel).getByText('2026-08-24 → 2026-09-06')).toBeInTheDocument();
    expect(within(panel).getByText('5 of 14 days left')).toBeInTheDocument();
    // ACME-US-0042 is 8 points and was committed; nothing is done yet.
    const bar = within(panel).getByRole('progressbar');
    expect(bar).toHaveAttribute('aria-valuemax', '8');
    expect(bar).toHaveAttribute('aria-valuenow', '0');
    expect(within(panel).getByText('Done against committed')).toBeInTheDocument();
  });

  it('counts the work that arrived after the sprint started', async () => {
    render(
      'ACME-TEAM-S-0007',
      provider([{ ...current, items: ['ACME/ACME-US-0042', 'ACME/ACME-US-0043'] }]),
    );

    const panel = await screen.findByTestId('active-cycle');
    expect(within(panel).getByText('Added mid-sprint').nextSibling).toHaveTextContent('1');
  });

  it('shows an upcoming sprint as starting, not as running', async () => {
    render('ACME-TEAM-S-0008', provider([upcoming]));

    const panel = await screen.findByTestId('active-cycle');
    expect(within(panel).getByText('Upcoming')).toBeInTheDocument();
    expect(within(panel).getByText('Starts on 2026-09-07')).toBeInTheDocument();
  });

  it('shows a draft its planning state instead of a progress bar', async () => {
    render('ACME-TEAM-S-0009', provider([draft]));

    const panel = await screen.findByTestId('active-cycle');
    expect(within(panel).getByText('Draft')).toBeInTheDocument();
    expect(within(panel).getByText('No dates yet')).toBeInTheDocument();
    expect(within(panel).getByText('Still a draft')).toBeInTheDocument();
    expect(within(panel).getByText(/Adding a start and an end date/)).toBeInTheDocument();
    // A bar at zero would read as "no work done" rather than "not scheduled".
    expect(within(panel).queryByRole('progressbar')).not.toBeInTheDocument();
  });

  it('prints the provenance note above the chart', async () => {
    render('ACME-TEAM-S-0007', provider([current]));

    const note = await screen.findByRole('note');
    const chart = await screen.findByRole('img', { name: /Burndown of/ });
    expect(note.textContent).not.toBe('');
    // "Above" is a DOM fact, not a styling one: the note precedes the figure.
    expect(note.compareDocumentPosition(chart) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('says a closed sprint reads numbers that were frozen at the close', async () => {
    const store = provider([current]);
    // Closing freezes a snapshot into the sprint file; the metrics then come
    // from it rather than from a reconstruction that is no longer possible.
    await store.closeSprint('ACME-TEAM-S-0007', {});
    render('ACME-TEAM-S-0007', store);

    const note = await screen.findByRole('note');
    expect(note).toHaveTextContent('Frozen at the close.');
    expect(note).toHaveTextContent(/frozen when the sprint was closed/);
  });
});
