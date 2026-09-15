/**
 * The store's half of the interrupt protocol (tasks GIT-T-0067 and
 * GIT-T-0098): what gets answered without a human, and what does not.
 */

import { PERMISSION_TOOL_NAME, QUESTION_TOOL_NAME } from '@pando-ai/sdk/agui/client';
import type { AguiEvent } from '@pando-ai/sdk/agui/client';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { FakeAgent } from '@/api/fake-provider';
import { FakeProvider } from '@/api/fake-provider';
import {
  frontendToolInterrupt,
  permissionInterrupt,
  snapshotState,
} from '@/features/agent/fixtures';
import { createAgentStore } from '@/features/agent/store';
import { ThreadGuard } from '@/features/agent/threads';
import type { ChannelLike } from '@/features/agent/threads';
import { createToolRunner } from '@/features/agent/tools/registry';
import type { ToolContext } from '@/features/agent/tools/types';

const REPO = 'repo-1';
const THREAD = 'thread-fixture';

function silentChannel(): ChannelLike {
  return { postMessage: () => {}, close: () => {}, onmessage: null };
}

function guard(): ThreadGuard {
  return new ThreadGuard({
    channelFactory: () => silentChannel(),
    wait: () => Promise.resolve(),
    tabId: 'tab-a',
  });
}

/** One turn parked on a frontend tool this client actually implements. */
function toolInterrupt(name: string, args: unknown): AguiEvent[] {
  return [
    { type: 'RUN_STARTED', threadId: THREAD, runId: 'run-tool' },
    { type: 'STATE_SNAPSHOT', snapshot: snapshotState },
    { type: 'TOOL_CALL_START', toolCallId: 'ft-1', toolCallName: name, parentMessageId: 'f1' },
    { type: 'TOOL_CALL_ARGS', toolCallId: 'ft-1', delta: JSON.stringify(args) },
    { type: 'TOOL_CALL_END', toolCallId: 'ft-1' },
    { type: 'RUN_FINISHED', threadId: THREAD, runId: 'run-tool', outcome: 'interrupt' },
  ];
}

const afterTool: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD, runId: 'run-after' },
  { type: 'TEXT_MESSAGE_START', messageId: 'a1', role: 'assistant' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'a1', delta: 'Opened it.' },
  { type: 'TEXT_MESSAGE_END', messageId: 'a1' },
  { type: 'RUN_FINISHED', threadId: THREAD, runId: 'run-after', outcome: 'success' },
];

/** A permission prompt whose arguments never finished streaming. */
const malformedPermission: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD, runId: 'run-bad-perm' },
  {
    type: 'TOOL_CALL_START',
    toolCallId: 'perm-bad',
    toolCallName: PERMISSION_TOOL_NAME,
    parentMessageId: 'p1',
  },
  { type: 'TOOL_CALL_ARGS', toolCallId: 'perm-bad', delta: '{"toolNa' },
  { type: 'TOOL_CALL_END', toolCallId: 'perm-bad' },
  { type: 'RUN_FINISHED', threadId: THREAD, runId: 'run-bad-perm', outcome: 'interrupt' },
];

/** A question nobody answered in time, so nothing askable reached the client. */
const malformedQuestion: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD, runId: 'run-bad-q' },
  {
    type: 'TOOL_CALL_START',
    toolCallId: 'q-bad',
    toolCallName: QUESTION_TOOL_NAME,
    parentMessageId: 'q1',
  },
  { type: 'TOOL_CALL_ARGS', toolCallId: 'q-bad', delta: '{"questions":[]}' },
  { type: 'TOOL_CALL_END', toolCallId: 'q-bad' },
  { type: 'RUN_FINISHED', threadId: THREAD, runId: 'run-bad-q', outcome: 'interrupt' },
];

function toolContext(overrides: Partial<ToolContext> = {}) {
  const navigate = vi.fn();
  const ctx: ToolContext = { navigate, project: 'GIT', ...overrides };
  return { ctx, navigate };
}

