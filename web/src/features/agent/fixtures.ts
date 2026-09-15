/**
 * Recorded AG-UI streams, for the tests (story GIT-US-0053).
 *
 * `@pando-ai/sdk@0.2.0` ships no SSE fixtures (its `files` field is `dist`,
 * `README.md` and `LICENSE`), so these are written against the wire shapes in
 * `internal/agui/events.go` as the SDK's own type declarations mirror them —
 * which is the same source both sides were generated from. Events are arrays
 * rather than raw SSE text because the parser under test is the SDK's
 * `parseSSE`, exercised where it lives: in the provider (see
 * `companion-provider.test.ts`, which does feed it real `data:` frames).
 */

import type { AguiEvent } from '@pando-ai/sdk/agui/client';

export const THREAD_ID = 'thread-fixture';
export const RUN_ID = 'run-fixture';

/** The state document a `STATE_SNAPSHOT` seeds. */
export const snapshotState = {
  thread: THREAD_ID,
  session: 'session-1',
  agent: 'coder',
  model: { id: 'claude', name: 'Claude', provider: 'anthropic', contextWindow: 200000 },
  todos: [{ content: 'Read the repo', status: 'pending' as const, priority: 'high' as const }],
  tokenUsage: null,
  files: [],
  subAgents: [],
};

/**
 * One turn: reasoning, then text, then a tool call and its result, then more
 * text. It is deliberately interleaved — the ordering of the rendered message
 * list is the thing worth asserting.
 */
export const simpleTurn: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD_ID, runId: RUN_ID },
  { type: 'STATE_SNAPSHOT', snapshot: snapshotState },
  { type: 'REASONING_START', messageId: 'm1' },
  { type: 'REASONING_MESSAGE_START', messageId: 'm1', role: 'reasoning' },
  { type: 'REASONING_MESSAGE_CONTENT', messageId: 'm1', delta: 'Let me look' },
  { type: 'REASONING_MESSAGE_CONTENT', messageId: 'm1', delta: ' at the repo.' },
  { type: 'REASONING_MESSAGE_END', messageId: 'm1' },
  { type: 'REASONING_END', messageId: 'm1' },
  { type: 'TEXT_MESSAGE_START', messageId: 'm1', role: 'assistant' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm1', delta: 'Reading ' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm1', delta: 'the files.' },
  { type: 'TEXT_MESSAGE_END', messageId: 'm1' },
  { type: 'TOOL_CALL_START', toolCallId: 'tc1', toolCallName: 'ls', parentMessageId: 'm1' },
  { type: 'TOOL_CALL_ARGS', toolCallId: 'tc1', delta: '{"path":' },
  { type: 'TOOL_CALL_ARGS', toolCallId: 'tc1', delta: '"/src"}' },
  { type: 'TOOL_CALL_END', toolCallId: 'tc1' },
  { type: 'TOOL_CALL_RESULT', messageId: 'm2', toolCallId: 'tc1', content: 'main.go' },
  {
    type: 'STATE_DELTA',
    delta: [{ op: 'replace', path: '/todos/0/status', value: 'completed' }],
  },
  { type: 'TEXT_MESSAGE_START', messageId: 'm3', role: 'assistant' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'm3', delta: 'There is one file.' },
  { type: 'TEXT_MESSAGE_END', messageId: 'm3' },
  { type: 'RUN_FINISHED', threadId: THREAD_ID, runId: RUN_ID, outcome: 'success' },
];

/**
 * A turn that ends parked on Pando's synthetic permission prompt: the
 * `pando_permission_request` tool call with no result, and
 * `RUN_FINISHED{outcome:"interrupt"}`.
 */
