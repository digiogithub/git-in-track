import { describe, expect, it } from 'vitest';

import type { GitRefs, ImpactResult, SyncRepoStatus } from '@/api/provider';
import {
  formatRange,
  groupByTier,
  impactRange,
  parseImpactSearch,
  readReason,
  refSuggestions,
  tierStatusText,
} from '@/features/specs/impact';

const result: Pick<ImpactResult, 'tiers' | 'hits'> = {
  tiers: [
    { tier: 1, status: 'ok', hits: 1 },
    { tier: 2, status: 'ok', hits: 1 },
    { tier: 3, status: 'ok', hits: 2 },
  ],
  hits: [
    { ref: 'ACME-SP-0001.R1', title: 'Trim', tier: 1, reasons: ['symbol:a.go#Trim'] },
    { ref: 'ACME-SP-0001.R2', title: 'Keep', tier: 2, reasons: ['call:b.go#Keep calls Trim d1'] },
    {
      ref: 'ACME-SP-0002.R1',
      title: 'Low',
      tier: 3,
      candidate: true,
      score: 0.41,
      reasons: ['semantic'],
    },
    {
      ref: 'ACME-SP-0002.R2',
      title: 'High',
      tier: 3,
      candidate: true,
      score: 0.9,
      reasons: ['semantic'],
    },
  ],
};

describe('groupByTier', () => {
  it('always yields three groups in tier order, candidates ranked by score', () => {
    const groups = groupByTier(result);
    expect(groups.map((g) => g.tier)).toEqual([1, 2, 3]);
    expect(groups[0]?.hits.map((h) => h.ref)).toEqual(['ACME-SP-0001.R1']);
    expect(groups[1]?.hits.map((h) => h.ref)).toEqual(['ACME-SP-0001.R2']);
    expect(groups[2]?.hits.map((h) => h.ref)).toEqual(['ACME-SP-0002.R2', 'ACME-SP-0002.R1']);
  });

  it('never puts a candidate among the certain hits', () => {
    const groups = groupByTier({
      tiers: [],
      hits: [{ ref: 'X-SP-0001.R1', title: 't', tier: 1, candidate: true, reasons: [] }],
    });
    expect(groups[0]?.hits).toEqual([]);
    expect(groups[2]?.hits).toHaveLength(1);
  });

  it('carries each tier status', () => {
    const groups = groupByTier({
      tiers: [{ tier: 2, status: 'unavailable', hits: 0, message: 'no Pando' }],
      hits: [],
    });
    expect(groups[0]?.status).toBeUndefined();
    expect(groups[1]?.status?.status).toBe('unavailable');
  });
});

describe('tierStatusText', () => {
  it.each([
    [{ tier: 1 as const, status: 'ok' as const, hits: 0 }, undefined],
    [{ tier: 2 as const, status: 'ok' as const, hits: 0, truncated: true }, 'Partial'],
    [{ tier: 3 as const, status: 'skipped' as const, hits: 0 }, 'Skipped'],
    [{ tier: 2 as const, status: 'unavailable' as const, hits: 0, message: 'down' }, 'down'],
    [{ tier: 3 as const, status: 'error' as const, hits: 0, message: 'not indexed' }, 'Error'],
    [undefined, 'Not reported'],
  ])('%j', (status, expected) => {
    const text = tierStatusText(status);
    if (expected === undefined) expect(text).toBeUndefined();
    else expect(text).toContain(expected);
  });
});

describe('readReason', () => {
  it.each([
    ['call:b.go#Keep calls Trim d2', 'called from', 'b.go#Keep → Trim, depth 2'],
    ['symbol:a.go#Trim', 'symbol', 'a.go#Trim'],
    ['delta:ACME-US-0001', 'pending delta', 'ACME-US-0001'],
    ['semantic', 'semantic', ''],
    ['+3', 'more', '3 more'],
  ])('%s', (reason, kind, text) => {
    expect(readReason(reason)).toMatchObject({ kind, text, title: reason });
  });
});

describe('range', () => {
  it('defaults the base to main and the head to the working tree', () => {
    expect(impactRange(parseImpactSearch({}))).toEqual({ base: 'main' });
    expect(impactRange(parseImpactSearch({ head: 'worktree' }))).toEqual({ base: 'main' });
    expect(impactRange(parseImpactSearch({ base: 'v1.0', head: 'HEAD' }))).toEqual({
      base: 'v1.0',
      head: 'HEAD',
    });
    expect(parseImpactSearch({ base: 42, head: '  ' })).toEqual({});
    expect(formatRange('main', undefined)).toBe('main..worktree');
  });

  it('suggests the branch, its upstream and recent commits', () => {
    const rows = [
      {
        repo: 'r',
        path: '/r',
        git: true,
        pending: 0,
        status: {
          branch: 'feat/x',
          detached: false,
          upstream: 'origin/feat/x',
          clean: true,
          trackedChanges: false,
          ahead: 0,
          behind: 0,
          state: 'up_to_date',
        },
      },
    ] satisfies SyncRepoStatus[];
    const got = refSuggestions(rows);
    expect(got.base.slice(0, 3).map((o) => o.value)).toEqual(['main', 'feat/x', 'origin/feat/x']);
    expect(got.head[0]).toEqual({ value: 'worktree', label: 'working tree' });
    expect(got.head.map((o) => o.value)).toContain('HEAD~1');
  });

  it('suggests every branch and the recent commits of the ref listing', () => {
    const refs = {
      repo: 'r',
      backend: 'jj',
      branches: [
        { name: 'main', sha: 'a'.repeat(40), current: true },
        { name: 'feat/y', sha: 'b'.repeat(40) },
        { name: 'origin/main', remote: 'origin', sha: 'c'.repeat(40) },
      ],
      commits: [
        { sha: '0123456789abcdef0123', subject: 'feat: add y', date: '2026-09-02T10:00:00Z' },
        { sha: 'fedcba9876543210fedc', subject: '' },
      ],
    } satisfies GitRefs;
    const got = refSuggestions(undefined, refs);
    expect(got.base).toEqual([
      { value: 'main' },
      { value: 'feat/y', label: 'branch' },
      { value: 'origin/main', label: 'remote branch' },
      { value: 'HEAD' },
      { value: '0123456789ab', label: 'feat: add y · 2026-09-02' },
      { value: 'fedcba987654', label: '(no description)' },
    ]);
    // The listing replaces the HEAD~n guesses; the working tree leads the head.
    expect(got.head[0]?.value).toBe('worktree');
    expect(got.head.map((o) => o.value)).not.toContain('HEAD~1');
    expect(got.head.find((o) => o.value === 'main')?.label).toBe('current branch');
  });
});
