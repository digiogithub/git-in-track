/**
 * Editor drafts (docs/05-web-app.md §8.3).
 *
 * An unsaved edit is kept in `localStorage` so that a reload, a crashed tab or
 * a closed browser does not lose it. It is offered back on the next visit —
 * restore or discard — and dropped as soon as the item is saved.
 *
 * A draft is **derived, per-viewer state, never a source of truth**: nothing
 * reads it but the editor that wrote it, it never reaches a repository, and the
 * files on disk stay the only record of what the project is (AGENTS.md, "Do not
 * store state outside Markdown and YAML"). Losing every draft costs a user
 * their unsaved typing and nothing else.
 *
 * Scope. The key carries the workspace (the companion this tab talks to, or the
 * browser profile in browser-only mode), the project key and the item id, so
 * two workspaces open on one machine — and two items of one project — never
 * read each other's drafts.
 */

import { workspaceScope } from '@/app/store';
import type { FrontMatterValues } from '@/features/editor/front-matter';

/** Prefix every editor draft is stored under. */
const DRAFT_PREFIX = 'gintrack:draft:';

/** How long a draft is offered back before it is treated as stale. */
const MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;

export type EditorDraft = {
  /** The revision the draft was started from; a newer file makes it suspect. */
  rev: string;
  /** The front-matter form as it stood. */
  values: FrontMatterValues;
  body: string;
  /** ISO timestamp of the last keystroke that reached storage. */
  savedAt: string;
};

/** `gintrack:draft:<workspace>:<project>:<itemId>`. */
export function draftKey(companionUrl: string | null, project: string, id: string): string {
  return `${DRAFT_PREFIX}${workspaceScope(companionUrl)}:${project}:${id}`;
}

function isDraft(value: unknown): value is EditorDraft {
  if (typeof value !== 'object' || value === null) return false;
  const draft = value as Partial<EditorDraft>;
  return (
    typeof draft.rev === 'string' &&
    typeof draft.body === 'string' &&
    typeof draft.savedAt === 'string' &&
    typeof draft.values === 'object' &&
    draft.values !== null
  );
}

/**
 * Reads the draft for one item, or `null` when there is none, when it cannot be
 * parsed, or when it is older than {@link MAX_AGE_MS}. Storage that throws —
 * a private window, a sandboxed frame — reads as "no draft" rather than as an
 * error: the editor works without drafts, it just forgets faster.
 */
export function readDraft(key: string): EditorDraft | null {
  let raw: string | null = null;
  try {
    raw = globalThis.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
  if (raw === null) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    clearDraft(key);
    return null;
  }
  if (!isDraft(parsed)) {
    clearDraft(key);
    return null;
  }
  const age = Date.now() - Date.parse(parsed.savedAt);
  if (Number.isFinite(age) && age > MAX_AGE_MS) {
    clearDraft(key);
    return null;
  }
  return parsed;
}

/** Writes (or overwrites) the draft for one item. */
export function writeDraft(key: string, draft: Omit<EditorDraft, 'savedAt'>): void {
  try {
    globalThis.localStorage?.setItem(
      key,
      JSON.stringify({ ...draft, savedAt: new Date().toISOString() }),
    );
  } catch {
    // A full or disabled storage costs the reload guarantee, nothing else: the
    // buffer in memory is still what the editor saves.
  }
}

/** Drops the draft for one item. Called on every successful save. */
export function clearDraft(key: string): void {
  try {
    globalThis.localStorage?.removeItem(key);
  } catch {
    // Nothing to do; a draft that cannot be removed is offered once more and
    // then discarded by the user.
  }
}