async function attached(agent: FakeAgent, context?: ToolContext) {
  const provider = new FakeProvider({ agent });
  const store = createAgentStore();
  await store.getState().attach({
    provider,
    repo: REPO,
    guard: guard(),
    ...(context === undefined ? {} : { tools: createToolRunner(context) }),
  });
  return { provider, store, state: () => store.getState() };
}

/** The trailing `tool` message of the nth run the store posted. */
function trailingTool(provider: FakeProvider, index: number) {
  return provider.agentRuns[index]?.messages?.at(-1);
}

/** That message's content, which the protocol allows to be either shape. */
function trailingContent(provider: FakeProvider, index: number): string {
  const content = trailingTool(provider, index)?.content;
  return typeof content === 'string' ? content : '';
}

beforeEach(() => {
  globalThis.localStorage.clear();
});

describe('frontend tools ride the interrupt protocol', () => {
  it('executes the tool and resumes the same thread with its result', async () => {
    const { ctx, navigate } = toolContext();
    const { provider, state } = await attached(
      { turns: [toolInterrupt('open_item', { id: 'GIT-US-0061' }), afterTool] },
      ctx,
    );

    await state().send('open the story');

    expect(navigate).toHaveBeenCalledWith({
      to: '/p/$project/items/$id',
      params: { project: 'GIT', id: 'GIT-US-0061' },
    });
    expect(provider.agentRuns).toHaveLength(2);
    expect(provider.agentRuns[1]?.threadId).toBe(provider.agentRuns[0]?.threadId);
    expect(trailingTool(provider, 1)).toMatchObject({ role: 'tool', toolCallId: 'ft-1' });
    expect(JSON.parse(trailingContent(provider, 1))).toMatchObject({
      opened: 'GIT-US-0061',
    });
    // The run continued rather than staying parked, and the step is visible.
    expect(state().runStatus).toBe('idle');
    expect(state().interrupt).toBeNull();
    expect(state().messages.at(-1)?.text).toBe('Opened it.');
    const toolCall = state().messages.flatMap((message) => message.toolCalls)[0];
    expect(toolCall).toMatchObject({ name: 'open_item', status: 'done' });
  });

  it('declares the registry on every run, so the agent-pool key holds', async () => {
    const { ctx } = toolContext();
    const { provider, state } = await attached(
      { turns: [toolInterrupt('open_item', { id: 'GIT-US-0061' }), afterTool] },
      ctx,
    );

    await state().send('open the story');

    for (const run of provider.agentRuns) {
      expect(run.tools?.map((tool) => tool.name)).toEqual([
        'open_item',
        'open_kb_page',
        'focus_board_card',
        'apply_backlog_filter',
        'show_items',
      ]);
    }
  });

  it('resumes with an error for a tool nobody implements, and never hangs', async () => {
    const { ctx, navigate } = toolContext();
    // The fixture calls `openItem`, which is not a registered name.
    const { provider, state } = await attached({ turns: [frontendToolInterrupt, afterTool] }, ctx);

    await state().send('open the story');

    expect(navigate).not.toHaveBeenCalled();
    expect(provider.agentRuns).toHaveLength(2);
    expect(JSON.parse(trailingContent(provider, 1))).toMatchObject({
      error: 'unknown_tool',
    });
    expect(state().runStatus).toBe('idle');
  });

  it('resumes with a validation error rather than navigating on bad arguments', async () => {
    const { ctx, navigate } = toolContext();
    const { provider, state } = await attached(
      { turns: [toolInterrupt('open_item', { id: '../../etc/passwd' }), afterTool] },
      ctx,
    );

    await state().send('open that file');

    expect(navigate).not.toHaveBeenCalled();
    expect(JSON.parse(trailingContent(provider, 1))).toMatchObject({
      error: 'invalid_arguments',
      field: 'id',
    });
  });

  it('leaves a frontend tool parked when no registry is attached', async () => {
    const { state } = await attached({ turns: [frontendToolInterrupt] });

    await state().send('open the story');

    expect(state().runStatus).toBe('interrupted');
    expect(state().interrupt?.toolCalls[0]?.name).toBe('openItem');
  });
});

