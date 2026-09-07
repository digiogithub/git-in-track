import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import { FakeProvider, sampleRetro, sampleTeam } from '@/api/fake-provider';
import { ToastProvider } from '@/components/ui/toast';
import { resetIdentityCache } from '@/features/workspace/identity';
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

/**
 * Names this browser, which is what the retro asks for before it lets anybody
 * write: there are no accounts, only the handle a participant chooses (ADR-028).
 */
async function identify(user: ReturnType<typeof userEvent.setup>, name = 'Ada Lovelace') {
  await user.type(await screen.findByLabelText('Your name'), name);
  await user.click(screen.getByRole('button', { name: 'Join the retro' }));
  return await screen.findByText(/Writing as/);
}

/** Moves the session to a facilitation stage, the way the facilitator does. */
async function setStage(user: ReturnType<typeof userEvent.setup>, stage: string) {
  await user.selectOptions(await screen.findByLabelText('Stage'), stage);
  await waitFor(() => expect(screen.getByLabelText('Stage')).toHaveValue(stage));
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
  beforeEach(() => {
    globalThis.localStorage?.clear();
    resetIdentityCache();
  });

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
    await identify(user);
    await setStage(user, 'collecting');

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
    await identify(user);
    await setStage(user, 'collecting');

    const puzzles = await column('Puzzles');
    await user.click(within(puzzles).getByRole('button', { name: 'Remove' }));

    await waitFor(async () =>
      expect(
        within(await column('Puzzles')).queryByText(/stale snapshot badge/),
      ).not.toBeInTheDocument(),
    );
  });

  it('votes on a note, and takes the vote back', async () => {
    const user = userEvent.setup();
    renderRetro();
    await identify(user);
    await setStage(user, 'voting');

    const wentWell = await column('Went well');
    const vote = within(wentWell).getByRole('button', { name: /Vote for Pairing on the OIDC flow/ });
    // t1 grouped this note and carries one vote already.
    expect(vote).toHaveTextContent('1 ▲');

    await user.click(vote);
    await waitFor(async () =>
      expect(
        within(await column('Went well')).getByRole('button', { name: /Take back your vote/ }),
      ).toHaveTextContent('2 ▲'),
    );

    await user.click(
      within(await column('Went well')).getByRole('button', { name: /Take back your vote/ }),
    );
    await waitFor(async () =>
      expect(
        within(await column('Went well')).getByRole('button', { name: /Vote for Pairing/ }),
      ).toHaveTextContent('1 ▲'),
    );
  });

  it('makes a note nobody grouped votable on its own', async () => {
    const user = userEvent.setup();
    const provider = renderRetro();
    await identify(user);
    await setStage(user, 'voting');

    // n3 belongs to no theme, so the first vote for it writes one.
    const puzzles = await column('Puzzles');
    await user.click(within(puzzles).getByRole('button', { name: /Vote for .*stale snapshot badge/ }));

    await waitFor(async () => {
      const view = await provider.getRetro(sampleRetro.id);
      const theme = view.themes.find((each) => (each.notes ?? []).includes('n3'));
      expect(theme?.voters).toEqual(['ada-lovelace']);
    });
  });

  it('spends a budget of votes and stops at it', async () => {
    const user = userEvent.setup();
    const provider = renderRetro();
    await provider.updateRetro(sampleRetro.id, { votesPerPerson: 1 });
    await identify(user);
    await setStage(user, 'voting');

    await user.click(
      within(await column('Went well')).getByRole('button', { name: /Vote for Pairing/ }),
    );

    await waitFor(async () =>
      expect(
        within(await column('Puzzles')).getByRole('button', { name: /Vote for .*snapshot badge/ }),
      ).toBeDisabled(),
    );
    // The vote already cast can still be taken back.
    expect(
      within(await column('Went well')).getByRole('button', { name: /Take back your vote/ }),
    ).toBeEnabled();
  });

  it('comments on a card while the room discusses it', async () => {
    const user = userEvent.setup();
    renderRetro();
    await identify(user);
    await setStage(user, 'discussing');

    const wentWell = await column('Went well');
    const field = within(wentWell).getByLabelText(/Comment on Pairing on the OIDC flow/);
    await user.type(field, 'Worth doing again next sprint');
    await user.click(within(wentWell).getByRole('button', { name: 'Comment' }));

    const commented = await column('Went well');
    expect(
      await within(commented).findByText('Worth doing again next sprint'),
    ).toBeInTheDocument();
    expect(within(commented).getByText('ada-lovelace:')).toBeInTheDocument();
  });

  it('lets an unidentified visitor read the wall but not write on it', async () => {
    const user = userEvent.setup();
    renderRetro();
    await setStage(user, 'voting');

    expect(await screen.findByLabelText('Your name')).toBeInTheDocument();
    expect(
      within(await column('Went well')).getByRole('button', { name: /Vote for Pairing/ }),
    ).toBeDisabled();
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
