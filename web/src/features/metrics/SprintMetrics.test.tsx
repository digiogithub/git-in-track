import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { SprintMetricsView } from '@/api/provider';

import { ProvenanceBanner, SprintMetricsBody } from './SprintMetrics';

/**
 * The metrics page (story GIT-US-0028).
 *
 * These tests are about what the page promises a reader: that the numbers come
 * with the table behind them, that a day nobody measured is never drawn as a
 * zero, and that an approximation says so before it shows a single chart.
 */

function view(overrides: Partial<SprintMetricsView> = {}): SprintMetricsView {
  const base: SprintMetricsView = {
    sprint: {
      id: 'DEMO-TEAM-S-0001',
      title: 'Sprint 1',
      board: 'demo-scrum',
      state: 'active',
      start: '2026-03-02',
      end: '2026-03-04',
      items: ['DEMO/DEMO-US-0001', 'DEMO/DEMO-US-0002'],
      totalDays: 3,
      remainingDays: 1,
      metrics: {
        items: 2,
        resolved: 2,
        done: 1,
        points: 8,
        committedPoints: 8,
        donePoints: 3,
        added: 0,
        unresolved: 0,
      },
    },
    burndown: {
      sprint: 'DEMO-TEAM-S-0001',
      start: '2026-03-02',
      end: '2026-03-04',
      committedPoints: 8,
      points: [
        {
          date: '2026-03-02',
          day: 1,
          ideal: 8,
          observed: true,
          remaining: 8,
          scope: 8,
          done: 0,
          items: 2,
          completed: 0,
          unknown: 0,
        },
        {
          date: '2026-03-03',
          day: 2,
          ideal: 4,
          observed: true,
          remaining: 5,
          scope: 8,
          done: 3,
          items: 2,
          completed: 1,
          unknown: 0,
        },
        {
          date: '2026-03-04',
          day: 3,
          ideal: 0,
          observed: false,
          remaining: 0,
          scope: 0,
          done: 0,
          items: 0,
          completed: 0,
          unknown: 0,
        },
      ],
    },
    flow: {
      bands: ['done', 'cancelled', 'in_progress', 'todo', 'unknown'],
      days: [
        {
          date: '2026-03-02',
          day: 1,
          observed: true,
          counts: { done: 0, cancelled: 0, in_progress: 1, todo: 1, unknown: 0 },
          total: 2,
        },
        {
          date: '2026-03-03',
          day: 2,
          observed: true,
          counts: { done: 1, cancelled: 0, in_progress: 1, todo: 0, unknown: 0 },
          total: 2,
        },
        {
          date: '2026-03-04',
          day: 3,
          observed: false,
          counts: { done: 0, cancelled: 0, in_progress: 0, todo: 0, unknown: 0 },
          total: 0,
        },
      ],
    },
    stats: {
      throughput: 1,
      throughputPerWeek: 2.33,
      cycleTime: { count: 1, mean: 1.25, median: 1.25, p85: 1.25, min: 1.25, max: 1.25 },
      leadTime: { count: 0, mean: 0, median: 0, p85: 0, min: 0, max: 0 },
      excluded: 0,
    },
    provenance: {
      source: 'git',
      approximate: false,
      items: 2,
      covered: 2,
      note: 'Reconstructed from the git history of the item files, back to 2026-02-25.',
    },
    items: [],
  };
  return { ...base, ...overrides };
}

describe('the sprint metrics page', () => {
  it('leads with where the numbers came from', () => {
    render(<SprintMetricsBody view={view()} />);

    const note = screen.getByRole('note');
    expect(within(note).getByText(/Reconstructed from git history/)).toBeInTheDocument();
    expect(within(note).getByText(/back to 2026-02-25/)).toBeInTheDocument();
  });

  it('says plainly when the series is an approximation', () => {
    render(
      <ProvenanceBanner
        provenance={{
          source: 'updated',
          approximate: true,
          items: 3,
          covered: 3,
          note: "Approximated from each item's `updated` stamp.",
        }}
      />,
    );

    expect(screen.getByText(/These numbers are an approximation/)).toBeInTheDocument();
    expect(screen.getByText(/updated. stamp/)).toBeInTheDocument();
  });

  it('reports a partial history instead of counting the gap as work', () => {
    render(
      <ProvenanceBanner
        provenance={{
          source: 'git',
          approximate: false,
          items: 3,
          covered: 1,
          note: 'Reconstructed from the git history of the item files.',
        }}
      />,
    );

    expect(screen.getByText(/1 of 3 references have a readable history/)).toBeInTheDocument();
  });

  it('gives every chart a data table with the numbers behind it', async () => {
    const user = userEvent.setup();
    render(<SprintMetricsBody view={view()} />);

    await user.click(screen.getByText('Burndown as a table'));
    const burndown = screen.getByRole('table', { name: /Every day of DEMO-TEAM-S-0001/ });
    // The measured day carries its remaining points; the future day says so
    // rather than reading as zero.
    expect(within(burndown).getByText('not measured')).toBeInTheDocument();
    expect(within(burndown).getAllByText('5').length).toBeGreaterThan(0);

    await user.click(screen.getByText('Cumulative flow as a table'));
    const flow = screen.getByRole('table', { name: /Item counts by status/ });
    expect(within(flow).getByText('2026-03-03')).toBeInTheDocument();
    // The future day is not a row: it was never measured.
    expect(within(flow).queryByText('2026-03-04')).not.toBeInTheDocument();
  });

  it('draws both charts with an accessible description', () => {
    render(<SprintMetricsBody view={view()} />);

    expect(screen.getByRole('img', { name: /Burndown of DEMO-TEAM-S-0001/ })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: /Cumulative flow/ })).toBeInTheDocument();
  });

  it('names every series in a legend, so colour is never the only channel', () => {
    render(<SprintMetricsBody view={view()} />);

    const legends = screen.getAllByRole('list');
    const labels = legends.flatMap((list) =>
      within(list)
        .queryAllByRole('listitem')
        .map((item) => item.textContent),
    );
    for (const label of ['Remaining', 'Ideal', 'Done', 'In progress', 'To do']) {
      expect(labels).toContain(label);
    }
  });

  it('does not invent a statistic it has no sample for', () => {
    render(<SprintMetricsBody view={view()} />);

    expect(screen.getByText('No item in this sprint has a measurable one.')).toBeInTheDocument();
  });

  it('plots nothing for a sprint that has no days', () => {
    const empty = view();
    empty.burndown = { ...empty.burndown, points: [] };
    empty.flow = { ...empty.flow, days: [] };
    render(<SprintMetricsBody view={empty} />);

    expect(screen.getByText(/no dates, so it has no days to plot/)).toBeInTheDocument();
    expect(screen.getByText(/No day of this sprint has been measured yet/)).toBeInTheDocument();
  });
});
