/**
 * Pure model of the requirement impact view (story GIT-US-0131, doc 03
 * §21.11): the URL state of the base/head pickers, the tier grouping of the
 * hits, the per-tier status line, the reason codes read aloud and the ref
 * suggestions the pickers offer.
 *
 * Tier 3 candidates are a group of their own and never sit among the certain
 * hits of tiers 1 and 2 (R-IMP-4): a candidate is a guess with a score, not a
 * reached requirement.
 */

import type { SearchSchemaInput } from '@tanstack/react-router';
import { z } from 'zod';

import type { GitRefs, ImpactResult, SyncCommit, SyncRepoStatus } from '@/api/provider';

/** One requirement a diff affects (doc 03 §21.11 R-IMP-5). */
export type ImpactHit = ImpactResult['hits'][number];

/** The head value that names the working tree; the query sends no `head` for it. */
export const WORKTREE = 'worktree';

/** The base the view diffs against when the URL names none: the usual default branch. */
export const DEFAULT_BASE = 'main';

export type ImpactSearch = { base?: string; head?: string };

const refParam = z
  .preprocess(
    (value) => (typeof value === 'string' ? value.trim() : undefined),
    z.string().max(200).optional(),
  )
  .catch(undefined);

const impactSearchSchema = z.object({ base: refParam, head: refParam });

/** Parses raw search params. Never throws; an empty value is dropped. */
export function parseImpactSearch(input: unknown): ImpactSearch {
  const result = impactSearchSchema.safeParse(input ?? {});
  if (!result.success) return {};
  const { base, head } = result.data;
  return {
    ...(base ? { base } : {}),
    ...(head ? { head } : {}),
  };
}

/** Route `validateSearch` for `/p/$project/specs/impact`. */
export function validateImpactSearch(input: ImpactSearch & SearchSchemaInput): ImpactSearch {
  return parseImpactSearch(input);
}

/** True when a head value means the working tree: empty, or the `worktree` keyword. */
export function isWorktree(head: string | undefined): boolean {
  return head === undefined || head === '' || head.toLowerCase() === WORKTREE;
}

/** The range the view asks for: `base` defaulted, `head` omitted for the working tree. */
export function impactRange(search: ImpactSearch): { base: string; head?: string } {
  const base = search.base ?? DEFAULT_BASE;
  return isWorktree(search.head) ? { base } : { base, head: search.head as string };
}

/** `main..worktree`, the way the report's text form names a range. */
export function formatRange(base: string, head: string | undefined): string {
  return `${base}..${isWorktree(head) ? WORKTREE : head}`;
}

export type TierKey = 1 | 2 | 3;

export type TierStatus = ImpactResult['tiers'][number];

export type TierGroup = {
  tier: TierKey;
  /** The tier's own status line; absent when the answer did not report the tier. */
  status?: TierStatus;
  hits: ImpactHit[];
};

export const tierLabels: Record<TierKey, { title: string; hint: string }> = {
  1: { title: 'Direct', hint: 'The diff touches a traced file, symbol or marker.' },
  2: { title: 'Transitive', hint: 'A changed symbol is called from traced code.' },
  3: {
    title: 'Candidates',
    hint: 'Semantically close to the change; not reached by a trace or a call.',
  },
};

/** The tier a hit is shown under: a candidate is always tier 3, whatever else it says. */
export function tierOf(hit: ImpactHit): TierKey {
  return hit.candidate ? 3 : hit.tier;
}

/**
 * The hits grouped by tier, always three groups in tier order. Tiers 1 and 2
 * keep the answer's order (spec, then number, R-IMP-6); candidates are ranked
 * by score, highest first, then by ref (R-IMP-8).
 */
export function groupByTier(result: Pick<ImpactResult, 'tiers' | 'hits'>): TierGroup[] {
  return ([1, 2, 3] as const).map((tier) => {
    const hits = result.hits.filter((hit) => tierOf(hit) === tier);
    if (tier === 3) {
      hits.sort((a, b) => (b.score ?? 0) - (a.score ?? 0) || a.ref.localeCompare(b.ref));
    }
    const status = result.tiers.find((t) => t.tier === tier);
    return { tier, hits, ...(status ? { status } : {}) };
  });
}

