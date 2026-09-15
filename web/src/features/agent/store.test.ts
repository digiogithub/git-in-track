import { PERMISSION_TOOL_NAME, approve, isPermissionRequest } from '@pando-ai/sdk/agui/client';
import { beforeEach, describe, expect, it } from 'vitest';

import type { FakeAgent } from '@/api/fake-provider';
import { FakeProvider } from '@/api/fake-provider';
import {
  badPatchTurn,
  frontendToolInterrupt,
  permissionInterrupt,
  permissionResumed,
  runErrorTurn,
  simpleTurn,
  snapshotState,
  strayEventsTurn,
} from '@/features/agent/fixtures';
import { createAgentStore } from '@/features/agent/store';
import { ThreadGuard, readActiveThreadId, readThreadIds } from '@/features/agent/threads';
import type { ChannelLike } from '@/features/agent/threads';

const REPO = 'repo-1';

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

function guard(tabId = 'tab-a', channelFactory = makeBus()): ThreadGuard {
  return new ThreadGuard({ channelFactory, wait: () => Promise.resolve(), tabId });
}

async function attached(agent: FakeAgent, options: { guard?: ThreadGuard } = {}) {
  const provider = new FakeProvider({ agent });
  const store = createAgentStore();
  await store.getState().attach({
    provider,
    repo: REPO,
    guard: options.guard ?? guard(),
  });
  return { provider, store, state: () => store.getState() };
}

beforeEach(() => {
  globalThis.localStorage.clear();
});

describe('identity and persistence', () => {
  it('keeps the thread id stable across turns and allocates a fresh run id', async () => {
    const { provider, state } = await attached({ turns: [simpleTurn, simpleTurn] });

    await state().send('first');
    await state().send('second');

    expect(provider.agentRuns).toHaveLength(2);
    const [first, second] = provider.agentRuns;
    expect(first?.threadId).toBe(second?.threadId);
    expect(first?.threadId).toBe(state().threadId);
    expect(first?.runId).not.toBe(second?.runId);
  });

  it('resends the whole transcript, growing each turn', async () => {
    const { provider, state } = await attached({ turns: [simpleTurn, simpleTurn] });

    await state().send('first');
    await state().send('second');

    const first = provider.agentRuns[0]?.messages ?? [];
    const second = provider.agentRuns[1]?.messages ?? [];
    expect(first).toHaveLength(1);
    expect(first[0]).toMatchObject({ role: 'user', content: 'first' });
    expect(second.length).toBeGreaterThan(first.length);
    expect(second[0]).toMatchObject({ role: 'user', content: 'first' });
    expect(second.at(-1)).toMatchObject({ role: 'user', content: 'second' });
  });

  it('persists the thread id per repository so a reload keeps its identity', async () => {
    const { state } = await attached({ events: simpleTurn });
    const threadId = state().threadId;

    expect(readThreadIds(REPO)).toEqual([threadId]);
    expect(readActiveThreadId(REPO)).toBe(threadId);

    // A reload: a second store over the same storage picks the same thread up.
    const provider = new FakeProvider({ agent: { events: simpleTurn } });
    const reloaded = createAgentStore();
    await reloaded.getState().attach({ provider, repo: REPO, guard: guard('tab-b') });
    expect(reloaded.getState().threadId).toBe(threadId);
  });
});

describe('the event stream', () => {
  it('builds an ordered message list interleaving text, reasoning and tool calls', async () => {
    const { state } = await attached({ events: simpleTurn });
    await state().send('what is in the repo?');

    const messages = state().messages;
    expect(messages.map((m) => m.role)).toEqual(['user', 'assistant', 'tool', 'assistant']);
    expect(messages[0]?.text).toBe('what is in the repo?');
    expect(messages[1]).toMatchObject({
      text: 'Reading the files.',
      reasoning: 'Let me look at the repo.',
    });
    expect(messages[1]?.toolCalls[0]).toMatchObject({
      name: 'ls',
      args: { path: '/src' },
      status: 'done',
      result: 'main.go',
    });
    expect(messages[3]?.text).toBe('There is one file.');
    expect(state().runStatus).toBe('idle');
  });

  it('applies a STATE_SNAPSHOT and then its deltas', async () => {
    const { state } = await attached({ events: simpleTurn });
    await state().send('go');

    expect(state().stateDoc?.todos[0]?.status).toBe('completed');
    expect(state().stateDoc?.agent).toBe(snapshotState.agent);
  });

  it('ignores an unapplicable patch instead of losing the run', async () => {
    const { state } = await attached({ events: badPatchTurn });
    await state().send('go');

    // The bad replace was dropped, the good append after it still landed, and
    // the reply that followed both arrived.
    expect(state().stateDoc?.todos[0]?.status).toBe('pending');
    expect(state().stateDoc?.files).toHaveLength(1);
    expect(state().messages.at(-1)?.text).toBe('Still here.');
    expect(state().runStatus).toBe('idle');
  });

  it('tolerates a delta before any snapshot and an end without a start', async () => {
    const { state } = await attached({ events: strayEventsTurn });
    await state().send('go');

    expect(state().runStatus).toBe('idle');
    expect(state().messages.at(-1)?.text).toBe('Fine.');
  });
});

