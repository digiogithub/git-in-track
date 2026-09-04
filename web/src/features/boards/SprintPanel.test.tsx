import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleSprint, sampleTeam } from '@/api/fake-provider';
import { ToastProvider } from '@/components/ui/toast';
import { renderWithRouter } from '@/test/router';

import { BoardCanvas } from './BoardView';

/** The scrum board of the sample team, writable unless said so. */
function renderScrumBoard(provider = new FakeProvider({ team: sampleTeam })) {
  renderWithRouter({
    index: () => (
      <ToastProvider>
        <BoardCanvas slug="acme-scrum" />
      </ToastProvider>
    ),
    provider,
  });
  return provider;
}

/** The rendered column with that id. */
async function column(id: string): Promise<HTMLElement> {
  return await waitFor(() => {
    const found = document.querySelector<HTMLElement>(`[data-column="${id}"]`);
    if (!found) throw new Error(`no column ${id}`);
    return found;
  });
}

function refsIn(element: HTMLElement): string[] {
  return [...element.querySelectorAll('[data-ref]')].map((node) => node.getAttribute('data-ref')!);
}

describe('the scrum board', () => {
  it('shows the goal, the dates, the days left and the points', async () => {
    renderScrumBoard();

    const panel = await screen.findByTestId('sprint-panel');
    expect(within(panel).getByText('Sprint 7 — SSO end to end')).toBeInTheDocument();
    expect(within(panel).getByText('active')).toBeInTheDocument();
    expect(
      within(panel).getByText('A tenant can log in with their identity provider in staging.'),
    ).toBeInTheDocument();
    expect(within(panel).getByText('2026-08-24 → 2026-09-06')).toBeInTheDocument();
    expect(within(panel).getByText('5 of 14 days left')).toBeInTheDocument();
    // ACME-US-0042 is 8 points, WEB-US-0031 is 5, and both were committed.
    expect(within(panel).getByText('13 points')).toBeInTheDocument();
    expect(within(panel).getByText('0 of 13 points')).toBeInTheDocument();
  });

  it('shows only the sprint scope, with the candidates in the backlog column', async () => {
    renderScrumBoard();

    const doing = await column('in_progress');
    // The remote sprint item lands in the column its snapshot status maps to.
    expect(refsIn(doing)).toEqual(['ACME/ACME-US-0042', 'WEB/WEB-US-0031']);

    // ACME-US-0043 and ACME-T-0107 are not in the sprint: they are candidates.
    const backlog = await column('sprint_backlog');
    expect(refsIn(backlog)).toEqual(['ACME/ACME-T-0107', 'ACME/ACME-US-0043']);
  });

  it('renders the remote item of the sprint from the committed snapshot', async () => {
    renderScrumBoard();

    const doing = await column('in_progress');
    const remote = within(doing).getByText('Rewrite the hero section');
    expect(remote).toBeInTheDocument();
    expect(within(doing).getAllByText('remote').length).toBeGreaterThan(0);
  });

  it('moves items in and out of the sprint from the planning view', async () => {
    const user = userEvent.setup();
    const provider = renderScrumBoard();

    await user.click(await screen.findByRole('button', { name: 'Plan sprint' }));
    const scope = await screen.findByRole('region', { name: 'In the sprint' });
    const candidates = await screen.findByRole('region', { name: 'Sprint candidates' });

    // A candidate joins the sprint; the scope grows by one reference.
    const add = within(candidates)
      .getByText('Add OIDC client')
      .closest('li') as HTMLElement;
    await user.click(within(add).getByRole('button', { name: 'Add' }));
    await waitFor(async () => {
      const sprint = await provider.getSprint(sampleSprint.id);
      expect(sprint.sprint.items).toContain('ACME/ACME-T-0107');
    });

    // A member leaves it again, including the remote one: sprint membership
    // lives in the team repository (docs/04 R-SPR-2).
    const remove = within(scope)
      .getByText('Rewrite the hero section')
      .closest('li') as HTMLElement;
    await user.click(within(remove).getByRole('button', { name: 'Remove' }));
    await waitFor(async () => {
      const sprint = await provider.getSprint(sampleSprint.id);
      expect(sprint.sprint.items).not.toContain('WEB/WEB-US-0031');
    });
  });

  it('saves a new goal to the sprint file', async () => {
    const user = userEvent.setup();
    const provider = renderScrumBoard();

    await user.click(await screen.findByRole('button', { name: 'Edit goal' }));
    const field = screen.getByRole('textbox', { name: 'Goal' });
    await user.clear(field);
    await user.type(field, 'Ship SSO to staging');
    await user.click(screen.getByRole('button', { name: 'Save goal' }));

    await waitFor(async () => {
      const sprint = await provider.getSprint(sampleSprint.id);
      expect(sprint.sprint.goal).toBe('Ship SSO to staging');
    });
  });

  it('refuses a sprint whose dates overlap the running one', async () => {
    const user = userEvent.setup();
    renderScrumBoard();

    await user.click(await screen.findByRole('button', { name: 'New sprint' }));
    await user.type(screen.getByLabelText('Start'), '2026-09-01');
    await user.type(screen.getByLabelText('End'), '2026-09-14');
    await user.click(screen.getByRole('button', { name: 'Create sprint' }));

    expect(await screen.findByText('These dates overlap another sprint')).toBeInTheDocument();
  });

  it('closes a sprint, reporting completed against incomplete work', async () => {
    const user = userEvent.setup();
    const provider = renderScrumBoard();

    await user.click(await screen.findByRole('button', { name: 'Close sprint' }));
    expect(
      await screen.findByText(/0 of 2 items finished \(0 of 13 points\)/),
    ).toBeInTheDocument();

    // Each unfinished item gets an explicit decision; nothing happens by itself.
    const unfinished = screen.getByRole('list', { name: 'Unfinished items' });
    expect(within(unfinished).getAllByRole('listitem')).toHaveLength(2);
    await user.selectOptions(
      screen.getByLabelText('What happens to ACME-US-0042'),
      'backlog',
    );
    await user.click(screen.getByRole('button', { name: 'Close sprint' }));

    await waitFor(async () => {
      const sprint = await provider.getSprint(sampleSprint.id);
      expect(sprint.sprint.state).toBe('closed');
    });
    const item = await provider.getItem('ACME-US-0042');
    expect(item.status).toBe('backlog');
  });

  it('creates a planned sprint and starts it once the board is free', async () => {
    const user = userEvent.setup();
    const provider = new FakeProvider({
      team: sampleTeam,
      sprints: [{ ...sampleSprint, state: 'planned', committed: [] }],
    });
    renderScrumBoard(provider);

    await user.click(await screen.findByRole('button', { name: 'Start sprint' }));
    await waitFor(async () => {
      const sprint = await provider.getSprint(sampleSprint.id);
      expect(sprint.sprint.state).toBe('active');
      // Starting a sprint freezes what it committed to (R-SPR-1).
      expect(sprint.sprint.committed).toEqual(sampleSprint.items);
    });
  });
});
