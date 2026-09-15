import type { AguiEvent } from '@pando-ai/sdk/agui/client';
import { describe, expect, it, vi } from 'vitest';

import type { DataProvider, RunAgentInput } from '@/api/provider';
import { buildRunInput, createAgentTransport, patchApplies } from '@/features/agent/client';
import { snapshotState } from '@/features/agent/fixtures';

type RunCall = { input: RunAgentInput; options: { repo?: string; signal?: AbortSignal } };

function providerYielding(events: AguiEvent[], calls: RunCall[]): DataProvider {
  return {
    async *runAgent(input: RunAgentInput, options = {}) {
      calls.push({ input, options });
      for (const event of events) {
        await Promise.resolve();
        yield event;
      }
    },
  } as unknown as DataProvider;
}

describe('buildRunInput', () => {
  it('carries the ids, transcript and tools the adapter expects', () => {
    const input = buildRunInput({
      threadId: 't-1',
      runId: 'r-1',
      messages: [{ id: 'm1', role: 'user', content: 'hi' }],
      tools: [{ name: 'openItem' }],
      context: [{ description: 'repo', value: 'git-in-track' }],
      state: { client: 1 },
    });

    expect(input).toMatchObject({
      threadId: 't-1',
      runId: 'r-1',
      messages: [{ id: 'm1', role: 'user', content: 'hi' }],
      tools: [{ name: 'openItem' }],
      context: [{ description: 'repo', value: 'git-in-track' }],
      state: { client: 1 },
    });
  });

  it('turns a bare prompt into a single user message and invents ids', () => {
    const input = buildRunInput({ prompt: 'hello' });
    expect(input.messages).toHaveLength(1);
    expect(input.messages?.[0]).toMatchObject({ role: 'user', content: 'hello' });
    expect(input.threadId).toMatch(/^thread-/);
    expect(input.runId).toMatch(/^run-/);
  });

  it('omits every optional field it was not given', () => {
    const input = buildRunInput({ threadId: 't', runId: 'r', messages: [] });
    expect(Object.keys(input).sort()).toEqual(['messages', 'runId', 'threadId']);
  });
});

describe('patchApplies', () => {
  it('rejects a patch that arrives before any snapshot', () => {
    expect(patchApplies(undefined, [{ op: 'replace', path: '/agent', value: 'x' }])).toBe(false);
  });

  it('accepts a patch the SDK can apply', () => {
    expect(
      patchApplies(snapshotState, [{ op: 'replace', path: '/todos/0/status', value: 'completed' }]),
    ).toBe(true);
  });

  it('rejects an out-of-bounds patch without mutating the document', () => {
    const before = structuredClone(snapshotState);
    expect(
      patchApplies(snapshotState, [{ op: 'replace', path: '/todos/9/status', value: 'x' }]),
    ).toBe(false);
    expect(snapshotState).toEqual(before);
  });
});

describe('createAgentTransport', () => {
  it('runs through the provider, passing the repository and the signal', async () => {
    const calls: RunCall[] = [];
    const controller = new AbortController();
    const transport = createAgentTransport({
      provider: providerYielding([{ type: 'RUN_STARTED', threadId: 't', runId: 'r' }], calls),
      repo: 'repo-1',
    });

    const seen: AguiEvent[] = [];
    for await (const event of transport.run({
      threadId: 't',
      runId: 'r',
      messages: [],
      signal: controller.signal,
    })) {
      seen.push(event);
    }

    expect(seen).toHaveLength(1);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.options.repo).toBe('repo-1');
    expect(calls[0]?.options.signal).toBe(controller.signal);
    expect(calls[0]?.input.threadId).toBe('t');
  });

  it('drops a STATE_DELTA the thread would throw on, and keeps streaming', async () => {
    const calls: RunCall[] = [];
    const dropped = vi.fn();
    const transport = createAgentTransport({
      provider: providerYielding(
        [
          { type: 'STATE_DELTA', delta: [{ op: 'replace', path: '/todos/9/status', value: 'x' }] },
          { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm', delta: 'ok' },
        ],
        calls,
      ),
      getState: () => snapshotState,
      onDroppedPatch: dropped,
    });

    const types: string[] = [];
    for await (const event of transport.run({ threadId: 't', runId: 'r', messages: [] })) {
      types.push(event.type);
    }

    expect(types).toEqual(['TEXT_MESSAGE_CONTENT']);
    expect(dropped).toHaveBeenCalledTimes(1);
    expect(dropped.mock.calls[0]?.[1]).toBe('the patch does not apply');
  });

  it('drops a STATE_DELTA that arrives before the first snapshot', async () => {
    const dropped = vi.fn();
    const transport = createAgentTransport({
      provider: providerYielding(
        [{ type: 'STATE_DELTA', delta: [{ op: 'replace', path: '/agent', value: 'x' }] }],
        [],
      ),
      getState: () => undefined,
      onDroppedPatch: dropped,
    });

    const types: string[] = [];
    for await (const event of transport.run({ threadId: 't', runId: 'r', messages: [] })) {
      types.push(event.type);
    }

    expect(types).toEqual([]);
    expect(dropped.mock.calls[0]?.[1]).toBe('no STATE_SNAPSHOT yet');
  });
});
