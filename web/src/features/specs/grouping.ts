/**
 * The pure half of the specs page: requirements grouped under their spec, each
 * one a row with its computed coverage, and the URL filters applied to them.
 * Kept free of React so the grouping rules are testable on their own.
 */

import type { CoverageRow, Item, Requirement } from '@/api/provider';
import type { CoverageState, SpecSearch } from '@/features/specs/search';

/** What the page knows about coverage: the rows, or why there are none. */
export type CoverageView =
  | { kind: 'loading' }
  | { kind: 'ready'; byRef: ReadonlyMap<string, CoverageRow> }
  | { kind: 'unavailable'; reason: string };

/** One requirement row. `coverage` is absent while the rows load or cannot load. */
export type RequirementRowModel = {
  requirement: Requirement;
  coverage: CoverageState | 'unavailable' | undefined;
};

export type SpecGroup = {
  /** Undefined for requirements whose spec is not in the spec list (a stale index). */
  spec: Item | undefined;
  id: string;
  rows: RequirementRowModel[];
  /** Requirements of the spec before filtering, for the "n of m" count. */
  total: number;
};

/** The ref's number, so `R10` sorts after `R2`. */
function refNumber(ref: string): number {
  const match = /\.R(\d+)$/.exec(ref);
  return match?.[1] ? Number(match[1]) : Number.MAX_SAFE_INTEGER;
}

function coverageOf(
  requirement: Requirement,
  coverage: CoverageView,
): RequirementRowModel['coverage'] {
  if (coverage.kind === 'unavailable') return 'unavailable';
  if (coverage.kind === 'loading') return undefined;
  // A requirement the coverage answer does not name has nothing linked yet.
  return coverage.byRef.get(requirement.ref)?.status ?? 'untested';
}

function matches(row: RequirementRowModel, search: SpecSearch, coverage: CoverageView): boolean {
  if (search.status?.length && !search.status.includes(row.requirement.status)) return false;
  // A coverage filter needs coverage: while it is unavailable it is not applied,
  // and the page says so instead of emptying the list.
  if (search.coverage?.length && coverage.kind === 'ready') {
    if (row.coverage === undefined || row.coverage === 'unavailable') return false;
    if (!search.coverage.includes(row.coverage)) return false;
  }
  return true;
}

function hasFilter(search: SpecSearch): boolean {
  return Boolean(search.status?.length) || Boolean(search.coverage?.length);
}

/**
 * Every spec as a group, in spec-id order, and every requirement as its own
 * row, in ref order. With a filter applied, a spec none of whose requirements
 * match is left out; without one, an empty spec is kept so it can be filled.
 */
export function groupRequirements(
  specs: readonly Item[],
  requirements: readonly Requirement[],
  coverage: CoverageView,
  search: SpecSearch,
): SpecGroup[] {
  const bySpec = new Map<string, Requirement[]>();
  for (const requirement of requirements) {
    bySpec.set(requirement.spec, [...(bySpec.get(requirement.spec) ?? []), requirement]);
  }

  const ids = new Set<string>([...specs.map((spec) => spec.id), ...bySpec.keys()]);
  const specById = new Map(specs.map((spec) => [spec.id, spec]));
  const filtered = hasFilter(search);

  const groups: SpecGroup[] = [];
  for (const id of [...ids].sort((a, b) => a.localeCompare(b))) {
    const all = [...(bySpec.get(id) ?? [])].sort((a, b) => refNumber(a.ref) - refNumber(b.ref));
    const rows = all
      .map((requirement) => ({ requirement, coverage: coverageOf(requirement, coverage) }))
      .filter((row) => matches(row, search, coverage));
    if (filtered && rows.length === 0) continue;
    groups.push({ spec: specById.get(id), id, rows, total: all.length });
  }
  return groups;
}

/** Counts per coverage state over the rows shown, for the filter chips. */
export function countCoverage(
  requirements: readonly Requirement[],
  coverage: CoverageView,
): Record<CoverageState, number> {
  const counts: Record<CoverageState, number> = { untested: 0, passing: 0, failing: 0, suspect: 0 };
  if (coverage.kind !== 'ready') return counts;
  for (const requirement of requirements) {
    const state = coverage.byRef.get(requirement.ref)?.status ?? 'untested';
    counts[state] += 1;
  }
  return counts;
}
