import { describe, expect, it } from 'vitest';

import type { CoverageRow, Item, Requirement } from '@/api/provider';
import {
  buildMatrix,
  countStatuses,
  joinRows,
  parseMatrixSearch,
  splitTest,
  summarizeSpecs,
  visibleRange,
} from '@/features/specs/matrix';

function requirement(ref: string, title: string): Requirement {
  const [spec = ''] = ref.split('.');
  return {
    ref,
    spec,
    path: `docs/.pmngr/specs/${spec}.md`,
    anchor: ref.toLowerCase().replace('.', '-'),
    line: 1,
    title,
    status: 'todo',
    rev: `sha256:r-${ref}`,
    blockRev: `sha256:b-${ref}`,
  };
}

const requirements = [
  requirement('ACME-SP-0001.R10', 'Refuse an empty cart'),
  requirement('ACME-SP-0001.R2', 'Keep the cart'),
  requirement('ACME-SP-0002.R1', 'IDs are never reused'),
];

const coverage: CoverageRow[] = [
  {
    ref: 'ACME-SP-0001.R2',
    status: 'failing',
    reasons: ['failed', 'results'],
    tests: [
      { test: 'web/cart.test.ts#keeps', result: 'fail' },
      { test: 'internal/cart_test.go#TestKeep', result: 'pass' },
    ],
  },
  {
    ref: 'ACME-SP-0002.R1',
    status: 'passing',
    reasons: ['results'],
    tests: [
      { test: 'internal/cart_test.go#TestAlloc', result: 'skip' },
      { test: 'scripts/check.sh', result: 'missing' },
    ],
  },
];

describe('coverage matrix model', () => {
  it('splits a test id into file and symbol', () => {
    expect(splitTest('a/b_test.go#TestX')).toEqual({
      test: 'a/b_test.go#TestX',
      file: 'a/b_test.go',
      symbol: 'TestX',
    });
    expect(splitTest('scripts/check.sh')).toMatchObject({ file: 'scripts/check.sh', symbol: '' });
  });

  it('joins requirements with coverage in ref order; unnamed ones are untested', () => {
    const rows = joinRows(requirements, coverage);
    expect(rows.map((row) => [row.ref, row.status])).toEqual([
      ['ACME-SP-0001.R2', 'failing'],
      ['ACME-SP-0001.R10', 'untested'],
      ['ACME-SP-0002.R1', 'passing'],
    ]);
    expect(rows[0]?.reasons).toEqual(['failed', 'results']);
    expect(rows[1]?.results.size).toBe(0);
  });

  it('groups the columns by file, files and symbols sorted', () => {
    const matrix = buildMatrix(joinRows(requirements, coverage), {});
    expect(matrix.groups.map((g) => [g.file, g.columns.map((c) => c.symbol)])).toEqual([
      ['internal/cart_test.go', ['TestAlloc', 'TestKeep']],
      ['scripts/check.sh', ['']],
      ['web/cart.test.ts', ['keeps']],
    ]);
    expect(matrix.columns).toHaveLength(4);
  });

  it('filters by spec and status, narrowing the columns to the rows kept', () => {
    const rows = joinRows(requirements, coverage);
    const bySpec = buildMatrix(rows, { spec: ['ACME-SP-0002'] });
    expect(bySpec.rows.map((row) => row.ref)).toEqual(['ACME-SP-0002.R1']);
    expect(bySpec.groups.map((g) => g.file)).toEqual(['internal/cart_test.go', 'scripts/check.sh']);

    const byStatus = buildMatrix(rows, { status: ['untested', 'failing'] });
    expect(byStatus.rows.map((row) => row.ref)).toEqual(['ACME-SP-0001.R2', 'ACME-SP-0001.R10']);
  });

  it('counts per status and per spec', () => {
    const rows = joinRows(requirements, coverage);
    expect(countStatuses(rows)).toEqual({ untested: 1, passing: 1, failing: 1, suspect: 0 });
    const specs = [{ id: 'ACME-SP-0003', title: 'Empty' } as Item];
    expect(summarizeSpecs(specs, rows).map((s) => [s.id, s.total, s.counts.failing])).toEqual([
      ['ACME-SP-0001', 2, 1],
      ['ACME-SP-0002', 1, 0],
      ['ACME-SP-0003', 0, 0],
    ]);
  });

  it('parses the URL filter and drops unknown states', () => {
    expect(parseMatrixSearch({ spec: 'ACME-SP-0001,ACME-SP-0002', status: 'failing' })).toEqual({
      spec: ['ACME-SP-0001', 'ACME-SP-0002'],
      status: ['failing'],
    });
    expect(parseMatrixSearch({ status: 'bogus' })).toEqual({});
  });

  it('computes a bounded window of rows', () => {
    expect(visibleRange(0, 0, 480, 64, 8)).toEqual({ start: 0, end: 0 });
    expect(visibleRange(10_000, 0, 480, 64, 8)).toEqual({ start: 0, end: 16 });
    expect(visibleRange(10_000, 64 * 5_000, 480, 64, 8)).toEqual({ start: 4_992, end: 5_016 });
    expect(visibleRange(10, 64 * 50, 480, 64, 8)).toEqual({ start: 1, end: 10 });
  });
});