describe('human-in-the-loop prompts are never answered automatically', () => {
  it('parks a permission prompt even with a registry attached', async () => {
    const { ctx } = toolContext();
    const { provider, state } = await attached({ turns: [permissionInterrupt] }, ctx);

    await state().send('write a file');

    expect(state().runStatus).toBe('interrupted');
    expect(state().interrupt?.toolCalls[0]?.name).toBe(PERMISSION_TOOL_NAME);
    expect(provider.agentRuns).toHaveLength(1);
  });

  it('auto-approves only a tool this thread granted, and forgets it on a new one', async () => {
    const { ctx } = toolContext();
    const { provider, state } = await attached({ turns: [permissionInterrupt, afterTool] }, ctx);

    state().allowToolForThread('write');
    expect(state().alwaysAllowed).toEqual(['write']);

    await state().send('write a file');

    expect(provider.agentRuns).toHaveLength(2);
    expect(trailingTool(provider, 1)).toMatchObject({
      role: 'tool',
      toolCallId: 'perm-1',
      content: '{"approved":true}',
    });
    expect(state().runStatus).toBe('idle');

    await state().newThread();
    expect(state().alwaysAllowed).toEqual([]);
  });

  it('revokes a grant', async () => {
    const { state } = await attached({ turns: [permissionInterrupt] });
    state().allowToolForThread('write');
    state().revokeToolForThread('write');
    expect(state().alwaysAllowed).toEqual([]);
  });

  it('denies a permission prompt whose arguments never arrived', async () => {
    const { ctx } = toolContext();
    const { provider, state } = await attached({ turns: [malformedPermission, afterTool] }, ctx);

    await state().send('do something');

    expect(trailingTool(provider, 1)).toMatchObject({
      toolCallId: 'perm-bad',
      content: '{"approved":false}',
    });
    expect(state().runStatus).toBe('idle');
  });

  it('cancels a question that carried nothing askable', async () => {
    const { ctx } = toolContext();
    const { provider, state } = await attached({ turns: [malformedQuestion, afterTool] }, ctx);

    await state().send('ask me something');

    expect(JSON.parse(trailingContent(provider, 1))).toEqual({
      cancelled: true,
      answers: [],
    });
  });
});

describe('hydrate', () => {
  it('restores a known thread without opening a stream', async () => {
    globalThis.localStorage.setItem(`gintrack:agent-active-thread:${REPO}`, 'thread-known');
    const { provider, state } = await attached({
      turns: [afterTool],
      messages: {
        'thread-known': [
          { id: 'h1', role: 'user', content: 'earlier prompt' },
          { id: 'h2', role: 'assistant', content: 'earlier reply' },
        ],
      },
    });

    expect(state().messages).toHaveLength(0);

    await state().hydrate();

    expect(state().messages.map((message) => message.text)).toEqual([
      'earlier prompt',
      'earlier reply',
    ]);
    // No run was posted and no thread stream was opened.
    expect(provider.agentRuns).toHaveLength(0);
    expect(state().runStatus).toBe('idle');
  });

  it('surfaces a failure as an error instead of throwing', async () => {
    const { store, state } = await attached({ turns: [afterTool] });
    store.setState({ thread: null });
    await expect(state().hydrate()).resolves.toBeUndefined();
  });
});

describe('registerTools', () => {
  it('can be attached after the fact and withdrawn', async () => {
    const { ctx } = toolContext();
    const { state } = await attached({
      turns: [toolInterrupt('open_item', { id: 'GIT-US-0061' })],
    });

    state().registerTools(createToolRunner(ctx));
    expect(state().runner).not.toBeNull();
    state().registerTools(null);
    expect(state().runner).toBeNull();
  });
});
