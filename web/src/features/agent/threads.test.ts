import type { AguiEvent } from '@pando-ai/sdk/agui/client';
import { PandoThread } from '@pando-ai/sdk/agui/client';
import { beforeEach, describe, expect, it } from 'vitest';

import type { ChannelLike } from '@/features/agent/threads';
import {
  ThreadGuard,
  forgetThreadId,
  messageText,
  readActiveThreadId,
  readThreadIds,
  rememberThreadId,
  supplementaryReduce,
  toAgentMessages,
  writeActiveThreadId,
} from '@/features/agent/threads';

/** A `BroadcastChannel` that only reaches the other members of one bus. */
function makeBus() {
  const members = new Set<FakeChannel>();
  class FakeChannel implements ChannelLike {
    onmessage: ((event: { data: unknown }) => void) | null = null;
    constructor() {
      members.add(this);
    }
    postMessage(message: unknown): void {
      for (const member of members) {
        if (member === this) continue;
        member.onmessage?.({ data: message });
      }
    }
    close(): void {
      members.delete(this);
    }
  }
  return () => new FakeChannel();
}

/** A thread with no transport: `reduce` is exercised through the SDK itself. */
function bareThread(): PandoThread {
  return new PandoThread({
    client: { run: () => [] } as never,
    threadId: 'thread-x',
  });
}

describe('thread persistence', () => {
  beforeEach(() => {
    globalThis.localStorage.clear();
  });

  it('remembers thread ids per repository, newest first', () => {
    rememberThreadId('repo-a', 't1');
    rememberThreadId('repo-a', 't2');
    rememberThreadId('repo-b', 't3');

    expect(readThreadIds('repo-a')).toEqual(['t2', 't1']);
    expect(readThreadIds('repo-b')).toEqual(['t3']);
  });

  it('does not duplicate a thread it already knows', () => {
    rememberThreadId('repo-a', 't1');
    rememberThreadId('repo-a', 't2');
    expect(rememberThreadId('repo-a', 't1')).toEqual(['t1', 't2']);
  });

  it('forgets a thread and clears it as the active one', () => {
    rememberThreadId('repo-a', 't1');
    writeActiveThreadId('repo-a', 't1');
    expect(forgetThreadId('repo-a', 't1')).toEqual([]);
    expect(readActiveThreadId('repo-a')).toBeNull();
  });

  it('reads nothing rather than throwing on corrupt storage', () => {
    globalThis.localStorage.setItem('gintrack:agent-threads:repo-a', 'not json');
    expect(readThreadIds('repo-a')).toEqual([]);
  });
});

describe('ThreadGuard', () => {
  const wait = () => Promise.resolve();

  it('grants a thread nobody else holds', async () => {
    const guard = new ThreadGuard({ channelFactory: makeBus(), wait, tabId: 'a' });
    await expect(guard.claim('t1')).resolves.toBe(true);
    expect(guard.owned).toEqual(['t1']);
  });

  it('refuses a second tab opening a thread the first already owns', async () => {
    const channelFactory = makeBus();
    const first = new ThreadGuard({ channelFactory, wait, tabId: 'a' });
    const second = new ThreadGuard({ channelFactory, wait, tabId: 'b' });

    await expect(first.claim('t1')).resolves.toBe(true);
    await expect(second.claim('t1')).resolves.toBe(false);
    expect(second.owned).toEqual([]);
  });

  it('lets a second tab own a different thread', async () => {
    const channelFactory = makeBus();
    const first = new ThreadGuard({ channelFactory, wait, tabId: 'a' });
    const second = new ThreadGuard({ channelFactory, wait, tabId: 'b' });

    await first.claim('t1');
    await expect(second.claim('t2')).resolves.toBe(true);
  });

  it('hands a thread over once the owner releases it', async () => {
    const channelFactory = makeBus();
    const first = new ThreadGuard({ channelFactory, wait, tabId: 'a' });
    const second = new ThreadGuard({ channelFactory, wait, tabId: 'b' });

    await first.claim('t1');
    first.release('t1');
    await expect(second.claim('t1')).resolves.toBe(true);
  });

  it('owns everything when the runtime has no BroadcastChannel', async () => {
    const guard = new ThreadGuard({ channelFactory: () => null, wait });
    await expect(guard.claim('t1')).resolves.toBe(true);
  });
});

