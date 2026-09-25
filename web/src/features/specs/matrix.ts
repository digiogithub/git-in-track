/**
 * The pure half of the coverage matrix (`/p/$project/specs/coverage`, story
 * GIT-US-0130): requirements as rows, linked tests as columns grouped by file,
 * the URL filter and the summary counts. Kept free of React so the rules are
 * testable on their own.
 *
 * A test id is the trace ref of its edge, `<path>[#<symbol>]` (doc 03 §21.7):
 * the path is the column group, the symbol the column.
 */

import type { SearchSchemaInput } from '@tanstack/react-router';
import { z } from 'zod';

import type { CoverageRow, Item, Requirement } from '@/api/provider';
import { coverageStates, type CoverageState } from '@/features/specs/search';

/** A linked test's latest local result; `missing` is "no result yet". */
export type TestResult = NonNullable<CoverageRow['tests']>[number]['result'];

/** One test column: the whole id, and its file and symbol halves. */
export type MatrixColumn = { test: string; file: string; symbol: string };

/** The columns of one file, in symbol order. */
export type MatrixColumnGroup = { file: string; columns: MatrixColumn[] };

/** One requirement row: its computed status, the reasons, and a result per linked test. */
export type MatrixRow = {
  ref: string;
  spec: string;
  title: string;
  anchor: string;
  status: CoverageState;
  reasons: string[];
  results: ReadonlyMap<string, TestResult>;
};

export type Matrix = {
  rows: MatrixRow[];
  groups: MatrixColumnGroup[];
  /** Flattened `groups`, in display order. */
  columns: MatrixColumn[];
};

export type StatusCounts = Record<CoverageState, number>;

export type SpecSummary = {
  id: string;
  title: string;
  total: number;
  counts: StatusCounts;
};

function toList(value: unknown): string[] | undefined {
  if (value === undefined || value === null || value === '') return undefined;
  const raw: unknown[] = Array.isArray(value)
    ? value
    : typeof value === 'string'
      ? value.split(',')
      : [];
  const items = raw
    .filter((entry): entry is string => typeof entry === 'string')
    .map((entry) => entry.trim())
    .filter(Boolean);
  return items.length > 0 ? [...new Set(items)] : undefined;
}

/**
 * `?spec=ACME-SP-0001,ACME-SP-0002&status=failing,suspect`. Unlike the specs
 * page, `status` here is the computed coverage state: the matrix has no
 * workflow-status filter.
 */
const matrixSearchSchema = z.object({
  spec: z.preprocess(toList, z.array(z.string()).optional()).catch(undefined),
  status: z.preprocess(toList, z.array(z.enum(coverageStates)).optional()).catch(undefined),
});

export type MatrixSearch = z.infer<typeof matrixSearchSchema>;

export type MatrixSearchInput = { spec?: string | undefined; status?: string | undefined };

/** Parses raw search params. Never throws; a bad value degrades to "no filter". */
export function parseMatrixSearch(input: unknown): MatrixSearch {
  const result = matrixSearchSchema.safeParse(input ?? {});
  if (!result.success) return {};
  const { spec, status } = result.data;
  return { ...(spec ? { spec } : {}), ...(status ? { status } : {}) };
}

/** Route `validateSearch` for `/p/$project/specs/coverage`. */
export function validateMatrixSearch(input: MatrixSearchInput & SearchSchemaInput): MatrixSearch {
  return parseMatrixSearch(input);
}

/** Splits `<path>[#<symbol>]`; a test without a symbol is the whole file. */
export function splitTest(test: string): MatrixColumn {
  const hash = test.indexOf('#');
  if (hash < 0) return { test, file: test, symbol: '' };
  return { test, file: test.slice(0, hash), symbol: test.slice(hash + 1) };
}

/** The ref's spec (`ACME-SP-0001.R2` → `ACME-SP-0001`). */
function specOf(ref: string): string {
  const dot = ref.lastIndexOf('.');
  return dot < 0 ? ref : ref.slice(0, dot);
}

/** The ref's number, so `R10` sorts after `R2`. */
function refNumber(ref: string): number {
  const match = /\.R(\d+)$/.exec(ref);
  return match?.[1] ? Number(match[1]) : Number.MAX_SAFE_INTEGER;
}

function compareRefs(a: { spec: string; ref: string }, b: { spec: string; ref: string }): number {
  return a.spec.localeCompare(b.spec) || refNumber(a.ref) - refNumber(b.ref);
}

