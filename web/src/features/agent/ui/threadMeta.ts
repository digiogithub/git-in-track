/**
 * Thread titles, in `localStorage` (task GIT-T-0062).
 *
 * `web/src/features/agent/threads.ts` already persists the *identity* of a
 * conversation — the ids this repository has seen and which one is active.
 * What it deliberately does not persist is anything a human reads, because the
 * adapter owns the transcript and a second copy of it would go stale the
 * moment the server compacted a context.
 *
 * A title is the exception: it is derived from the first user message, it is
 * pure presentation, and the thread list has to render before a single
 * transcript has been fetched. So it lives here, next to the UI that shows it,
 * as derived data under one key — `gintrack:agent:threads`, the key the story
 * names — shaped `{ [repo]: AgentThreadMeta[] }` so it never collides with the
 * per-repository id lists.
 *
 * Nothing here is authoritative. Losing the whole key costs a list of labels;
 * the conversations themselves are still on the server, keyed by id.
 */

import type { AgentMessage } from '@/features/agent/types';

/** One row of the local thread list. */
export type AgentThreadMeta = {
  id: string;
  /** Derived from the first user message; never empty. */
  title: string;
  /** RFC 3339, so a reload can order the list without a second source. */
  updatedAt: string;
};

/** Where the local thread list lives (story GIT-US-0057). */
export const THREAD_META_KEY = 'gintrack:agent:threads';

/** What an unnamed conversation is called until someone says something. */
export const UNTITLED_THREAD = 'New conversation';

/** How long a derived title may be before it is cut on a word boundary. */
const TITLE_LIMIT = 60;

type MetaFile = Record<string, AgentThreadMeta[]>;

/**
 * Every access is guarded: `localStorage` is a property that *throws* in a
 * sandboxed frame or a locked-down private mode, so reading it is not safe
 * even inside a `typeof` check.
 */
function readFile(): MetaFile {
  try {
    const raw = globalThis.localStorage?.getItem(THREAD_META_KEY) ?? null;
    if (raw === null) return {};
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {};
    const out: MetaFile = {};
    for (const [repo, rows] of Object.entries(parsed as Record<string, unknown>)) {
      if (Array.isArray(rows)) out[repo] = rows.filter(isMeta);
    }
    return out;
  } catch {
    return {};
  }
}

function writeFile(file: MetaFile): void {
  try {
    globalThis.localStorage?.setItem(THREAD_META_KEY, JSON.stringify(file));
  } catch {
    // Persistence is a convenience: without it every thread is "New
    // conversation" until its transcript loads, which is a degraded label and
    // not a broken page.
  }
}

function isMeta(value: unknown): value is AgentThreadMeta {
  if (typeof value !== 'object' || value === null) return false;
  const row = value as Partial<AgentThreadMeta>;
  return typeof row.id === 'string' && typeof row.title === 'string';
}

/** The rows known for a repository, newest first. */
export function readThreadMeta(repo: string): AgentThreadMeta[] {
  return readFile()[repo] ?? [];
}

/**
 * Records a row, moving it to the front. Called on every turn, so it is
 * deliberately idempotent and cheap: same id, same title, new timestamp.
 */
export function rememberThreadMeta(repo: string, meta: AgentThreadMeta): AgentThreadMeta[] {
  const file = readFile();
  const rows = [meta, ...(file[repo] ?? []).filter((row) => row.id !== meta.id)];
  file[repo] = rows;
  writeFile(file);
  return rows;
}

/** Drops a row, e.g. after the adapter forgot the thread. */
export function forgetThreadMeta(repo: string, threadId: string): AgentThreadMeta[] {
  const file = readFile();
  const rows = (file[repo] ?? []).filter((row) => row.id !== threadId);
  file[repo] = rows;
  writeFile(file);
  return rows;
}

/**
 * The label a conversation gets: its first user message, flattened to one line
 * and cut on a word boundary.
 *
 * The first *user* message rather than the first message of any kind, because
 * a transcript can open with a system prompt or an activity line, and neither
 * says anything about what the person came to do.
 */
export function deriveThreadTitle(messages: readonly AgentMessage[]): string {
  const first = messages.find((message) => message.role === 'user' && message.text.trim() !== '');
  if (first === undefined) return UNTITLED_THREAD;
  const line = first.text.replace(/\s+/g, ' ').trim();
  if (line.length <= TITLE_LIMIT) return line;
  const cut = line.slice(0, TITLE_LIMIT);
  const space = cut.lastIndexOf(' ');
  return `${(space > 24 ? cut.slice(0, space) : cut).trimEnd()}…`;
}
