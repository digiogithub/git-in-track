import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleRetro, sampleTeam } from '@/api/fake-provider';
import { ToastProvider } from '@/components/ui/toast';
import { renderWithRouter } from '@/test/router';

import { RetroCanvas } from './RetroBoard';

/** The sample retro of the sample team, writable unless said so. */
function renderRetro(provider = new FakeProvider({ team: sampleTeam })) {
  renderWithRouter({
    index: () => (
      <ToastProvider>
        <RetroCanvas retroId={sampleRetro.id} />
      </ToastProvider>
    ),
    provider,
  });
  return provider;
}

/** One of the three collection columns, by its heading. */
async function column(label: string): Promise<HTMLElement> {
  const columns = await screen.findByTestId('retro-columns');
  return await waitFor(() => {
    const heading = within(columns).getByText(label);
    const card = heading.closest('div[class*="rounded"]');
    if (!card) throw new Error(`no column ${label}`);
    return card as HTMLElement;
  });
}

describe('the retro board', () => {
  it('renders the three collection columns from the body bullets', async () => {
    renderRetro();

    expect(await within(await column('Went well')).findByText(/Pairing on the OIDC flow/)).
      toBeInTheDocument();
    expect(within(await column('To improve')).getByText(/trailing slash/)).toBeInTheDocument();
    expect(within(await column('Puzzles')).getByText(/stale snapshot badge/)).toBeInTheDocument();
  });

  it('adds a note to the column it was typed into', async () => {
    const user = userEvent.setup();
    renderRetro();

    const field = await screen.findByLabelText('Add a note to To improve');
    await user.type(field, 'Staging credentials expired with no warning');
    await user.click(within(await column('To improve')).getByRole('button', { name: 'Add' }));

    expect(
      await within(await column('To improve')).findByText(/Staging credentials expired/),
    ).toBeInTheDocument();
  });

  it('removes a note', async () => {
    const user = userEvent.setup();
    renderRetro();

    const puzzles = await column('Puzzles');
    await user.click(within(puzzles).getByRole('button', { name: 'Remove' }));

    await waitFor(async () =>
      expect(
        within(await column('Puzzles')).queryByText(/stale snapshot badge/),
      ).not.toBeInTheDocument(),
    );
  });

  it('ranks the themes by the votes they got and casts one', async () => {
    const user = userEvent.setup();
    renderRetro();

    const votes = await screen.findAllByRole('button', { name: /▲$/ });
    expect(votes[0]).toHaveTextContent('2 ▲');

    await user.click(votes[0] as HTMLElement);
    await waitFor(async () =>
      expect((await screen.findAllByRole('button', { name: /▲$/ }))[0]).toHaveTextContent('1 ▲'),
    );
  });

  it('shows the live status of a promoted action rather than the retro status', async () => {
    renderRetro();

    const actions = await screen.findByTestId('retro-actions');
    const promoted = within(actions).getByText('Assert the OIDC redirect URI at startup');
    const row = promoted.closest('li') as HTMLElement;
    // The action is `promoted` in the retro file, but its task is `todo`, so it
    // is still open and its checkbox is not the retro's to tick (R-RETRO-1).
    expect(within(row).getByText(/ACME\/ACME-T-0107/)).toBeInTheDocument();
    expect(within(row).getByRole('checkbox')).toBeDisabled();
    expect(within(row).getByRole('checkbox')).not.toBeChecked();
  });

  it('flags an action nobody owns', async () => {
    renderRetro();

    const actions = await screen.findByTestId('retro-actions');
    const row = within(actions).getByText('Write the staging runbook').closest('li') as HTMLElement;
    expect(within(row).getByText('no owner')).toBeInTheDocument();
  });

  it('adds an improvement action with an owner and a due date', async () => {
    const user = userEvent.setup();
    renderRetro();

    await user.type(await screen.findByLabelText('Action'), 'Alert on snapshot age');
    await user.type(screen.getByLabelText('Owner'), 'jose');
    await user.click(screen.getByRole('button', { name: 'Add action' }));

    const actions = await screen.findByTestId('retro-actions');
    await waitFor(() =>
      expect(within(actions).getByText('Alert on snapshot age')).toBeInTheDocument(),
    );
  });

  it('promotes an action into a task in a chosen project and links it back', async () => {
    const user = userEvent.setup();
    const provider = renderRetro();

    const actions = await screen.findByTestId('retro-actions');
    const row = within(actions).getByText('Write the staging runbook').closest('li') as HTMLElement;
    await user.click(within(row).getByRole('button', { name: 'Promote to task' }));

    await waitFor(async () => {
      const view = await provider.getRetro(sampleRetro.id);
      const action = view.actions.find((entry) => entry.id === 'a3');
      expect(action?.task).toMatch(/^ACME\/ACME-T-/);
      expect(action?.status).toBe('promoted');
    });
    const view = await provider.getRetro(sampleRetro.id);
    const task = view.actions.find((entry) => entry.id === 'a3')?.task ?? '';
    const created = await provider.getItem(task.split('/')[1] ?? '');
    expect(created.body).toContain('Promoted from retro ACME-TEAM-R-0007 (action a3).');
    expect(created.labels).toContain('retro');
  });

  it('offers no promote button for an action that already became a task', async () => {
    renderRetro();

    const actions = await screen.findByTestId('retro-actions');
    const row = within(actions)
      .getByText('Assert the OIDC redirect URI at startup')
      .closest('li') as HTMLElement;
    // An already promoted action offers no promote button at all: the link to
    // the task it became is shown instead.
    expect(within(row).queryByRole('button', { name: 'Promote to task' })).not.toBeInTheDocument();
  });

  it('carries the open actions of the previous retro into a new one', async () => {
    const provider = new FakeProvider({ team: sampleTeam });
    const created = await provider.createRetro({ title: 'Sprint 8 Retrospective' });
    renderWithRouter({
      index: () => (
        <ToastProvider>
          <RetroCanvas retroId={created.retro.retro.id} />
        </ToastProvider>
      ),
      provider,
    });

    const carried = await screen.findByTestId('open-actions');
    expect(within(carried).getByText('Assert the OIDC redirect URI at startup')).toBeInTheDocument();
    expect(within(carried).getByText('Write the staging runbook')).toBeInTheDocument();
    // a2 was done in the room, so it is not carried.
    expect(
      within(carried).queryByText('Split Monday planning into two slots'),
    ).not.toBeInTheDocument();
  });
});