describe('supplementaryReduce', () => {
  it('replaces the transcript on MESSAGES_SNAPSHOT', () => {
    const thread = bareThread();
    (thread.messages as { id: string; role: 'user' }[]).push({ id: 'old', role: 'user' });

    supplementaryReduce(thread, {
      type: 'MESSAGES_SNAPSHOT',
      messages: [{ id: 'new', role: 'assistant', content: 'fresh' }],
    });

    expect(thread.messages).toEqual([{ id: 'new', role: 'assistant', content: 'fresh' }]);
  });

  it('appends a single-frame TEXT_MESSAGE_CHUNK', () => {
    const thread = bareThread();
    supplementaryReduce(thread, {
      type: 'TEXT_MESSAGE_CHUNK',
      messageId: 'c1',
      role: 'assistant',
      delta: 'one shot',
    });
    supplementaryReduce(thread, { type: 'TEXT_MESSAGE_CHUNK', messageId: 'c1', delta: '!' });

    expect(thread.messages).toEqual([{ id: 'c1', role: 'assistant', content: 'one shot!' }]);
  });

  it('records an ACTIVITY_SNAPSHOT as its own message', () => {
    const thread = bareThread();
    supplementaryReduce(thread, {
      type: 'ACTIVITY_SNAPSHOT',
      messageId: 'a1',
      activityType: 'search',
      content: { query: 'x' },
    });

    expect(thread.messages[0]).toMatchObject({ role: 'activity', activityType: 'search' });
  });

  it('ignores an event type it does not know', () => {
    const thread = bareThread();
    const unknown = { type: 'SOMETHING_NEW', payload: 1 } as unknown as AguiEvent;
    expect(() => {
      supplementaryReduce(thread, unknown);
    }).not.toThrow();
    expect(thread.messages).toEqual([]);
  });
});

describe('the AgentMessage projection', () => {
  it('flattens both content shapes the protocol allows', () => {
    expect(messageText('plain')).toBe('plain');
    expect(
      messageText([
        { type: 'text', text: 'a' },
        { type: 'image', url: 'x' },
        { type: 'text', text: 'b' },
      ]),
    ).toBe('ab');
    expect(messageText(undefined)).toBe('');
  });

  it('keeps reasoning apart from the visible reply and resolves tool results', () => {
    const thread = bareThread();
    const messages = thread.messages as {
      id: string;
      role: string;
      content?: string;
      toolCalls?: { id: string; function: { name: string; arguments: string } }[];
      toolCallId?: string;
    }[];
    messages.push({
      id: 'm1',
      role: 'assistant',
      content: 'Reading the files.',
      toolCalls: [{ id: 'tc1', function: { name: 'ls', arguments: '{"path":"/src"}' } }],
    });
    messages.push({ id: 'm2', role: 'tool', toolCallId: 'tc1', content: 'main.go' });
    thread.reasoning.set('m1', 'Let me look.');

    const view = toAgentMessages(thread);

    expect(view[0]).toMatchObject({
      id: 'm1',
      role: 'assistant',
      text: 'Reading the files.',
      reasoning: 'Let me look.',
    });
    expect(view[0]?.toolCalls[0]).toMatchObject({
      id: 'tc1',
      name: 'ls',
      args: { path: '/src' },
      status: 'done',
      result: 'main.go',
    });
    expect(view[1]).toMatchObject({ role: 'tool', toolCallId: 'tc1', text: 'main.go' });
  });
});
