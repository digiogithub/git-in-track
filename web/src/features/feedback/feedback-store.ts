import { useCallback, useMemo, useSyncExternalStore } from 'react';

/**
 * Feedback drafts (docs/05-web-app.md §8.5).
 *
 * A reader in feedback mode selects text in an item body or a knowledge-base
 * page and attaches a note to it. Notes pile up in this browser until the
 * reader saves them — as one comment on an item, or as the feedback block at
 * the end of a page — so a draft must survive a reload, a crash and a detour to
 * another screen. Every target keeps its own draft under its own key, so the
 * unsaved feedback of several items and pages lives side by side.
 *
 * `localStorage` is the only store: a draft is personal and unfinished, and it
 * never belongs in the repository until it is saved.
 */

export type FeedbackTarget = {
  kind: 'item' | 'kb';
  /** Project key, or team key for a team knowledge base. */
  project: string;
  /** Item id or page path. */
  ref: string;
};

export type FeedbackNote = {
  id: string;
  /** The selected text, whitespace-collapsed. */
  quote: string;
  /** 1-based source lines of the body the quote came from, when resolved. */
  startLine?: number;
  endLine?: number;
  note: string;
  created: string;
};

export type FeedbackDraft = {
  /** Whether feedback mode is on for this target. */
  active: boolean;
  notes: FeedbackNote[];
};

export const FEEDBACK_STORAGE_PREFIX = 'gintrack:feedback:';

const EMPTY: FeedbackDraft = Object.freeze({ active: false, notes: [] });

export function feedbackKey(target: FeedbackTarget): string {
  return `${FEEDBACK_STORAGE_PREFIX}${target.kind}:${target.project}:${target.ref}`;
}

type Listener = () => void;
const listeners = new Set<Listener>();
/** Parsed drafts by storage key, so a snapshot is stable between changes. */
const cache = new Map<string, FeedbackDraft>();

function isNote(value: unknown): value is FeedbackNote {
  if (typeof value !== 'object' || value === null) return false;
  const note = value as Record<string, unknown>;
  return (
    typeof note['id'] === 'string' &&
    typeof note['quote'] === 'string' &&
    typeof note['note'] === 'string'
  );
}

function parse(raw: string | null): FeedbackDraft {
  if (!raw) return EMPTY;
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return EMPTY;
    const record = value as Record<string, unknown>;
    const notes = Array.isArray(record['notes']) ? record['notes'].filter(isNote) : [];
    return { active: record['active'] === true, notes };
  } catch {
    return EMPTY;
  }
}

function readStorage(key: string): string | null {
  try {
    return globalThis.localStorage?.getItem(key) ?? null;
  } catch {
    // Private modes and sandboxed iframes throw on access.
    return null;
  }
}

function writeStorage(key: string, draft: FeedbackDraft): void {
  try {
    if (!draft.active && draft.notes.length === 0) globalThis.localStorage?.removeItem(key);
    else globalThis.localStorage?.setItem(key, JSON.stringify(draft));
  } catch {
    // The in-memory copy still serves this tab.
  }
}

export function readFeedbackDraft(key: string): FeedbackDraft {
  let draft = cache.get(key);
  if (!draft) {
    draft = parse(readStorage(key));
    cache.set(key, draft);
  }
  return draft;
}

function writeFeedbackDraft(key: string, draft: FeedbackDraft): void {
  cache.set(key, draft);
  writeStorage(key, draft);
  for (const listener of [...listeners]) listener();
}

function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  // Another tab of the same app editing the same draft.
  const onStorage = (event: StorageEvent) => {
    if (event.key !== null && !event.key.startsWith(FEEDBACK_STORAGE_PREFIX)) return;
    if (event.key === null) cache.clear();
    else cache.delete(event.key);
    listener();
  };
  globalThis.addEventListener?.('storage', onStorage);
  return () => {
    listeners.delete(listener);
    globalThis.removeEventListener?.('storage', onStorage);
  };
}

let counter = 0;

function newNoteId(): string {
  counter += 1;
  return `n${Date.now().toString(36)}${counter.toString(36)}`;
}

export type FeedbackNoteInput = Omit<FeedbackNote, 'id' | 'created'>;

export type FeedbackDraftApi = {
  draft: FeedbackDraft;
  setActive: (active: boolean) => void;
  addNote: (note: FeedbackNoteInput) => void;
  updateNote: (id: string, note: string) => void;
  removeNote: (id: string) => void;
  /** Forgets the draft entirely: the notes and the mode. */
  clear: () => void;
};

/** The feedback draft of one item or page, re-rendering on every change. */
export function useFeedbackDraft(target: FeedbackTarget): FeedbackDraftApi {
  const key = feedbackKey(target);
  const draft = useSyncExternalStore(
    subscribe,
    () => readFeedbackDraft(key),
    () => EMPTY,
  );

  const update = useCallback(
    (change: (current: FeedbackDraft) => FeedbackDraft) => {
      writeFeedbackDraft(key, change(readFeedbackDraft(key)));
    },
    [key],
  );

  const setActive = useCallback(
    (active: boolean) => update((current) => ({ ...current, active })),
    [update],
  );
  const addNote = useCallback(
    (input: FeedbackNoteInput) =>
      update((current) => ({
        active: true,
        notes: [...current.notes, { ...input, id: newNoteId(), created: new Date().toISOString() }],
      })),
    [update],
  );
  const updateNote = useCallback(
    (id: string, note: string) =>
      update((current) => ({
        ...current,
        notes: current.notes.map((entry) => (entry.id === id ? { ...entry, note } : entry)),
      })),
    [update],
  );
  const removeNote = useCallback(
    (id: string) =>
      update((current) => ({
        ...current,
        notes: current.notes.filter((entry) => entry.id !== id),
      })),
    [update],
  );
  const clear = useCallback(() => update(() => EMPTY), [update]);

  return useMemo(
    () => ({ draft, setActive, addNote, updateNote, removeNote, clear }),
    [draft, setActive, addNote, updateNote, removeNote, clear],
  );
}

/** Test seam: forgets the parsed copies so the next read hits storage again. */
export function resetFeedbackCache(): void {
  cache.clear();
}
