import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { AgentToolCall } from '@/features/agent/types';
import { ReasoningBlock } from '@/features/agent/ui/ReasoningBlock';
import { RESULT_CLAMP, ToolCallCard } from '@/features/agent/ui/ToolCallCard';

function call(partial: Partial<AgentToolCall> = {}): AgentToolCall {
  return {
    id: 'tc1',
    name: 'search_items',
    argsText: '{"query":"open"}',
    args: { query: 'open' },
    status: 'done',
    ...partial,
  };
}

describe('ToolCallCard', () => {
  it('renders the three states with name and status', () => {
    const { rerender } = render(<ToolCallCard call={call({ status: 'streaming' })} />);
    expect(screen.getByText('search_items')).toBeInTheDocument();
    expect(screen.getByText('Calling')).toBeInTheDocument();

    rerender(<ToolCallCard call={call({ status: 'pending' })} />);
    expect(screen.getByText('Waiting')).toBeInTheDocument();

    rerender(<ToolCallCard call={call({ status: 'done', result: 'two items' })} />);
    expect(screen.getByText('Done')).toBeInTheDocument();
  });

  it('starts collapsed and opens by keyboard', async () => {
    const user = userEvent.setup();
    render(<ToolCallCard call={call({ result: 'two items' })} />);

    const card = screen.getByTestId('tool-call-tc1');
    expect(card).not.toHaveAttribute('open');

    // The disclosure is a native <summary>: it takes focus in tab order and
    // toggles on activation, which is the whole reason it is not a div.
    await user.tab();
    expect(screen.getByText('search_items').closest('summary')).toHaveFocus();

    await user.click(screen.getByText('search_items'));
    expect(card).toHaveAttribute('open');
  });

  it('pretty-prints the arguments as escaped text', () => {
    render(<ToolCallCard call={call({ args: { query: '<img src=x onerror=1>' } })} />);

    const args = screen.getByTestId('tool-args');
    expect(args.textContent).toContain('"query": "<img src=x onerror=1>"');
    expect(args.querySelector('img')).toBeNull();
  });

  it('falls back to the raw stream while the arguments are still partial', () => {
    render(<ToolCallCard call={call({ argsText: '{"quer', args: undefined })} />);

    expect(screen.getByTestId('tool-args').textContent).toContain('{"quer');
  });

  it('clamps a huge result behind "show more"', async () => {
    const user = userEvent.setup();
    const result = 'x'.repeat(RESULT_CLAMP + 500);
    render(<ToolCallCard call={call({ result })} />);

    const more = screen.getByRole('button', { name: /Show more/ });
    expect(screen.getByTestId('tool-result').textContent).toHaveLength(RESULT_CLAMP + 2);

    await user.click(more);
    expect(screen.getByTestId('tool-result').textContent).toHaveLength(result.length);
    expect(screen.getByRole('button', { name: 'Show less' })).toBeInTheDocument();
  });

  it('omits the result section entirely until one arrives', () => {
    const { result: _result, ...pending } = call({ status: 'pending' });
    render(<ToolCallCard call={pending} />);

    expect(screen.queryByTestId('tool-result')).toBeNull();
  });
});

describe('ReasoningBlock', () => {
  it('renders collapsed by default', () => {
    render(<ReasoningBlock reasoning={'Let me look\nat the repo.'} messageId="m1" />);

    const summary = screen.getByLabelText('Reasoning for message m1');
    expect(summary.closest('details')).not.toHaveAttribute('open');
    expect(screen.getByText('2 lines')).toBeInTheDocument();
    expect(screen.getByText(/Let me look/)).toBeInTheDocument();
  });
});