export const permissionInterrupt: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD_ID, runId: 'run-1' },
  { type: 'STATE_SNAPSHOT', snapshot: snapshotState },
  { type: 'TEXT_MESSAGE_START', messageId: 'p1', role: 'assistant' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'p1', delta: 'I need to write a file.' },
  { type: 'TEXT_MESSAGE_END', messageId: 'p1' },
  {
    type: 'TOOL_CALL_START',
    toolCallId: 'perm-1',
    toolCallName: 'pando_permission_request',
    parentMessageId: 'p1',
  },
  {
    type: 'TOOL_CALL_ARGS',
    toolCallId: 'perm-1',
    delta: '{"toolName":"write","action":"write","path":"main.go"}',
  },
  { type: 'TOOL_CALL_END', toolCallId: 'perm-1' },
  { type: 'RUN_FINISHED', threadId: THREAD_ID, runId: 'run-1', outcome: 'interrupt' },
];

/** What the agent streams once the permission was approved. */
export const permissionResumed: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD_ID, runId: 'run-2' },
  { type: 'TEXT_MESSAGE_START', messageId: 'p2', role: 'assistant' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'p2', delta: 'Written.' },
  { type: 'TEXT_MESSAGE_END', messageId: 'p2' },
  { type: 'RUN_FINISHED', threadId: THREAD_ID, runId: 'run-2', outcome: 'success' },
];

/**
 * A frontend-tool interrupt: a tool the *browser* implements, not a HITL
 * prompt. The shape on the wire is identical — the only difference is the
 * name, and that the answer is a real result rather than an approval.
 */
export const frontendToolInterrupt: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD_ID, runId: 'run-ft-1' },
  { type: 'STATE_SNAPSHOT', snapshot: snapshotState },
  {
    type: 'TOOL_CALL_START',
    toolCallId: 'ft-1',
    toolCallName: 'openItem',
    parentMessageId: 'f1',
  },
  { type: 'TOOL_CALL_ARGS', toolCallId: 'ft-1', delta: '{"id":"GIT-US-0053"}' },
  { type: 'TOOL_CALL_END', toolCallId: 'ft-1' },
  { type: 'RUN_FINISHED', threadId: THREAD_ID, runId: 'run-ft-1', outcome: 'interrupt' },
];

/** A run the agent itself failed, over an otherwise healthy stream. */
export const runErrorTurn: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD_ID, runId: 'run-err' },
  { type: 'TEXT_MESSAGE_START', messageId: 'e1', role: 'assistant' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'e1', delta: 'Working' },
  { type: 'RUN_ERROR', message: 'the model provider timed out' },
];

/** A `STATE_DELTA` that cannot be applied: the path resolves to nothing. */
export const badPatchTurn: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD_ID, runId: 'run-bad' },
  { type: 'STATE_SNAPSHOT', snapshot: snapshotState },
  { type: 'STATE_DELTA', delta: [{ op: 'replace', path: '/todos/9/status', value: 'nope' }] },
  {
    type: 'STATE_DELTA',
    delta: [{ op: 'add', path: '/files/-', value: { path: 'a.go', name: 'a.go', action: 'read' } }],
  },
  { type: 'TEXT_MESSAGE_START', messageId: 'b1', role: 'assistant' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 'b1', delta: 'Still here.' },
  { type: 'TEXT_MESSAGE_END', messageId: 'b1' },
  { type: 'RUN_FINISHED', threadId: THREAD_ID, runId: 'run-bad', outcome: 'success' },
];

/** A `STATE_DELTA` before any snapshot, plus an event type this SDK never saw. */
export const strayEventsTurn: AguiEvent[] = [
  { type: 'RUN_STARTED', threadId: THREAD_ID, runId: 'run-stray' },
  { type: 'STATE_DELTA', delta: [{ op: 'replace', path: '/agent', value: 'other' }] },
  { type: 'TEXT_MESSAGE_END', messageId: 'never-started' },
  { type: 'TEXT_MESSAGE_START', messageId: 's1', role: 'assistant' },
  { type: 'TEXT_MESSAGE_CONTENT', messageId: 's1', delta: 'Fine.' },
  { type: 'TEXT_MESSAGE_END', messageId: 's1' },
  { type: 'RUN_FINISHED', threadId: THREAD_ID, runId: 'run-stray', outcome: 'success' },
];
