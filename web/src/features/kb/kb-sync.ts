/**
 * What the five knowledge-base synchronization states mean on screen
 * (story GIT-US-0093, ADR-031).
 *
 * The pure half of the feature lives here — labels, severity, the summary a
 * folder shows for its children and the name of the file a conflict writes —
 * so that the components stay about layout and every one of these rules can be
 * tested without rendering anything.
 *
 * Two rules are worth stating out loud:
 *
 *  1. **A state is always a word, never only a colour.** Each label below is
 *     rendered next to its tint, because a reader who cannot separate the two
 *     warning tones still has to be able to tell "the article is newer" from
 *     "this page has local edits".
 *  2. **Publishing never sends the `## Feedback` block.** It is stripped
 *     before a page leaves the repository (ADR-030), which is why the copy in
 *     `PUBLISH_SCOPE_NOTE` says so rather than leaving a reader to assume that
 *     the notes they wrote are about to appear in an article.
 */

import type { KbNode, KbPageSyncStatus, KbSyncState } from '@/api/provider';

/** The label shown beside every state tint. */
export const KB_SYNC_LABELS: Record<KbSyncState, string> = {
  unlinked: 'Not published',
  in_sync: 'In sync',
  local_ahead: 'Local changes',
  remote_ahead: 'Article is newer',
  conflict: 'Conflict',
};

/** One sentence explaining what the badge is claiming, for a title attribute. */
export const KB_SYNC_HINTS: Record<KbSyncState, string> = {
  unlinked: 'This page mirrors no YouTrack article yet.',
  in_sync: 'This page and its article hold the same content.',
  local_ahead: 'This page changed since it was last synchronized; the article has not.',
  remote_ahead: 'The article changed since it was last synchronized; this page has not.',
  conflict:
    'This page and its article both changed since the last synchronization, so neither side wins.',
};

/**
 * The badge tone per state.
 *
 * `conflict` is destructive because it is the one state that needs a person;
 * the two "ahead" states are warnings because they are drift that will become a
 * problem; `in_sync` is the only success, and an unpublished page is neutral
 * rather than bad — most pages are never published at all.
 */
export const KB_SYNC_TONES: Record<KbSyncState, 'outline' | 'success' | 'warning' | 'destructive'> =
  {
    unlinked: 'outline',
    in_sync: 'success',
    local_ahead: 'warning',
    remote_ahead: 'warning',
    conflict: 'destructive',
  };

/**
 * How loudly a state asks for attention. A folder badge shows the worst state
 * among its pages, because the reason to summarise a folder at all is to find
 * the page that needs looking at.
 */
const SEVERITY: Record<KbSyncState, number> = {
  unlinked: 0,
  in_sync: 1,
  local_ahead: 2,
  remote_ahead: 3,
  conflict: 4,
};

/** What a folder's badge says: the worst state under it and how many pages hold it. */
export type KbSyncSummary = { state: KbSyncState; count: number; pages: number };

/**
 * Summarises a set of page states.
 *
 * `count` is how many pages sit in the reported state, so that "Conflict" on a
 * folder can say whether it is one page or nine. An empty set summarises to
 * nothing, which is what keeps a badge off a folder with no pages in it.
 */
export function summariseKbSync(states: KbSyncState[]): KbSyncSummary | null {
  if (states.length === 0) return null;
  let worst: KbSyncState = 'unlinked';
  for (const state of states) {
    if (SEVERITY[state] > SEVERITY[worst]) worst = state;
  }
  return {
    state: worst,
    count: states.filter((state) => state === worst).length,
    pages: states.length,
  };
}

/** Every page path at or below `path`, from a set of status rows. */
export function pagesUnder(rows: KbPageSyncStatus[], path: string): KbPageSyncStatus[] {
  const prefix = path.replace(/\/+$/, '');
  if (prefix === '') return rows;
  return rows.filter((row) => row.path === prefix || row.path.startsWith(`${prefix}/`));
}

/** Every page path at or below `path` in the tree, for a folder that has no status yet. */
export function treePagesUnder(nodes: KbNode[], path: string): string[] {
  const out: string[] = [];
  const walk = (list: KbNode[]) => {
    for (const node of list) {
      if (node.kind === 'page') out.push(node.path);
      walk(node.children ?? []);
    }
  };
  const find = (list: KbNode[]): KbNode | null => {
    for (const node of list) {
      if (node.path === path) return node;
      const hit = find(node.children ?? []);
      if (hit) return hit;
    }
    return null;
  };
  if (path === '') {
    walk(nodes);
    return out;
  }
  const node = find(nodes);
  if (!node) return out;
  if (node.kind === 'page') return [node.path];
  walk(node.children ?? []);
  return out;
}

/** The folder a page lives in; `''` for a page at the root of the vault. */
export function folderOf(path: string): string {
  const cut = path.lastIndexOf('/');
  return cut === -1 ? '' : path.slice(0, cut);
}

/**
 * Where the incoming content of a conflict was written.
 *
 * It mirrors the companion exactly (`strings.TrimSuffix(path, ".md") +
 * ".conflict.md"`), because the notice links to that file by name and a link
 * that guessed differently would 404 on the one screen that must not.
 */
export function conflictPathOf(path: string): string {
  return `${path.replace(/\.md$/, '')}.conflict.md`;
}

/**
 * What a publish sends, said plainly.
 *
 * The `## Feedback` block is stripped on the way out and never reaches an
 * article (ADR-030), so a reader who has just written notes on a page is told
 * that publishing will not carry them anywhere.
 */
export const PUBLISH_SCOPE_NOTE =
  'The page content is published. The ## Feedback block stays in the repository and is never sent to YouTrack.';
