import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, type FakeData } from '@/api/fake-provider';
import { ProviderContext } from '@/api/provider-context';
import { McpToolsCard } from '@/features/settings/McpToolsCard';

function renderCard(data: FakeData = {}) {
  const provider = new FakeProvider(data);
  render(
    <ProviderContext.Provider value={provider}>
      <McpToolsCard />
    </ProviderContext.Provider>,
  );
  return provider;
}

const SWITCH = { name: 'Let agents create and edit items' };

describe('McpToolsCard', () => {
  it('starts read-only, with the write tools off', async () => {
    renderCard();

    expect(await screen.findByText('Read-only')).toBeInTheDocument();
    expect(screen.getByRole('switch', SWITCH)).not.toBeChecked();
    expect(screen.queryByText(/can now edit the backlog/)).toBeNull();
  });

  it('hides itself on a runtime with no MCP surface', async () => {
    const provider = renderCard({ mcp: { supported: false } });

    // Let the mount read settle inside act, so the absence below is a decision
    // the card made and not a render that has not happened yet.
    await act(async () => {
      await provider.getMcpSettings();
    });

    expect(screen.queryByRole('switch', SWITCH)).toBeNull();
  });

  it('turns the write tools on, persists the choice and says so', async () => {
    const provider = renderCard();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('switch', SWITCH));

    await waitFor(() => {
      expect(screen.getByRole('switch', SWITCH)).toBeChecked();
    });
    expect(await provider.getMcpSettings()).toMatchObject({ allowWrite: true, persisted: true });
    expect(screen.getByText('Read and write')).toBeInTheDocument();
    // The grant is stated plainly, and so is what an agent has to do to see it.
    expect(screen.getByText(/can now edit the backlog/)).toBeInTheDocument();
    expect(screen.getByText(/when that server restarts/)).toBeInTheDocument();
  });

  it('says when the change cannot outlive the process', async () => {
    renderCard({ mcp: { allowWrite: true, configPath: '' } });

    expect(await screen.findByText(/applies to this process only/)).toBeInTheDocument();
  });
});
