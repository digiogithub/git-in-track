import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import {
  buildQuestionAnswer,
  type QuestionPrompt,
  type QuestionSelection,
} from '@/features/agent/hitl';
import { QuestionDialog } from '@/features/agent/ui/QuestionDialog';

const withChoices: QuestionPrompt = {
  kind: 'question',
  toolCallId: 'q-1',
  questions: [
    {
      question: 'Which branch should I work on?',
      header: 'Branch',
      multiSelect: false,
      options: [
        { label: 'main', description: 'the trunk' },
        { label: 'develop', description: '' },
      ],
    },
  ],
};

const openEnded: QuestionPrompt = {
  kind: 'question',
  toolCallId: 'q-2',
  questions: [
    { question: 'What should the release note say?', header: 'Release note', options: [] },
  ],
};

function setup(prompt: QuestionPrompt) {
  const onAnswer = vi.fn<(selections: QuestionSelection[]) => void>();
  const onCancel = vi.fn();
  render(<QuestionDialog prompt={prompt} onAnswer={onAnswer} onCancel={onCancel} />);
  return { onAnswer, onCancel, user: userEvent.setup() };
}

describe('the question dialog', () => {
  it('renders the offered choices', async () => {
    const { onAnswer, user } = setup(withChoices);

    expect(screen.getByText('Which branch should I work on?')).toBeInTheDocument();
    await user.click(screen.getByRole('checkbox', { name: /main/ }));
    await user.click(screen.getByRole('button', { name: 'Send the answer' }));

    expect(onAnswer).toHaveBeenCalledWith([{ selected: ['main'], otherText: '' }]);
    const sent = onAnswer.mock.calls[0]?.[0] ?? [];
    expect(JSON.parse(buildQuestionAnswer(withChoices.questions, sent))).toEqual({
      answers: [{ questionId: '0', header: 'Branch', selected: ['main'] }],
    });
  });

  it('keeps a single-select question single', async () => {
    const { onAnswer, user } = setup(withChoices);

    await user.click(screen.getByRole('checkbox', { name: /main/ }));
    await user.click(screen.getByRole('checkbox', { name: /develop/ }));
    await user.click(screen.getByRole('button', { name: 'Send the answer' }));

    expect(onAnswer).toHaveBeenCalledWith([{ selected: ['develop'], otherText: '' }]);
  });

  it('falls back to a text field when nothing was offered', async () => {
    const { onAnswer, user } = setup(openEnded);

    expect(screen.queryByRole('checkbox')).toBeNull();
    await user.type(screen.getByRole('textbox', { name: 'Answer: Release note' }), 'Ship it');
    await user.click(screen.getByRole('button', { name: 'Send the answer' }));

    expect(onAnswer).toHaveBeenCalledWith([{ selected: [], otherText: 'Ship it' }]);
  });

  it('will not send an empty answer', () => {
    setup(openEnded);
    expect(screen.getByRole('button', { name: 'Send the answer' })).toBeDisabled();
  });

  it('turns Escape into a confirmed cancellation', async () => {
    const { onCancel, user } = setup(openEnded);

    await user.keyboard('{Escape}');

    expect(onCancel).not.toHaveBeenCalled();
    const confirm = screen.getByRole('alertdialog', { name: 'Confirm the cancellation' });
    await user.click(within(confirm).getByRole('button', { name: 'Cancel the question' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('cancels from the footer, through the same confirmation', async () => {
    const { onCancel, user } = setup(openEnded);

    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    await user.click(screen.getByRole('button', { name: 'Cancel the question' }));

    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
