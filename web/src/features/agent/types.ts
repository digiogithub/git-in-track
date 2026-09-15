/**
 * The agent feature's own vocabulary (story GIT-US-0053).
 *
 * Everything the AG-UI protocol already names is re-exported from the SDK's
 * browser-safe entry rather than restated here: the wire types are Pando's,
 * not ours, and a second copy of them would drift. What this module adds is
 * the handful of shapes the SDK has no opinion about — how a run reads to the
 * UI, how a failure reads to the UI, and the flattened message the chat wave
 * renders.
 *
 * `AgentMessage` is deliberately a *view* model: `PandoThread.messages` is the
 * transcript that goes back on the wire and must stay exactly as the protocol
 * wants it, so the projection into something a component can render happens
 * here and never mutates the transcript.
 */

import type {
  AguiEvent,
  AguiInfo,
  AguiMessage,
  AguiRole,
  AguiTool,
  PandoState,
  PandoTodo,
  PendingToolCall,
  RunAgentInput,
} from '@pando-ai/sdk/agui/client';

export type {
  AguiEvent,
  AguiInfo,
  AguiMessage,
  AguiRole,
  AguiTool,
  PandoState,
  PandoTodo,
  PendingToolCall,
  RunAgentInput,
};

export type { AgentHealth, AgentThreadSummary } from '@/api/provider';

/**
 * Where a run is.
 *
 * `interrupted` is not an error: the agent called a tool the browser owns (a
 * permission prompt, a question, a frontend tool) and the run is parked on the
 * server waiting for the answer. `cancelled` and `error` are both terminal and
 * both leave the transcript intact.
 */
export type AgentRunStatus = 'idle' | 'running' | 'interrupted' | 'cancelled' | 'error';

/**
 * Why a run stopped badly, in terms the UI can act on rather than an HTTP
 * status or a class name.
 *
 * - `not_supported` — this runtime has no agent (browser-only mode).
 * - `unauthorized` — the companion refused the token; the session is over.
 * - `session_busy` — a run is already attached to this thread, which is the
 *   one hard Pando constraint the tab guard exists to avoid hitting.
 * - `cancelled` — the run was aborted, here or elsewhere.
 * - `transport` — the stream broke mid-flight, or never became a stream.
 * - `run_error` — the agent itself reported a failure over a healthy stream.
 */
export type AgentErrorCode =
  | 'not_supported'
  | 'unauthorized'
  | 'session_busy'
  | 'cancelled'
  | 'transport'
  | 'run_error'
  | 'internal';

export type AgentError = {
  code: AgentErrorCode;
  message: string;
};

/** How far along one tool call is, as the transcript shows it. */
export type AgentToolCallStatus =
  /** Arguments are still streaming in. */
  | 'streaming'
  /** Fully received and waiting for a result — the interrupt case. */
  | 'pending'
  /** A `TOOL_CALL_RESULT`, or a resume, answered it. */
  | 'done';

/** One tool call, flattened for rendering. */
export type AgentToolCall = {
  id: string;
  name: string;
  /** Raw JSON arguments exactly as streamed. */
  argsText: string;
  /** `argsText` parsed, or `undefined` while it is incomplete or malformed. */
  args: unknown;
  status: AgentToolCallStatus;
  /** The tool result, once one arrived. */
  result?: string;
};

/**
 * One rendered message.
 *
 * `text` is the flattened content: the protocol allows either a string or an
 * array of multimodal parts, and a component should not have to care which.
 * `reasoning` stays a separate field because it is a separate channel on the
 * wire and must never be mistaken for the visible reply.
 */
export type AgentMessage = {
  id: string;
  role: AguiRole;
  text: string;
  /** Reasoning trace attached to this message, when the model emitted one. */
  reasoning?: string;
  toolCalls: AgentToolCall[];
  /** Set on `role: 'tool'`: which call this message answers. */
  toolCallId?: string;
  /** Set on `role: 'activity'`. */
  activityType?: string;
  error?: string;
};

/** The interrupt the UI has to answer before the run can continue. */
export type AgentInterrupt = {
  /** Every call the run is blocked on; usually exactly one. */
  toolCalls: PendingToolCall[];
};