describe('interrupts', () => {
  it('parks on RUN_FINISHED{outcome:interrupt} with the pending tool calls readable', async () => {
    const { state } = await attached({ turns: [permissionInterrupt] });
    await state().send('write a file');

    expect(state().runStatus).toBe('interrupted');
    const pending = state().interrupt?.toolCalls ?? [];
    expect(pending).toHaveLength(1);
    expect(pending[0]?.name).toBe(PERMISSION_TOOL_NAME);
    expect(isPermissionRequest(pending[0]!)).toBe(true);
    expect(pending[0]?.args).toMatchObject({ toolName: 'write', path: 'main.go' });
    // The transcript is intact, not restarted.
    expect(state().messages[1]?.text).toBe('I need to write a file.');
  });

  it('resumes with the same thread, a new run and a trailing tool message', async () => {
    const { provider, state } = await attached({
      turns: [permissionInterrupt, permissionResumed],
    });
    await state().send('write a file');
    const toolCallId = state().interrupt?.toolCalls[0]?.id ?? '';

    await state().resume(toolCallId, approve());

    expect(provider.agentRuns).toHaveLength(2);
    const [first, second] = provider.agentRuns;
    expect(second?.threadId).toBe(first?.threadId);
    expect(second?.runId).not.toBe(first?.runId);
    expect(second?.messages?.at(-1)).toMatchObject({
      role: 'tool',
      toolCallId,
      content: '{"approved":true}',
    });
    // The list continued rather than restarting.
    expect(state().messages.at(-1)?.text).toBe('Written.');
    expect(state().runStatus).toBe('idle');
    expect(state().interrupt).toBeNull();
  });

  it('parks the same way on a frontend-tool interrupt', async () => {
    const { state } = await attached({ turns: [frontendToolInterrupt] });
    await state().send('open the story');

    expect(state().runStatus).toBe('interrupted');
    const pending = state().interrupt?.toolCalls ?? [];
    expect(pending[0]).toMatchObject({ name: 'openItem', args: { id: 'GIT-US-0053' } });
    expect(isPermissionRequest(pending[0]!)).toBe(false);
  });
});

describe('failures', () => {
  it('surfaces a RUN_ERROR as a terminal error state', async () => {
    const { state } = await attached({ events: runErrorTurn });
    await state().send('go');

    expect(state().runStatus).toBe('error');
    expect(state().error).toMatchObject({ code: 'run_error' });
    expect(state().error?.message).toContain('timed out');
    // Whatever text arrived is kept: it is what the user saw.
    expect(state().messages.at(-1)?.text).toBe('Working');
  });

  it('surfaces a mid-stream transport failure the same way', async () => {
    const { state } = await attached({
      events: simpleTurn,
      streamError: { code: 'internal', message: 'the stream broke' },
      throwAfter: 3,
    });
    await state().send('go');

    expect(state().runStatus).toBe('error');
    expect(state().error).toMatchObject({ code: 'transport', message: 'the stream broke' });
  });

  it('surfaces a refusal to start the run', async () => {
    const { state } = await attached({
      events: simpleTurn,
      runError: { code: 'permission_denied', message: 'the token expired' },
    });
    await state().send('go');

    expect(state().runStatus).toBe('error');
    expect(state().error?.code).toBe('unauthorized');
  });
});

