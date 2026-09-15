import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import type { FakeAgent } from '@/api/fake-provider';
import { FakeProvider } from '@/api/fake-provider';
import { useAppStore } from '@/app/store';
import { permissionInterrupt, permissionResumed, simpleTurn } from '@/features/agent/fixtures';
import { useAgentStore } from '@/features/agent/store';
import { AgentPage } from '@/features/agent/ui/AgentPage';
import { THREAD_META_KEY } from '@/features/agent/ui/threadMeta';
import { renderWithRouter } from '@/test/router';

/** Renders the page with the capability snapshot the shell would have set. */
function renderAgent(agent?: FakeAgent) {
  const provider = new FakeProvider(agent === undefined ? {} : { agent });
  useAppStore.getState().setCapabilities(provider.capabilities);
  renderWithRouter({ index: AgentPage, provider });
  return provider;
}

function composer(): HTMLElement {
  return screen.getByRole('textbox', { name: 'Message the agent' });
}

/** The transcript, so a prompt is not confused with its own thread-list row. */
function transcript(): HTMLElement {
  return screen.getByRole('log', { name: 'Conversation' });
}

/** The router mounts asynchronously and the repository list is a query. */
async function ready(): Promise<void> {
  await screen.findByRole('textbox', { name: 'Message the agent' });
  await waitFor(() => {
    expect(composer()).toBeEnabled();
  });
}

beforeEach(() => {
  globalThis.localStorage.clear();
  useAppStore.getState().reset();
  useAgentStore.getState().dispose();
});

afterEach(() => {
  useAgentStore.getState().dispose();
});

describe('the capability gate', () => {
  it('explains itself when the runtime has no agent', async () => {
    renderAgent();

    expect(await screen.findByText('The agent is not available here')).toBeInTheDocument();
    expect(screen.queryByRole('textbox', { name: 'Message the agent' })).toBeNull();
  });

  it('renders the three columns when the runtime has one', async () => {
    renderAgent({ events: simpleTurn });

    expect(await screen.findByRole('navigation', { name: 'Conversations' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'Agent conversation' })).toBeInTheDocument();
    expect(screen.getByRole('complementary', { name: 'Agent state' })).toBeInTheDocument();
    expect(await screen.findByText('Ask about this workspace')).toBeInTheDocument();
  });
});

describe('a turn', () => {
  it('renders the reply as Markdown with its tool call, and titles the thread', async () => {
    const user = userEvent.setup();
    const provider = renderAgent({ events: simpleTurn });

    await ready();
    await user.type(composer(), 'What is in the repo?{Enter}');

    // The prompt the store posted, echoed back as an escaped user message.
    await waitFor(() => {
      expect(within(transcript()).getByText('What is in the repo?')).toBeInTheDocument();
      expect(within(transcript()).getByText('There is one file.')).toBeInTheDocument();
    });
    expect(provider.agentRuns).toHaveLength(1);

    // The tool call is a card, collapsed, and its result is not loose in the list.
    const card = await screen.findByTestId('tool-call-tc1');
    expect(card).not.toHaveAttribute('open');
    await user.click(screen.getByText('ls'));
    expect(card).toHaveAttribute('open');
    expect(screen.getByTestId('tool-result')).toHaveTextContent('main.go');

    // Reasoning came in on its own channel and stays collapsed.
    const reasoning = screen.getByLabelText('Reasoning for message m1');
    expect(reasoning.closest('details')).not.toHaveAttribute('open');

    // The title is derived from the first user message and survives a reload.
    await waitFor(() => {
      expect(globalThis.localStorage.getItem(THREAD_META_KEY)).toContain('What is in the repo?');
    });
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('What is in the repo?');
  });

  it('stops the run and tells the adapter to cancel it', async () => {
    const user = userEvent.setup();
    const provider = renderAgent({ events: simpleTurn });

    await ready();
    const threadId = useAgentStore.getState().threadId;
    // A scripted stream settles in microtasks, so the running state is staged
    // rather than raced for; what is under test is the wiring of the button.
    act(() => {
      useAgentStore.setState({ runStatus: 'running' });
    });

    await user.click(screen.getByRole('button', { name: 'Stop the run' }));

    await waitFor(() => {
      expect(provider.agentCancels).toEqual([threadId]);
    });
    expect(useAgentStore.getState().runStatus).toBe('cancelled');
    expect(screen.getByRole('button', { name: 'Send' })).toBeInTheDocument();
  });

  it('shows a dismissible banner when the run fails', async () => {
    const user = userEvent.setup();
    renderAgent({
      events: simpleTurn,
      runError: { code: 'internal', message: 'The adapter fell over.' },
    });

    await ready();
    await user.type(composer(), 'hello{Enter}');

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('The adapter fell over.');

    await user.click(screen.getByRole('button', { name: 'Dismiss the error' }));
    expect(screen.queryByRole('alert')).toBeNull();
  });
});