/** The sentence of a tier's status; `undefined` for a plain `ok`. */
export function tierStatusText(status: TierStatus | undefined): string | undefined {
  if (!status) return 'Not reported.';
  switch (status.status) {
    case 'ok':
      return status.truncated ? 'Partial: the per-symbol limit cut some callers.' : undefined;
    case 'skipped':
      return 'Skipped: this tier was not asked for.';
    case 'unavailable':
      return `Unavailable${status.message ? `: ${status.message}` : '.'} Tiers 2 and 3 need Pando.`;
    case 'error':
      return `Error${status.message ? `: ${status.message}` : '.'}`;
  }
}

export type ReadReason = { kind: string; text: string; title: string };

const reasonKinds: Record<string, string> = {
  file: 'file',
  symbol: 'symbol',
  marker: 'marker',
  renamed: 'renamed',
  removed: 'removed',
  implements: 'implements',
  modifies: 'modifies',
  delta: 'pending delta',
  call: 'called from',
  semantic: 'semantic',
};

const callPattern = /^call:(\S+) calls (\S+) d(\d+)$/;

/**
 * A reason code read aloud (doc 03 §21.11 R-IMP-2..5). `call:<caller> calls
 * <symbol> d<n>` becomes "called from <caller> → <symbol>, depth n"; `+<n>`
 * counts the reasons the answer left out.
 */
export function readReason(reason: string): ReadReason {
  const call = callPattern.exec(reason);
  if (call) {
    const [, caller = '', symbol = '', depth = ''] = call;
    return { kind: 'called from', text: `${caller} → ${symbol}, depth ${depth}`, title: reason };
  }
  if (/^\+\d+$/.test(reason)) {
    return { kind: 'more', text: `${reason.slice(1)} more`, title: reason };
  }
  const colon = reason.indexOf(':');
  if (colon < 0) return { kind: reasonKinds[reason] ?? reason, text: '', title: reason };
  const key = reason.slice(0, colon);
  return { kind: reasonKinds[key] ?? key, text: reason.slice(colon + 1), title: reason };
}

/** One entry of a ref picker: the ref it fills in, and what it is. */
export type RefOption = { value: string; label?: string };

/** The ref suggestions of the base picker, then the head picker. */
export type RefSuggestions = { base: RefOption[]; head: RefOption[] };

/** How many characters of a commit id a picker fills in; every backend resolves the prefix. */
export const SHORT_SHA = 12;

/** A commit read aloud in the picker: `subject · 2026-09-02`. */
function commitLabel(commit: SyncCommit): string {
  const subject = commit.subject.trim() || '(no description)';
  const day = commit.date ? commit.date.slice(0, 10) : '';
  return day ? `${subject} · ${day}` : subject;
}

/**
 * What the pickers offer (GIT-US-0149). With the ref listing of the companion
 * (`GET /api/v1/git/refs`): the default branch, every local and remote
 * branch — a jj repository's bookmarks — and the recent commits by short id,
 * labelled with their subject and date; for the head, the working tree first.
 * Without it (`unavailable`, or not loaded yet), the current branch and its
 * upstream from the sync status and `HEAD` with a few ancestors. The inputs
 * stay free text either way: a tag or any other ref can still be typed.
 */
export function refSuggestions(
  rows: readonly SyncRepoStatus[] | undefined,
  refs?: GitRefs,
): RefSuggestions {
  const options: RefOption[] = [];
  for (const row of rows ?? []) {
    const status = row.status;
    if (!status) continue;
    if (!status.detached && status.branch && status.branch !== '@') {
      options.push({ value: status.branch, label: 'current branch' });
    }
    if (status.upstream) options.push({ value: status.upstream, label: 'upstream' });
  }
  for (const branch of refs?.branches ?? []) {
    options.push({
      value: branch.name,
      label: branch.current ? 'current branch' : branch.remote ? 'remote branch' : 'branch',
    });
  }
  const commits = refs?.commits ?? [];
  if (commits.length > 0) {
    options.push({ value: 'HEAD' });
    for (const commit of commits) {
      options.push({ value: commit.sha.slice(0, SHORT_SHA), label: commitLabel(commit) });
    }
  } else {
    for (const value of ['HEAD', 'HEAD~1', 'HEAD~2', 'HEAD~5']) options.push({ value });
  }
  const unique = (values: RefOption[]): RefOption[] => {
    const seen = new Map<string, RefOption>();
    for (const option of values) {
      if (option.value && !seen.has(option.value)) seen.set(option.value, option);
    }
    return [...seen.values()];
  };
  return {
    base: unique([{ value: DEFAULT_BASE }, ...options]),
    head: unique([{ value: WORKTREE, label: 'working tree' }, ...options]),
  };
}
