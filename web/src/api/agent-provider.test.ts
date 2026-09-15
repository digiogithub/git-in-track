/**
 * The fake provider's agent surface (task GIT-T-0030).
 *
 * The store tests drive it end to end; this file asserts the fake itself, so
 * a test that later fails against it can tell a broken store from a broken
 * fixture replay.
 */

import type { AguiEvent } from '@pando-ai/sdk/agui/client';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleAgentInfo } from '@/api/fake-provider';
import { ProviderError } from '@/api/provider';

const turn: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: 't1', runId: 'r1' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm1', delta: 'hi' },
  { type: 'RUN_FINISHED', threadId: 't1', runId: 'r1', outcome: 'success' },
];

async function drain(stream: AsyncIterable<AguiEvent>): Promise<AguiEvent[]> {
  const seen: AguiEvent[] = [];
  for await (const event of stream) seen.push(event);
  return seen;
}

describe('FakeProvider without an agent', () => {
  it('advertises no agent capability and refuses every call', async () => {
    const provider = new FakeProvider();
    expect(provider.capabilities.agent).toBe(false);
    await expect(provider.getAgentInfo()).rejects.toBeInstanceOf(ProviderError);
    await expect(provider.getAgentInfo()).rejects.toMatchObject({ code: 'not_supported' });
  });
});

describe('FakeProvider with a scripted agent', () => {
  it('replays a scripted event sequence and records what was posted', async () => {
    const provider = new FakeProvider({ agent: { events: turn } });
    expect(provider.capabilities.agent).toBe(true);

    const input = { threadId: 't1', runId: 'r1', messages: [] };
    const seen = await drain(provider.runAgent(input));

    expect(seen).toEqual(turn);
    expect(provider.agentRuns).toEqual([input]);
  });

  it('takes one turn per run and repeats the last once the script runs dry', async () => {
    const second: AguiEvent[] = [{ type: 'RUN_FINISHED', threadId: 't1', runId: 'r2' }];
    const provider = new FakeProvider({ agent: { turns: [turn, second] } });

    expect(await drain(provider.runAgent({ threadId: 't1', runId: 'a', messages: [] }))).toEqual(
      turn,
    );
    expect(await drain(provider.runAgent({ threadId: 't1', runId: 'b', messages: [] }))).toEqual(
      second,
    );
    expect(await drain(provider.runAgent({ threadId: 't1', runId: 'c', messages: [] }))).toEqual(
      second,
    );
  });

  it('answers the discovery document, the thread list and stored messages', async () => {
    const provider = new FakeProvider({
      agent: {
        events: turn,
        threads: [{ id: 't1', title: 'First' }],
        messages: { t1: [{ id: 'm0', role: 'user', content: 'earlier' }] },
      },
    });

    await expect(provider.getAgentInfo()).resolves.toEqual(sampleAgentInfo);
    await expect(provider.getAgentHealth()).resolves.toMatchObject({ ok: true });
    await expect(provider.listAgentThreads()).resolves.toEqual([{ id: 't1', title: 'First' }]);
    await expect(provider.getAgentThreadMessages('t1')).resolves.toEqual([
      { id: 'm0', role: 'user', content: 'earlier' },
    ]);
    await expect(provider.getAgentThreadMessages('nope')).resolves.toEqual([]);
  });

  it('fails the run when the script says so', async () => {
    const provider = new FakeProvider({
      agent: { events: turn, runError: { code: 'permission_denied', message: 'no' } },
    });
    await expect(
      drain(provider.runAgent({ threadId: 't', runId: 'r', messages: [] })),
    ).rejects.toMatchObject({ code: 'permission_denied' });
  });

  it('breaks the stream mid-flight when the script says so', async () => {
    const provider = new FakeProvider({
      agent: { events: turn, streamError: { code: 'internal', message: 'broke' }, throwAfter: 1 },
    });
    await expect(
      drain(provider.runAgent({ threadId: 't', runId: 'r', messages: [] })),
    ).rejects.toMatchObject({ code: 'internal', message: 'broke' });
  });

  it('aborts the replay when the signal does', async () => {
    const provider = new FakeProvider({ agent: { events: turn } });
    const controller = new AbortController();
    const stream = provider.runAgent(
      { threadId: 't', runId: 'r', messages: [] },
      { signal: controller.signal },
    );
    const iterator = stream[Symbol.asyncIterator]();
    await iterator.next();
    controller.abort();
    await expect(iterator.next()).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('records deletes and cancels', async () => {
    const provider = new FakeProvider({ agent: { events: turn } });
    await provider.deleteAgentThread('t1');
    await provider.cancelAgentRun('t1');
    expect(provider.agentDeletes).toEqual(['t1']);
    expect(provider.agentCancels).toEqual(['t1']);
  });
});
