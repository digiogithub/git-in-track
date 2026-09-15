import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { AgentToolCall } from '@/features/agent/types';
import { AgentToolResult } from '@/features/agent/ui/ItemCards';
import { renderWithRouter } from '@/test/router';

function call(result: unknown, name = 'show_items'): AgentToolCall {
  return {
    id: 'ft-1',
    name,
    argsText: '{}',
    args: {},
    status: 'done',
    result: typeof result === 'string' ? result : JSON.stringify(result),
  };
}

function renderResult(toolCall: AgentToolCall) {
  renderWithRouter({
    index: () => <AgentToolResult call={toolCall} fallbackProject="GIT" />,
  });
}

describe('show_items cards', () => {
  it('renders one card per resolved item, from the tool result alone', async () => {
    renderResult(
      call({
        items: [
          { id: 'GIT-US-0061', type: 'story', title: 'Dialogs', status: 'todo', priority: 'high' },
          { id: 'GIT-T-0070', type: 'task', title: 'Permission dialog', status: 'in_progress' },
        ],
        unresolved: [],
      }),
    );

    expect(await screen.findByLabelText('GIT-US-0061 Dialogs')).toBeInTheDocument();
    expect(screen.getByLabelText('GIT-T-0070 Permission dialog')).toBeInTheDocument();
    expect(screen.getByText('Story')).toBeInTheDocument();
    expect(screen.getByText('Task')).toBeInTheDocument();
    expect(screen.getByText('high')).toBeInTheDocument();
    // The ids are links into the app, not bare text.
    expect(screen.getByRole('link', { name: 'GIT-US-0061' })).toHaveAttribute(
      'href',
      '/p/GIT/items/GIT-US-0061',
    );
  });

  it('shows the ids it could not resolve as a muted note', async () => {
    renderResult(
      call({
        items: [{ id: 'GIT-US-0061', type: 'story', title: 'Dialogs', status: 'todo' }],
        unresolved: ['GIT-US-9999'],
      }),
    );

    expect(await screen.findByText(/Not in this workspace/)).toBeInTheDocument();
    expect(screen.getByText('GIT-US-9999')).toBeInTheDocument();
  });

  it('renders nothing for another tool, or for a result it cannot read', () => {
    renderResult(call({ items: [] }, 'open_item'));
    expect(screen.queryByTestId('agent-item-cards')).toBeNull();

    renderResult(call('not json at all'));
    expect(screen.queryByTestId('agent-item-cards')).toBeNull();
  });
});
