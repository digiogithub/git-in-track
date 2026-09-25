import type { SearchHit } from '@/api/provider';

/** Nothing is queried below this length: one letter matches everything. */
export const MIN_QUERY = 2;

/** Terms shorter than this are not highlighted: they match almost any word. */
const MIN_TERM = 2;

/**
 * The query, split into the terms worth highlighting inside a snippet. The
 * terms are lower-cased and de-duplicated; matching happens on the snippet
 * itself, so a term the passage does not contain simply highlights nothing.
 */
export function queryTerms(query: string): string[] {
  const seen = new Set<string>();
  for (const raw of query.split(/\s+/)) {
    const term = raw.trim().toLowerCase();
    if (term.length >= MIN_TERM) seen.add(term);
  }
  return [...seen];
}

/**
 * A stable key for a hit: two repositories can hold the same path, and the
 * requirements of one spec share its path, so a requirement is keyed by its
 * ref (GIT-US-0118).
 */
export function hitKey(hit: SearchHit): string {
  if (hit.kind === 'requirement' && hit.id) return `${hit.vaultId ?? ''}:${hit.id}`;
  return `${hit.vaultId ?? ''}:${hit.path ?? hit.id ?? hit.title}`;
}