function emptyCounts(): StatusCounts {
  return { untested: 0, passing: 0, failing: 0, suspect: 0 };
}

/**
 * Every requirement joined with its coverage row, in spec then ref order. A
 * requirement the coverage answer does not name has nothing linked: it is
 * `untested` with no reasons. A coverage row whose requirement is not in the
 * list (a stale index) is kept, titled by its ref.
 */
export function joinRows(
  requirements: readonly Requirement[],
  coverage: readonly CoverageRow[],
): MatrixRow[] {
  const byRef = new Map(coverage.map((row) => [row.ref, row]));
  const rows: MatrixRow[] = [];
  const seen = new Set<string>();
  const add = (ref: string, spec: string, title: string, anchor: string) => {
    if (seen.has(ref)) return;
    seen.add(ref);
    const row = byRef.get(ref);
    rows.push({
      ref,
      spec,
      title,
      anchor,
      status: row?.status ?? 'untested',
      reasons: row?.reasons ?? [],
      results: new Map((row?.tests ?? []).map((t) => [t.test, t.result])),
    });
  };
  for (const requirement of requirements) {
    add(requirement.ref, requirement.spec, requirement.title, requirement.anchor);
  }
  for (const row of coverage) add(row.ref, specOf(row.ref), row.ref, '');
  return rows.sort(compareRefs);
}

/** The rows the URL filter keeps. */
export function filterRows(rows: readonly MatrixRow[], search: MatrixSearch): MatrixRow[] {
  return rows.filter(
    (row) =>
      (!search.spec?.length || search.spec.includes(row.spec)) &&
      (!search.status?.length || search.status.includes(row.status)),
  );
}

/**
 * The columns of `rows`: every test any of them links, grouped by file, files
 * and symbols in lexical order. Only the rows shown contribute, so filtering
 * to one spec narrows the columns to its tests.
 */
export function buildColumns(rows: readonly MatrixRow[]): MatrixColumnGroup[] {
  const byFile = new Map<string, Map<string, MatrixColumn>>();
  for (const row of rows) {
    for (const test of row.results.keys()) {
      const column = splitTest(test);
      const columns = byFile.get(column.file) ?? new Map<string, MatrixColumn>();
      columns.set(test, column);
      byFile.set(column.file, columns);
    }
  }
  return [...byFile.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([file, columns]) => ({
      file,
      columns: [...columns.values()].sort((a, b) => a.symbol.localeCompare(b.symbol)),
    }));
}

/** Filtered rows and their columns. */
export function buildMatrix(allRows: readonly MatrixRow[], search: MatrixSearch): Matrix {
  const rows = filterRows(allRows, search);
  const groups = buildColumns(rows);
  return { rows, groups, columns: groups.flatMap((group) => group.columns) };
}

/** Counts per coverage state. */
export function countStatuses(rows: readonly MatrixRow[]): StatusCounts {
  const counts = emptyCounts();
  for (const row of rows) counts[row.status] += 1;
  return counts;
}

/** Per-spec counts over `rows`, in spec order; a spec without requirements is listed with 0. */
export function summarizeSpecs(specs: readonly Item[], rows: readonly MatrixRow[]): SpecSummary[] {
  const summaries = new Map<string, SpecSummary>(
    specs.map((spec) => [
      spec.id,
      { id: spec.id, title: spec.title, total: 0, counts: emptyCounts() },
    ]),
  );
  for (const row of rows) {
    const summary = summaries.get(row.spec) ?? {
      id: row.spec,
      title: row.spec,
      total: 0,
      counts: emptyCounts(),
    };
    summary.total += 1;
    summary.counts[row.status] += 1;
    summaries.set(row.spec, summary);
  }
  return [...summaries.values()].sort((a, b) => a.id.localeCompare(b.id));
}

/** The rows a scroll position shows, plus `overscan` either side. */
export function visibleRange(
  count: number,
  scrollTop: number,
  viewport: number,
  rowHeight: number,
  overscan: number,
): { start: number; end: number } {
  if (count === 0) return { start: 0, end: 0 };
  const first = Math.floor(Math.max(0, scrollTop) / rowHeight);
  const start = Math.max(0, Math.min(first, count - 1) - overscan);
  const end = Math.min(count, first + Math.ceil(viewport / rowHeight) + overscan);
  return { start, end: Math.max(end, start + 1) };
}
