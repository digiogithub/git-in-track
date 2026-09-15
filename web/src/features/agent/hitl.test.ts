import { PERMISSION_TOOL_NAME, QUESTION_TOOL_NAME } from '@pando-ai/sdk/agui/client';
import type { PendingToolCall } from '@pando-ai/sdk/agui/client';
import { describe, expect, it } from 'vitest';

import {
  buildQuestionAnswer,
  classifyHitl,
  hitlPrompts,
  isHitlToolName,
  parsePermissionRequest,
  parseQuestionRequest,
  permissionSubject,
  refusalFor,
} from '@/features/agent/hitl';

function call(name: string, args: unknown): PendingToolCall {
  return { id: `call-${name}`, name, argsText: JSON.stringify(args ?? null), args };
}

describe('classification', () => {
  it('names the two tools Pando reserves for a human', () => {
    expect(isHitlToolName(PERMISSION_TOOL_NAME)).toBe(true);
    expect(isHitlToolName(QUESTION_TOOL_NAME)).toBe(true);
    expect(isHitlToolName('open_item')).toBe(false);
  });

  it('reads a permission request and exposes its arguments', () => {
    const prompt = classifyHitl(
      call(PERMISSION_TOOL_NAME, {
        toolName: 'write',
        action: 'write',
        path: 'main.go',
        params: { content: 'package main' },
      }),
    );

    expect(prompt).toMatchObject({ kind: 'permission' });
    if (prompt?.kind !== 'permission') throw new Error('not a permission prompt');
    expect(prompt.request.toolName).toBe('write');
    expect(prompt.request.path).toBe('main.go');
    expect(permissionSubject(prompt.request)).toBe('write');
  });

  it('reads a question and normalises its options', () => {
    const prompt = classifyHitl(
      call(QUESTION_TOOL_NAME, {
        questions: [
          {
            question: 'Which branch?',
            header: 'Branch',
            multiSelect: true,
            options: [{ label: 'main' }, { description: 'no label' }, 'nonsense'],
          },
        ],
      }),
    );

    if (prompt?.kind !== 'question') throw new Error('not a question prompt');
    expect(prompt.questions).toHaveLength(1);
    expect(prompt.questions[0]?.options).toEqual([{ label: 'main', description: '' }]);
    expect(prompt.questions[0]?.multiSelect).toBe(true);
  });

  it('falls back to the question text when no header was sent', () => {
    const questions = parseQuestionRequest({ questions: [{ question: 'Where?' }] });
    expect(questions?.[0]?.header).toBe('Where?');
  });

  it('leaves a frontend tool alone', () => {
    expect(classifyHitl(call('open_item', { id: 'GIT-US-0061' }))).toBeNull();
  });

  it('collects every prompt a run is parked on', () => {
    const prompts = hitlPrompts([
      call('open_item', { id: 'GIT-US-0061' }),
      call(PERMISSION_TOOL_NAME, { toolName: 'bash' }),
    ]);
    expect(prompts.map((prompt) => prompt.kind)).toEqual(['permission']);
  });
});

describe('a malformed payload is a refusal, never an error', () => {
  it('refuses a permission request with no readable tool name', () => {
    const prompt = classifyHitl({
      id: 'p1',
      name: PERMISSION_TOOL_NAME,
      argsText: '{"toolNa',
      args: undefined,
    });

    expect(prompt).toMatchObject({ kind: 'malformed', name: PERMISSION_TOOL_NAME });
    expect(prompt === null ? '' : refusalFor(prompt)).toBe(JSON.stringify({ approved: false }));
  });

  it('cancels a question that carries nothing askable', () => {
    const prompt = classifyHitl(call(QUESTION_TOOL_NAME, { questions: [] }));

    expect(prompt).toMatchObject({ kind: 'malformed', name: QUESTION_TOOL_NAME });
    expect(prompt === null ? '' : refusalFor(prompt)).toBe(
      JSON.stringify({ cancelled: true, answers: [] }),
    );
  });

  it('reads nothing out of a payload that is not an object', () => {
    expect(parsePermissionRequest('write')).toBeUndefined();
    expect(parsePermissionRequest({ toolName: '  ' })).toBeUndefined();
    expect(parseQuestionRequest({ questions: 'one' })).toBeUndefined();
  });
});

describe('the answer payload', () => {
  it('carries the header, the selection and the free text Pando reads', () => {
    const questions = parseQuestionRequest({
      questions: [
        { question: 'Which branch?', header: 'Branch', options: [{ label: 'main' }] },
        { question: 'Anything else?', header: 'Notes' },
      ],
    });
    if (questions === undefined) throw new Error('no questions');

    const raw = buildQuestionAnswer(questions, [
      { selected: ['main'], otherText: '' },
      { selected: [], otherText: '  be careful  ' },
    ]);

    expect(JSON.parse(raw)).toEqual({
      answers: [
        { questionId: '0', header: 'Branch', selected: ['main'] },
        { questionId: '1', header: 'Notes', selected: [], otherText: 'be careful' },
      ],
    });
  });
});
