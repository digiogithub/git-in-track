import { describe, expect, it } from 'vitest';

import type { ProjectKpis } from '@/api/provider';

import { kpiEntries, progressFormula, progressPercent } from './kpis';

function kpis(overrides: Partial<ProjectKpis> = {}): ProjectKpis {
  return {
    project: 'ACME',
    kbPages: 12,
    milestones: { total: 3, open: 1 },
    epics: { total: 4, open: 2 },
    stories: { total: 10, open: 6, done: 3, cancelled: 1, estimated: 0 },
    specs: { total: 2, unverified: 1, requirements: 5, unverifiedRequirements: 2 },
    progress: { ratio: 3 / 9, weighted: false, done: 3, scope: 9 },
    ...overrides,
  };
}

describe('workspace KPIs (GIT-US-0205)', () => {
  it('rounds the closed share to a whole percentage and clamps it', () => {
    expect(progressPercent(kpis())).toBe(33);
    expect(
      progressPercent(kpis({ progress: { ratio: 1.4, weighted: false, done: 1, scope: 1 } })),
    ).toBe(100);
    expect(
      progressPercent(
        kpis({ progress: { ratio: Number.NaN, weighted: false, done: 0, scope: 0 } }),
      ),
    ).toBe(0);
  });

  it('explains a count-based ratio', () => {
    expect(progressFormula(kpis())).toBe(
      'Estimated 33% closed: 3 of 9 stories done. Done stories ÷ non-cancelled stories; ' +
        'no story carries an estimate, so each counts the same.',
    );
  });

  it('explains an estimate-weighted ratio', () => {
    const weighted = kpis({
      stories: { total: 4, open: 2, done: 1, cancelled: 1, estimated: 2 },
      progress: { ratio: 8 / 15, weighted: true, done: 8, scope: 15 },
    });
    expect(progressFormula(weighted)).toBe(
      'Estimated 53% closed: 8 of 15 story points done. Points of done stories ÷ points of ' +
        'non-cancelled stories; a story without an estimate counts as the average estimate ' +
        '(2 of 3 stories are estimated).',
    );
  });

  it('says how the ratio is computed before any story exists', () => {
    expect(
      progressFormula(kpis({ progress: { ratio: 0, weighted: false, done: 0, scope: 0 } })),
    ).toMatch(/no open or done stories yet/i);
  });

  it('lists the five figures in order, flagging unverified specs', () => {
    const entries = kpiEntries(kpis());
    expect(entries.map((e) => [e.kind, e.value])).toEqual([
      ['docs', '12'],
      ['milestones', '1 open / 3'],
      ['epics', '2 open / 4'],
      ['stories', '6 left / 10'],
      ['specs', '2 · 1 unverified'],
    ]);
    expect(entries.find((e) => e.kind === 'specs')?.attention).toBe(true);
    expect(
      kpiEntries(
        kpis({ specs: { total: 2, unverified: 0, requirements: 5, unverifiedRequirements: 0 } }),
      ).find((e) => e.kind === 'specs'),
    ).toMatchObject({ value: '2', attention: false });
  });
});