describe('cancel', () => {
  it('aborts the request and settles as cancelled without an open message', async () => {
    const { provider, store, state } = await attached({ events: simpleTurn });

    const running = state().send('go');
    // The stream yields between microtasks, so this lands mid-run.
    await Promise.resolve();
    await state().cancel();
    await running;

    expect(store.getState().runStatus).toBe('cancelled');
    expect(store.getState().error).toBeNull();
    expect(provider.agentCancels).toEqual([state().threadId]);
    // Nothing is left claiming to still be streaming.
    for (const message of store.getState().messages) {
      for (const call of message.toolCalls) expect(call.status).not.toBe('pending');
    }
  });
});

describe('threads', () => {
  it('swaps the message list and the state document when switching threads', async () => {
    const { state } = await attached({
      turns: [simpleTurn],
      messages: { 'thread-other': [{ id: 'o1', role: 'assistant', content: 'older reply' }] },
    });
    await state().send('go');
    expect(state().messages).toHaveLength(4);

    await state().selectThread('thread-other');

    expect(state().threadId).toBe('thread-other');
    expect(state().messages).toEqual([
      { id: 'o1', role: 'assistant', text: 'older reply', toolCalls: [] },
    ]);
    expect(state().stateDoc).toBeUndefined();
    expect(state().runStatus).toBe('idle');
  });

  it('starts a fresh thread with a new id and an empty transcript', async () => {
    const { state } = await attached({ turns: [simpleTurn] });
    await state().send('go');
    const first = state().threadId;

    await state().newThread();

    expect(state().threadId).not.toBe(first);
    expect(state().messages).toEqual([]);
    expect(readThreadIds(REPO)).toEqual([state().threadId, first]);
  });

  it('restores the transcript on reattach after a reload', async () => {
    const provider = new FakeProvider({
      agent: {
        messages: { 'thread-live': [{ id: 'h1', role: 'assistant', content: 'from the server' }] },
        reattach: [
          { type: 'TEXT_MESSAGE_START', messageId: 'h2', role: 'assistant' },
          { type: 'TEXT_MESSAGE_CONTENT', messageId: 'h2', delta: ' and the rest' },
          { type: 'TEXT_MESSAGE_END', messageId: 'h2' },
          { type: 'RUN_FINISHED', threadId: 'thread-live', runId: 'r', outcome: 'success' },
        ],
      },
    });
    const store = createAgentStore();
    await store.getState().attach({ provider, repo: REPO, guard: guard() });
    await store.getState().selectThread('thread-live');

    await store.getState().reattach();

    expect(store.getState().messages.map((m) => m.text)).toEqual([
      'from the server',
      ' and the rest',
    ]);
    expect(store.getState().runStatus).toBe('idle');
    // Reattaching posts nothing: it is the server's stream, not a new run.
    expect(provider.agentRuns).toHaveLength(0);
  });

  it('lists and deletes threads through the provider', async () => {
    const { provider, state } = await attached({
      turns: [simpleTurn],
      threads: [{ id: 'thread-1', title: 'First' }],
    });

    await state().refreshThreads();
    expect(state().threads).toEqual([{ id: 'thread-1', title: 'First' }]);

    await state().deleteThread('thread-1');
    expect(provider.agentDeletes).toEqual(['thread-1']);
    expect(state().threads).toEqual([]);
  });
});

describe('the one-thread-per-tab guard', () => {
  it('makes a second tab on the same thread read-only and refuses to run', async () => {
    const channelFactory = makeBus();
    const first = await attached(
      { turns: [simpleTurn] },
      { guard: guard('tab-a', channelFactory) },
    );
    const threadId = first.state().threadId ?? '';

    const provider = new FakeProvider({ agent: { turns: [simpleTurn] } });
    const second = createAgentStore();
    await second.getState().attach({
      provider,
      repo: REPO,
      guard: guard('tab-b', channelFactory),
    });

    expect(second.getState().threadId).toBe(threadId);
    expect(second.getState().readOnly).toBe(true);

    await second.getState().send('go');
    expect(provider.agentRuns).toHaveLength(0);
    expect(second.getState().error?.code).toBe('session_busy');
  });

  it('lets the first tab run', async () => {
    const { state } = await attached({ turns: [simpleTurn] });
    expect(state().readOnly).toBe(false);
    await state().send('go');
    expect(state().runStatus).toBe('idle');
  });
});
