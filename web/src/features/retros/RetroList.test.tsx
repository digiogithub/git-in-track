import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleSprint, sampleTeam } from '@/api/fake-provider';
import { ToastProvider } from '@/components/ui/toast';
import { renderWithRouter } from '@/test/router';

import { RetroList } from './RetroList';

function renderList(provider = new FakeProvider({ team: sampleTeam })) {
  renderWithRouter({
    index: () => (
      <ToastProvider>
        <RetroList />
      </ToastProvider>
    ),
    provider,
  });
  return provider;
}

describe('the retro index', () => {
  it('lists past retros with how many actions they left open', async () => {
    renderList();

    expect(await screen.findByText('Sprint 7 Retrospective')).toBeInTheDocument();
    expect(await screen.findByText(/2 of 3 actions still open/)).toBeInTheDocument();
    expect(screen.getByText(/1 without an owner/)).toBeInTheDocument();
  });

  it('shows the still-open improvement actions above the list', async () => {
    renderList();

    const open = await screen.findByTestId('open-actions');
    expect(
      await within(open).findByText('Assert the OIDC redirect URI at startup'),
    ).toBeInTheDocument();
    expect(within(open).getByText('Write the staging runbook')).toBeInTheDocument();
    expect(
      within(open).queryByText('Split Monday planning into two slots'),
    ).not.toBeInTheDocument();
  });

  it('offers a retro for a closed sprint that has none', async () => {
    const user = userEvent.setup();
    const provider = renderList(
      new FakeProvider({
        team: sampleTeam,
        sprints: [{ ...sampleSprint, id: 'ACME-TEAM-S-0006', state: 'closed', title: 'Sprint 6' }],
      }),
    );

    await user.click(await screen.findByRole('button', { name: 'Retro for Sprint 6' }));

    await waitFor(async () => {
      const listing = await provider.listRetros();
      expect(listing.retros.some((retro) => retro.sprint === 'ACME-TEAM-S-0006')).toBe(true);
    });
  });

  it('refuses a second retro for the same sprint and says why', async () => {
    const user = userEvent.setup();
    renderList(
      new FakeProvider({
        team: sampleTeam,
        sprints: [{ ...sampleSprint, state: 'closed' }],
      }),
    );

    // The sprint already has `sampleRetro`, so it is not offered at all.
    expect(
      screen.queryByRole('button', { name: /Retro for Sprint 7/ }),
    ).not.toBeInTheDocument();

    await user.click(await screen.findByRole('button', { name: 'Retro without a sprint' }));
    expect(await screen.findByText('Sprint 7 Retrospective')).toBeInTheDocument();
  });
});
