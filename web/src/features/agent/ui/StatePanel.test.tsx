import { applyJsonPatch } from '@pando-ai/sdk/agui/client';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { PandoState } from '@/features/agent/types';
import { StatePanel } from '@/features/agent/ui/StatePanel';

const fixture: PandoState = {
  thread: 'thread-1',
  session: 'session-1',
  agent: 'coder',
  model: { id: 'claude', name: 'Claude', provider: 'anthropic', contextWindow: 200_000 },
  todos: [
    { content: 'Read the repo', status: 'completed', priority: 'high' },
    { content: 'Write the panel', status: 'in_progress', priority: 'medium' },
  ],
  tokenUsage: {
    promptTokens: 12_000,
    completionTokens: 3_000,
    contextWindow: 200_000,
    estimated: false,
  },
  files: [{ path: 'web/src/main.tsx', name: 'main.tsx', action: 'edit' }],
  subAgents: [{ id: 'task-1', status: 'running', role: 'worker', summary: 'Indexing' }],
};

describe('the shared-state panel', () => {
  it('renders every section of a snapshot', () => {
    render(<StatePanel state={fixture} />);

    expect(screen.getByText('Claude')).toBeInTheDocument();
    expect(screen.getByText('Read the repo')).toBeInTheDocument();
    expect(screen.getByText('Write the panel')).toBeInTheDocument();
    expect(screen.getByText('main.tsx')).toBeInTheDocument();
    expect(screen.getByText('worker')).toBeInTheDocument();

    const meter = screen.getByRole('progressbar', { name: 'Context window used' });
    expect(meter).toHaveAttribute('aria-valuenow', '15000');
    expect(meter).toHaveAttribute('aria-valuemax', '200000');
  });

  it('follows a STATE_DELTA, because the document is the thread’s', () => {
    const { rerender } = render(<StatePanel state={fixture} />);
    expect(screen.getByText('doing')).toBeInTheDocument();

    // Exactly the operation `internal/agui/state.go` emits.
    const patched = applyJsonPatch(fixture, [
      { op: 'replace', path: '/todos/1/status', value: 'completed' },
      { op: 'add', path: '/files/-', value: { path: 'a.go', name: 'a.go', action: 'read' } },
    ]);
    rerender(<StatePanel state={patched} />);

    expect(screen.queryByText('doing')).toBeNull();
    expect(screen.getAllByText('done')).toHaveLength(2);
    expect(screen.getByText('a.go')).toBeInTheDocument();
  });

  it('keeps its contents across a new run', () => {
    const { rerender } = render(<StatePanel state={fixture} />);
    // A new turn does not clear the document; the store re-renders with the
    // same object until a fresh snapshot arrives.
    rerender(<StatePanel state={fixture} />);
    expect(screen.getByText('Read the repo')).toBeInTheDocument();
  });

  it('omits empty sections rather than showing hollow headings', () => {
    render(
      <StatePanel state={{ ...fixture, todos: [], files: [], subAgents: [], tokenUsage: null }} />,
    );

    expect(screen.queryByText('Plan')).toBeNull();
    expect(screen.queryByText('Files')).toBeNull();
    expect(screen.queryByText('Sub-agents')).toBeNull();
    expect(screen.queryByRole('progressbar')).toBeNull();
    expect(screen.getByText('Claude')).toBeInTheDocument();
  });

  it('explains itself before the first snapshot', () => {
    render(<StatePanel state={undefined} />);
    expect(screen.getByText(/plan, files and token usage will appear here/)).toBeInTheDocument();
  });

  it('shows standing approvals as revocable badges', async () => {
    const user = userEvent.setup();
    const onRevoke = vi.fn();
    render(<StatePanel state={fixture} alwaysAllowed={['write']} onRevoke={onRevoke} />);

    expect(screen.getByText('write')).toBeInTheDocument();
    await user.click(
      screen.getByRole('button', { name: 'Revoke the standing approval for write' }),
    );
    expect(onRevoke).toHaveBeenCalledWith('write');
  });
});
