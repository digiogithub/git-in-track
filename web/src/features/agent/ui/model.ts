/**
 * The two derivations the agent UI does, kept out of the component files.
 *
 * Both are pure and both are worth a test of their own: what a transcript
 * shows, and what the conversation list shows. They live here rather than next
 * to their components so a component module exports components only.
 */

import type { AgentThreadSummary } from '@/api/provider';
import type { AgentMessage } from '@/features/agent/types';
import type { AgentThreadMeta } from '@/features/agent/ui/threadMeta';
import { UNTITLED_THREAD } from '@/features/agent/ui/threadMeta';

/**
 * Whether a message has anything a person should see.
 *
 * `tool` messages are the results, and they are already rendered inside the
 * card of the call they answer; showing them twice would double the noisiest
 * part of the transcript. `system`, `developer` and `reasoning` are channels,
 * not replies — reasoning reaches the screen attached to its own message.
 */
export function isVisible(message: AgentMessage): boolean {
  if (message.role === 'tool' || message.role === 'system' || message.role === 'developer') {
    return false;
  }
  if (message.role === 'reasoning') return false;
  if (message.role === 'user') return message.text !== '';
  return (
    message.text !== '' ||
    message.toolCalls.length > 0 ||
    message.reasoning !== undefined ||
    message.error !== undefined
  );
}

/** One row of the conversation list. */
export type ThreadRow = {
  id: string;
  title: string;
  updatedAt?: string;
  running?: boolean;
};

/**
 * One row per thread this repository knows about, newest first.
 *
 * The rows are a merge of two sources and neither is authoritative alone: the
 * adapter knows which threads exist and which still have a run attached, and
 * `localStorage` knows what each one is *called*, because a title is derived
 * from the first user message and the server has no reason to keep one.
 *
 * Order comes from the adapter when it gives one, because it is the only side
 * that sees every tab; the local list supplies titles and the threads the
 * adapter has not heard of yet — a conversation started and not yet sent.
 */
export function mergeThreadRows(
  ids: readonly string[],
  summaries: readonly AgentThreadSummary[],
  meta: readonly AgentThreadMeta[],
): ThreadRow[] {
  const titles = new Map(meta.map((row) => [row.id, row.title]));
  const stamps = new Map(meta.map((row) => [row.id, row.updatedAt]));
  const seen = new Set<string>();
  const rows: ThreadRow[] = [];

  function push(id: string, summary?: AgentThreadSummary): void {
    if (seen.has(id)) return;
    seen.add(id);
    const updatedAt = summary?.updatedAt ?? stamps.get(id);
    rows.push({
      id,
      title: titles.get(id) ?? summary?.title ?? UNTITLED_THREAD,
      ...(updatedAt === undefined ? {} : { updatedAt }),
      ...(summary?.running === undefined ? {} : { running: summary.running }),
    });
  }

  for (const summary of summaries) push(summary.id, summary);
  for (const id of ids) push(id);
  return rows;
}