describe('the conversation list', () => {
  it('starts a new conversation and switches back to a stored one', async () => {
    const user = userEvent.setup();
    renderAgent({ events: simpleTurn });

    await ready();
    await user.type(composer(), 'first question{Enter}');
    await waitFor(() => {
      expect(within(transcript()).getByText('first question')).toBeInTheDocument();
    });
    const first = useAgentStore.getState().threadId;

    await user.click(screen.getByRole('button', { name: 'New conversation' }));
    await waitFor(() => {
      expect(useAgentStore.getState().threadId).not.toBe(first);
    });
    expect(await screen.findByText('Ask about this workspace')).toBeInTheDocument();

    // The old conversation is still listed, under its derived title.
    const row = screen.getByRole('button', { name: 'first question' });
    await user.click(row);
    await waitFor(() => {
      expect(useAgentStore.getState().threadId).toBe(first);
    });
  });

  it('deletes a conversation after a confirmation and forgets its title', async () => {
    const user = userEvent.setup();
    const provider = renderAgent({ events: simpleTurn });

    await ready();
    await user.type(composer(), 'doomed question{Enter}');
    await waitFor(() => {
      expect(within(transcript()).getByText('doomed question')).toBeInTheDocument();
    });
    const threadId = useAgentStore.getState().threadId;

    await user.click(screen.getByRole('button', { name: 'Delete doomed question' }));
    await user.click(screen.getByRole('button', { name: 'Confirm deleting doomed question' }));

    await waitFor(() => {
      expect(provider.agentDeletes).toEqual([threadId]);
    });
    await waitFor(() => {
      expect(globalThis.localStorage.getItem(THREAD_META_KEY)).not.toContain('doomed question');
    });
  });
});

describe('restoring a conversation on mount', () => {
  it('hydrates a known thread instead of opening a stream for it', async () => {
    globalThis.localStorage.setItem('gintrack:agent-active-thread:repo-1', 'thread-known');
    const provider = renderAgent({
      events: simpleTurn,
      threads: [{ id: 'thread-known', updatedAt: '2026-09-15T10:00:00Z' }],
      messages: {
        'thread-known': [
          { id: 'h1', role: 'user', content: 'what did we decide?' },
          { id: 'h2', role: 'assistant', content: 'we decided to ship.' },
        ],
      },
    });

    await waitFor(() => {
      expect(screen.getByText('we decided to ship.')).toBeInTheDocument();
    });
    // Hydration is a read, not a run: nothing was posted to start one.
    expect(provider.agentRuns).toHaveLength(0);
    expect(useAgentStore.getState().runStatus).toBe('idle');
  });
});

describe('the interrupt dialogs', () => {
  it('opens the permission dialog and resumes the run on approval', async () => {
    const user = userEvent.setup();
    const provider = renderAgent({ turns: [permissionInterrupt, permissionResumed] });

    await ready();
    await user.type(composer(), 'write a file{Enter}');

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('main.go')).toBeInTheDocument();

    await user.click(within(dialog).getByRole('button', { name: 'Approve once' }));

    await waitFor(() => {
      expect(provider.agentRuns).toHaveLength(2);
    });
    expect(provider.agentRuns[1]?.messages?.at(-1)).toMatchObject({
      role: 'tool',
      content: '{"approved":true}',
    });
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
  });

  it('records an always-allow grant as a revocable badge in the rail', async () => {
    const user = userEvent.setup();
    renderAgent({ turns: [permissionInterrupt, permissionResumed] });

    await ready();
    await user.type(composer(), 'write a file{Enter}');

    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('switch', { name: 'Always allow' }));
    await user.click(within(dialog).getByRole('button', { name: 'Always allow' }));

    await waitFor(() => {
      expect(useAgentStore.getState().alwaysAllowed).toEqual(['write']);
    });
    const rail = screen.getByRole('complementary', { name: 'Agent state' });
    expect(within(rail).getByText('write')).toBeInTheDocument();

    await user.click(
      within(rail).getByRole('button', { name: 'Revoke the standing approval for write' }),
    );
    expect(useAgentStore.getState().alwaysAllowed).toEqual([]);
  });
});
