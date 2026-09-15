/**
 * Human-in-the-loop classification (task GIT-T-0067).
 *
 * Pando parks a run on two synthetic tool calls: `pando_permission_request`
 * and `AskUserQuestion` (`internal/agui/hitl.go`). Both arrive as ordinary
 * pending tool calls, so something has to decide, per call, whether the answer
 * is a dialog, a frontend tool, or nothing this client understands.
 *
 * The SDK's `isPermissionRequest` / `isQuestionRequest` narrow on the tool
 * *name* only — their return type claims a parsed `args`, but the wire may
 * carry anything, and a model that streams half an argument object leaves
 * `args` as `undefined`. So the shape is re-checked here and a call that fails
 * the check becomes a {@link MalformedPrompt}: something the UI can only
 * refuse. That is the safe direction and the one Pando already assumes —
 * `approvalFromMessage` (`hitl.go:145`) denies anything that is not an
 * explicit approval, and an unanswered question cancels the turn silently
 * (`questionCancelled`, `hitl.go:222`). A prompt we cannot render must
 * therefore be refused *loudly*, not dropped.
 */

import {
  PERMISSION_TOOL_NAME,
  QUESTION_TOOL_NAME,
  answerQuestion,
  cancelQuestion,
  deny,
  isPermissionRequest,
  isQuestionRequest,
} from '@pando-ai/sdk/agui/client';
import type {
  PandoPermissionRequest,
  PandoQuestion,
  PandoQuestionAnswer,
  PandoQuestionOption,
  PendingToolCall,
} from '@pando-ai/sdk/agui/client';

export { PERMISSION_TOOL_NAME, QUESTION_TOOL_NAME };

/** A parked permission prompt, with arguments known to be readable. */
export type PermissionPrompt = {
  kind: 'permission';
  toolCallId: string;
  request: PandoPermissionRequest;
};

/** A parked `AskUserQuestion` call, with at least one readable question. */
export type QuestionPrompt = {
  kind: 'question';
  toolCallId: string;
  questions: PandoQuestion[];
};

/**
 * A HITL call whose payload could not be read. It is still a prompt — the run
 * is parked on it — but the only honest answer is a refusal.
 */
export type MalformedPrompt = {
  kind: 'malformed';
  toolCallId: string;
  name: string;
  reason: string;
};

export type HitlPrompt = PermissionPrompt | QuestionPrompt | MalformedPrompt;

/** True for the two tool names Pando reserves for human-in-the-loop prompts. */
export function isHitlToolName(name: string): boolean {
  return name === PERMISSION_TOOL_NAME || name === QUESTION_TOOL_NAME;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function text(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value : undefined;
}

/** Reads a permission payload, or `undefined` when it is not one. */
export function parsePermissionRequest(args: unknown): PandoPermissionRequest | undefined {
  if (!isRecord(args)) return undefined;
  const toolName = text(args['toolName']);
  if (toolName === undefined) return undefined;
  return {
    toolName,
    action: text(args['action']) ?? '',
    ...(text(args['description']) === undefined
      ? {}
      : { description: args['description'] as string }),
    ...(text(args['path']) === undefined ? {} : { path: args['path'] as string }),
    ...(args['params'] === undefined ? {} : { params: args['params'] }),
  };
}

function parseOptions(value: unknown): PandoQuestionOption[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry): PandoQuestionOption[] => {
    if (!isRecord(entry)) return [];
    const label = text(entry['label']);
    if (label === undefined) return [];
    return [{ label, description: text(entry['description']) ?? '' }];
  });
}

/** Reads an `AskUserQuestion` payload, or `undefined` when it carries nothing askable. */
export function parseQuestionRequest(args: unknown): PandoQuestion[] | undefined {
  if (!isRecord(args)) return undefined;
  const raw = args['questions'];
  if (!Array.isArray(raw)) return undefined;
  const questions = raw.flatMap((entry): PandoQuestion[] => {
    if (!isRecord(entry)) return [];
    const question = text(entry['question']);
    if (question === undefined) return [];
    return [
      {
        question,
        header: text(entry['header']) ?? question,
        multiSelect: entry['multiSelect'] === true,
        options: parseOptions(entry['options']),
      },
    ];
  });
  return questions.length > 0 ? questions : undefined;
}

/**
 * Classifies one parked call. `null` means it is not a HITL prompt at all —
 * a frontend tool, or a name this client has never heard of.
 */
export function classifyHitl(call: PendingToolCall): HitlPrompt | null {
  if (isPermissionRequest(call)) {
    const request = parsePermissionRequest(call.args);
    if (request === undefined) {
      return {
        kind: 'malformed',
        toolCallId: call.id,
        name: call.name,
        reason: 'The permission request did not carry a readable tool name.',
      };
    }
    return { kind: 'permission', toolCallId: call.id, request };
  }
  if (isQuestionRequest(call)) {
    const questions = parseQuestionRequest(call.args);
    if (questions === undefined) {
      return {
        kind: 'malformed',
        toolCallId: call.id,
        name: call.name,
        reason: 'The question did not carry anything askable.',
      };
    }
    return { kind: 'question', toolCallId: call.id, questions };
  }
  return null;
}

/** Every HITL prompt among the calls a run is parked on, in arrival order. */
export function hitlPrompts(calls: readonly PendingToolCall[]): HitlPrompt[] {
  return calls.flatMap((call) => {
    const prompt = classifyHitl(call);
    return prompt === null ? [] : [prompt];
  });
}

/**
 * The refusal for a prompt: a denial for a permission, a cancellation for a
 * question. Dismissing a dialog, failing to render one, and giving up all
 * resolve to this — never to silence.
 */
export function refusalFor(prompt: HitlPrompt): string {
  if (prompt.kind === 'question') return cancelQuestion();
  if (prompt.kind === 'malformed' && prompt.name === QUESTION_TOOL_NAME) return cancelQuestion();
  return deny();
}

/** The tool name an "always allow" grant is keyed on. */
export function permissionSubject(request: PandoPermissionRequest): string {
  return request.toolName;
}

/** One answer being composed by the question dialog. */
export type QuestionSelection = {
  selected: string[];
  otherText: string;
};

/**
 * Builds the wire answer for a set of questions.
 *
 * `questionId` is the index, and `header` carries the human label: Pando
 * prefers the header and falls back to the id when rendering the answer for
 * the model (`answerFromMessage`, `hitl.go:262-277`).
 */
export function buildQuestionAnswer(
  questions: readonly PandoQuestion[],
  selections: readonly QuestionSelection[],
): string {
  const answer: PandoQuestionAnswer = {
    answers: questions.map((question, index) => {
      const selection = selections[index] ?? { selected: [], otherText: '' };
      const otherText = selection.otherText.trim();
      return {
        questionId: String(index),
        header: question.header,
        selected: selection.selected,
        ...(otherText === '' ? {} : { otherText }),
      };
    }),
  };
  return answerQuestion(answer);
}
