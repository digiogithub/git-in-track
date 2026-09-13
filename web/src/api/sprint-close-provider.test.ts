/**
 * The closing half of the sprint provider surface (story GIT-US-0089, task
 * GIT-T-0157): the derived status, the dry run, the real close and the
 * standalone transfer.
 *
 * The dry run is the one worth being strict about. It is what the confirmation
 * dialog renders, so it has to produce the whole report — including the per-item
 * refusals — while writing absolutely nothing.
 */

import { describe, expect, it } from 'vitest';

import {
  FakeProvider,
  sampleBoard,
  sampleItems,
  sampleProject,
  sampleScrumBoard,
  sampleTeam,
  type FakeSprint,
} from '@/api/fake-provider';
import type { ChangeEvent } from '@/api/provider';

const TODAY = '2026-09-02';

const current: FakeSprint = {
  id: 'ACME-TEAM-S-0007',
  title: 'Sprint 7',
  board: 'acme-scrum',
  state: 'active',
  start: '2026-08-24',
  end: '2026-09-06',
  goal: 'SSO end to end',
  // ACME-US-0042 is in progress in the clone; WEB/WEB-US-0031 belongs to a
  // project this workspace has not cloned, which is what produces a refusal.
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

/** A sprint with no dates at all: a draft until someone schedules it. */
const draft: FakeSprint = {
  id: 'ACME-TEAM-S-0009',
  title: 'Someday',
  board: 'acme-scrum',
  state: 'planned',
  items: [],
  rev: 'sha256:00000000000000c3',
};

function provider() {
  return new FakeProvider({
    projects: [sampleProject],
    items: sampleItems,
    team: sampleTeam,
    boards: [sampleBoard, sampleScrumBoard],
    sprints: [over, current, upcoming, draft],
    today: TODAY,
  });
}

describe('derived sprint status', () => {
  it('comes from the dates and today, and a sprint with no dates is a draft', async () => {
    const sprints = await provider().listSprints({ board: 'acme-scrum' });
    const byId = Object.fromEntries(sprints.map((s) => [s.id, s.status]));

    expect(byId['ACME-TEAM-S-0006']).toBe('completed');
    expect(byId['ACME-TEAM-S-0007']).toBe('current');
    expect(byId['ACME-TEAM-S-0008']).toBe('upcoming');
    expect(byId['ACME-TEAM-S-0009']).toBe('draft');
  });

  it('creates a sprint with no dates instead of refusing it', async () => {
    const result = await provider().createSprint({ board: 'acme-scrum', start: '', end: '' });

    expect(result.sprint.sprint.status).toBe('draft');
    expect(result.sprint.sprint.start).toBeUndefined();
  });

  it('still refuses two dated sprints of one board that share a day', async () => {
    await expect(
      provider().createSprint({ board: 'acme-scrum', start: '2026-09-01', end: '2026-09-10' }),
    ).rejects.toMatchObject({ code: 'sprint_overlap' });
  });
});

describe('closeSprint with dryRun', () => {
  it('reports what would move and writes nothing at all', async () => {
    const store = provider();
    const seen: ChangeEvent[] = [];
    store.subscribe((event) => seen.push(event));

    const preview = await store.closeSprint('ACME-TEAM-S-0007', {
      transfer: { mode: 'next', target: 'ACME-TEAM-S-0008' },
      dryRun: true,
    });

    expect(preview.dryRun).toBe(true);
    expect(preview.report?.incomplete.map((card) => card.ref)).toEqual([
      'ACME/ACME-US-0042',
      'WEB/WEB-US-0031',
    ]);
    expect(preview.report?.carried).toContainEqual({
      ref: 'ACME/ACME-US-0042',
      action: 'next',
      sprint: 'ACME-TEAM-S-0008',
    });

    // Nothing moved: the sprint is still running, the target is still empty and
    // no event was published.
    const sprints = await store.listSprints({ board: 'acme-scrum' });
    expect(sprints.find((s) => s.id === 'ACME-TEAM-S-0007')?.state).toBe('active');
    expect(sprints.find((s) => s.id === 'ACME-TEAM-S-0008')?.items).toEqual([]);
    expect(seen).toEqual([]);
  });

  it('lists a per-item refusal in the preview, before anything is confirmed', async () => {
    const preview = await provider().closeSprint('ACME-TEAM-S-0007', {
      transfer: { mode: 'backlog' },
      dryRun: true,
    });

    const refused = (preview.report?.carried ?? []).filter((one) => one.error !== undefined);
    expect(refused).toHaveLength(1);
    expect(refused[0]?.ref).toBe('WEB/WEB-US-0031');
    expect(refused[0]?.error).toMatch(/not cloned/);
  });

  it('refuses a transfer aimed at a sprint that is already over', async () => {
    await expect(
      provider().closeSprint('ACME-TEAM-S-0007', {
        transfer: { mode: 'next', target: 'ACME-TEAM-S-0006' },
        dryRun: true,
      }),
    ).rejects.toMatchObject({ code: 'sprint_target_completed' });
  });
});

describe('closeSprint', () => {
  it('closes the sprint, moves the work and freezes a snapshot', async () => {
    const store = provider();
    const result = await store.closeSprint('ACME-TEAM-S-0007', {
      transfer: { mode: 'next', target: 'ACME-TEAM-S-0008' },
    });

    expect(result.dryRun).toBeUndefined();
    expect(result.sprint.sprint.state).toBe('closed');
    expect(result.sprint.sprint.snapshot?.closedAt).toBe(`${TODAY}T00:00:00Z`);
    const sprints = await store.listSprints({ board: 'acme-scrum' });
    expect(sprints.find((s) => s.id === 'ACME-TEAM-S-0008')?.items).toContain('ACME/ACME-US-0042');
  });

  it('answers a closed sprint metrics from the snapshot, with no flow series', async () => {
    const store = provider();
    await store.closeSprint('ACME-TEAM-S-0007', {});
    const metrics = await store.getSprintMetrics('ACME-TEAM-S-0007');

    expect(metrics.provenance.source).toBe('snapshot');
    expect(metrics.provenance.note).toMatch(/frozen when the sprint was closed/);
    expect(metrics.burndown.points.length).toBeGreaterThan(0);
    // The cumulative-flow series is not frozen at the close, so it comes back
    // empty rather than as zeros a chart would draw as a flat line.
    expect(metrics.flow.days).toEqual([]);
    expect(metrics.stats.cycleTime.count).toBe(0);
  });

  it('announces the close so another view can refresh itself', async () => {
    const store = provider();
    const seen: ChangeEvent[] = [];
    store.subscribe((event) => seen.push(event));

    await store.closeSprint('ACME-TEAM-S-0007', {
      transfer: { mode: 'next', target: 'ACME-TEAM-S-0008' },
    });

    expect(seen).toContainEqual(
      expect.objectContaining({ kind: 'sprint', sprint: 'ACME-TEAM-S-0007', carried: 2 }),
    );
  });

  it('refuses a close taken on a revision that has moved on', async () => {
    await expect(
      provider().closeSprint('ACME-TEAM-S-0007', { rev: 'sha256:stale' }),
    ).rejects.toMatchObject({ code: 'stale_revision' });
  });
});

describe('transferSprintItems', () => {
  it('moves the unfinished scope without closing either sprint', async () => {
    const store = provider();
    const result = await store.transferSprintItems('ACME-TEAM-S-0007', {
      mode: 'next',
      target: 'ACME-TEAM-S-0008',
    });

    expect(result.sprint.sprint.state).toBe('active');
    const sprints = await store.listSprints({ board: 'acme-scrum' });
    expect(sprints.find((s) => s.id === 'ACME-TEAM-S-0008')?.items).toContain('ACME/ACME-US-0042');
  });

  it('previews a transfer without moving anything', async () => {
    const store = provider();
    const preview = await store.transferSprintItems('ACME-TEAM-S-0007', {
      mode: 'next',
      target: 'ACME-TEAM-S-0008',
      dryRun: true,
    });

    expect(preview.dryRun).toBe(true);
    const sprints = await store.listSprints({ board: 'acme-scrum' });
    expect(sprints.find((s) => s.id === 'ACME-TEAM-S-0008')?.items).toEqual([]);
  });
});
