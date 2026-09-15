import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { AgentMessage } from '@/features/agent/types';
import { MessageBubble } from '@/features/agent/ui/MessageBubble';
import { MessageList } from '@/features/agent/ui/MessageList';

function message(partial: Partial<AgentMessage> & Pick<AgentMessage, 'id' | 'role'>): AgentMessage {
  return { text: '', toolCalls: [], ...partial };
}

describe('MessageBubble', () => {
  it('renders assistant text as Markdown', async () => {
    render(
      <MessageBubble
        message={message({ id: 'a', role: 'assistant', text: '# Title\n\nSome **bold** text.' })}
      />,
    );

    expect(await screen.findByRole('heading', { name: 'Title' })).toBeInTheDocument();
    expect(screen.getByText('bold').tagName).toBe('STRONG');
  });

  it('does not execute raw HTML in agent output', async () => {
    const hostile =
      'Here you go\n\n<img src=x onerror="globalThis.__pwned=true">\n\n<script>1</script>';
    const { container } = render(
      <MessageBubble message={message({ id: 'a', role: 'assistant', text: hostile })} />,
    );

    await screen.findByText(/Here you go/);
    await waitFor(() => {
      expect(container.querySelector('img')).toBeNull();
    });
    expect(container.querySelector('script')).toBeNull();
    expect((globalThis as Record<string, unknown>)['__pwned']).toBeUndefined();
  });

  it('renders a user message as escaped text, never as Markdown', () => {
    render(
      <MessageBubble message={message({ id: 'u', role: 'user', text: '# not a heading *x*' })} />,
    );

    expect(screen.getByText('# not a heading *x*')).toBeInTheDocument();
    expect(screen.queryByRole('heading')).toBeNull();
  });

  it('shows a caret while the message is still streaming, and drops it when it ends', async () => {
    const streaming = message({ id: 'a', role: 'assistant', text: 'Reading ' });
    const { rerender } = render(<MessageBubble message={streaming} streaming />);

    expect(screen.getByTestId('stream-caret')).toBeInTheDocument();
    // The tail is visible as plain text before the throttled parse catches up.
    expect(screen.getByText(/Reading/)).toBeInTheDocument();

    const done = message({ id: 'a', role: 'assistant', text: 'Reading **the files**.' });
    rerender(<MessageBubble message={done} streaming={false} />);

    await waitFor(() => {
      expect(screen.getByText('the files').tagName).toBe('STRONG');
    });
    expect(screen.queryByTestId('stream-caret')).toBeNull();
  });

  it('attaches the reasoning and the tool calls to the message', () => {
    render(
      <MessageBubble
        message={message({
          id: 'a',
          role: 'assistant',
          text: 'Done.',
          reasoning: 'thinking',
          toolCalls: [
            { id: 'tc1', name: 'ls', argsText: '{}', args: {}, status: 'done', result: 'main.go' },
          ],
        })}
      />,
    );

    expect(screen.getByLabelText('Reasoning for message a').closest('details')).not.toHaveAttribute(
      'open',
    );
    expect(screen.getByTestId('tool-call-tc1')).toBeInTheDocument();
  });
});

describe('MessageList', () => {
  it('is a polite live region and hides the channels', async () => {
    render(
      <MessageList
        runStatus="running"
        messages={[
          message({ id: 'sys', role: 'system', text: 'the system prompt' }),
          message({ id: 'u', role: 'user', text: 'hello' }),
          message({ id: 'tool', role: 'tool', text: 'raw result', toolCallId: 'tc1' }),
          message({ id: 'a', role: 'assistant', text: 'hi' }),
        ]}
      />,
    );

    const log = screen.getByRole('log', { name: 'Conversation' });
    expect(log).toHaveAttribute('aria-live', 'polite');
    expect(log).toHaveAttribute('aria-busy', 'true');
    expect(screen.queryByText('the system prompt')).toBeNull();
    expect(screen.queryByText('raw result')).toBeNull();
    // The Markdown pass replaces the plain-text node it first rendered, so the
    // element is re-queried rather than held across the await.
    await waitFor(() => {
      expect(screen.getByText('hi')).toBeInTheDocument();
    });
  });

  it('marks only the last assistant message as streaming', () => {
    render(
      <MessageList
        runStatus="running"
        messages={[
          message({ id: 'a1', role: 'assistant', text: 'first' }),
          message({ id: 'a2', role: 'assistant', text: 'second' }),
        ]}
      />,
    );

    expect(screen.getAllByTestId('stream-caret')).toHaveLength(1);
  });

  it('shows no caret once the run has settled', () => {
    render(
      <MessageList
        runStatus="idle"
        messages={[message({ id: 'a1', role: 'assistant', text: 'first' })]}
      />,
    );

    expect(screen.queryByTestId('stream-caret')).toBeNull();
  });
});
