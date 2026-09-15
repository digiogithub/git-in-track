import { beforeEach, describe, expect, it } from 'vitest';

import type { AgentThreadSummary } from '@/api/provider';
import type { AgentMessage } from '@/features/agent/types';
import { isVisible, mergeThreadRows } from '@/features/agent/ui/model';
import {
  THREAD_META_KEY,
  UNTITLED_THREAD,
  deriveThreadTitle,
  forgetThreadMeta,
  readThreadMeta,
  rememberThreadMeta,
} from '@/features/agent/ui/threadMeta';

const REPO = 'repo-1';

function message(partial: Partial<AgentMessage> & Pick<AgentMessage, 'id' | 'role'>): AgentMessage {
  return { text: '', toolCalls: [], ...partial };
}

beforeEach(() => {
  globalThis.localStorage.clear();
});

describe('thread titles', () => {
  it('derives the title from the first user message, not the first message', () => {
    const title = deriveThreadTitle([
      message({ id: 's', role: 'system', text: 'You are a helpful agent.' }),
      message({ id: 'u', role: 'user', text: 'Which stories are\n still open?' }),
      message({ id: 'a', role: 'assistant', text: 'Three.' }),
    ]);

    expect(title).toBe('Which stories are still open?');
  });

  it('falls back to a placeholder when nobody has said anything', () => {
    expect(deriveThreadTitle([])).toBe(UNTITLED_THREAD);
    expect(deriveThreadTitle([message({ id: 'u', role: 'user', text: '   ' })])).toBe(
      UNTITLED_THREAD,
    );
  });

  it('cuts a long title on a word boundary', () => {
    const title = deriveThreadTitle([
      message({
        id: 'u',
        role: 'user',
        text: 'Summarise every story of the current sprint and tell me which ones are blocked',
      }),
    ]);

    expect(title.length).toBeLessThanOrEqual(61);
    expect(title.endsWith('…')).toBe(true);
    expect(title).not.toContain(' …');
  });

  it('survives a reload, newest first, and forgets on delete', () => {
    rememberThreadMeta(REPO, { id: 'a', title: 'First', updatedAt: '2026-09-01T00:00:00Z' });
    rememberThreadMeta(REPO, { id: 'b', title: 'Second', updatedAt: '2026-09-02T00:00:00Z' });

    expect(readThreadMeta(REPO).map((row) => row.id)).toEqual(['b', 'a']);
    expect(globalThis.localStorage.getItem(THREAD_META_KEY)).toContain('Second');

    expect(forgetThreadMeta(REPO, 'b').map((row) => row.id)).toEqual(['a']);
    expect(readThreadMeta(REPO).map((row) => row.id)).toEqual(['a']);
  });

  it('keeps repositories apart and ignores a corrupt file', () => {
    rememberThreadMeta(REPO, { id: 'a', title: 'Mine', updatedAt: '2026-09-01T00:00:00Z' });
    expect(readThreadMeta('other')).toEqual([]);

    globalThis.localStorage.setItem(THREAD_META_KEY, 'not json');
    expect(readThreadMeta(REPO)).toEqual([]);
  });
});

describe('mergeThreadRows', () => {
  const summaries: AgentThreadSummary[] = [
    { id: 'b', title: 'Adapter title', updatedAt: '2026-09-02T00:00:00Z', running: true },
    { id: 'a' },
  ];

  it('orders by the adapter and takes the title from the local list', () => {
    const rows = mergeThreadRows(['a', 'b'], summaries, [
      { id: 'b', title: 'What I called it', updatedAt: '2026-09-02T00:00:00Z' },
    ]);

    expect(rows.map((row) => row.id)).toEqual(['b', 'a']);
    expect(rows[0]?.title).toBe('What I called it');
    expect(rows[0]?.running).toBe(true);
    expect(rows[1]?.title).toBe(UNTITLED_THREAD);
  });

  it('keeps a thread the adapter has never heard of, without duplicating it', () => {
    const rows = mergeThreadRows(['fresh', 'b'], summaries, []);

    expect(rows.map((row) => row.id)).toEqual(['b', 'a', 'fresh']);
  });
});

describe('isVisible', () => {
  it('hides the channels and the tool results, which the cards already show', () => {
    expect(isVisible(message({ id: '1', role: 'tool', text: 'main.go', toolCallId: 'tc1' }))).toBe(
      false,
    );
    expect(isVisible(message({ id: '2', role: 'system', text: 'prompt' }))).toBe(false);
    expect(isVisible(message({ id: '3', role: 'developer', text: 'prompt' }))).toBe(false);
    expect(isVisible(message({ id: '4', role: 'reasoning', text: 'thinking' }))).toBe(false);
  });

  it('keeps an assistant message that is only a tool call', () => {
    expect(
      isVisible(
        message({
          id: '5',
          role: 'assistant',
          toolCalls: [{ id: 'tc1', name: 'ls', argsText: '{}', args: {}, status: 'done' }],
        }),
      ),
    ).toBe(true);
    expect(isVisible(message({ id: '6', role: 'assistant' }))).toBe(false);
    expect(isVisible(message({ id: '7', role: 'user' }))).toBe(false);
  });
});
