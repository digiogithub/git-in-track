import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { Composer } from '@/features/agent/ui/Composer';

function setup(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const onSend = vi.fn();
  const onStop = vi.fn();
  render(<Composer onSend={onSend} onStop={onStop} running={false} {...props} />);
  return { onSend, onStop, field: screen.getByRole('textbox', { name: 'Message the agent' }) };
}

describe('Composer', () => {
  it('sends on Enter and breaks the line on Shift+Enter', async () => {
    const user = userEvent.setup();
    const { onSend, field } = setup();

    await user.type(field, 'first{Shift>}{Enter}{/Shift}second');
    expect(onSend).not.toHaveBeenCalled();
    expect(field).toHaveValue('first\nsecond');

    await user.type(field, '{Enter}');
    expect(onSend).toHaveBeenCalledWith('first\nsecond');
    expect(field).toHaveValue('');
  });

  it('refuses to send whitespace', async () => {
    const user = userEvent.setup();
    const { onSend, field } = setup();

    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
    await user.type(field, '   {Enter}');
    expect(onSend).not.toHaveBeenCalled();
  });

  it('replaces send with stop during a run and keeps the draft', async () => {
    const user = userEvent.setup();
    const { onStop, onSend, field } = setup({ running: true });

    await user.type(field, 'half a thought');
    expect(screen.queryByRole('button', { name: 'Send' })).toBeNull();

    await user.click(screen.getByRole('button', { name: 'Stop the run' }));
    expect(onStop).toHaveBeenCalledTimes(1);
    expect(onSend).not.toHaveBeenCalled();
    expect(field).toHaveValue('half a thought');
  });

  it('blocks sending while the run waits for an answer, and says why', async () => {
    const user = userEvent.setup();
    const { onSend, field } = setup({ interrupted: true });

    await user.type(field, 'never mind{Enter}');
    expect(onSend).not.toHaveBeenCalled();
    expect(screen.getByRole('status')).toHaveTextContent(/waiting for an answer/i);
  });

  it('disables the field entirely when another tab owns the conversation', () => {
    const { field } = setup({ readOnly: true });

    expect(field).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
    expect(screen.getByRole('status')).toHaveTextContent(/Another tab owns this conversation/i);
  });

  it('describes the keyboard contract in the field', () => {
    const { field } = setup();

    expect(field).toHaveAccessibleDescription('Enter sends, Shift+Enter starts a new line.');
  });
});
