import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { FakeProvider, sampleTeam } from '@/api/fake-provider';
import { renderWithRouter } from '@/test/router';

import { BoardList } from './BoardList';

describe('BoardList', () => {
  it('lists the boards of the team repository', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: sampleTeam }) });

    const link = await screen.findByRole('link', { name: 'Delivery' });
    expect(link).toHaveAttribute('href', '/boards/delivery');
    const kanban = link.closest('li') as HTMLElement;
    expect(within(kanban).getByText('kanban')).toBeInTheDocument();
    expect(within(kanban).getByText('ACME')).toBeInTheDocument();
    expect(within(kanban).getByText('WEB')).toBeInTheDocument();
  });

  it('marks a scrum board with the sprint it runs', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: sampleTeam }) });

    const link = await screen.findByRole('link', { name: 'SSO Sprint Board' });
    const scrum = link.closest('li') as HTMLElement;
    expect(within(scrum).getByText('scrum')).toBeInTheDocument();
    expect(within(scrum).getByText('ACME-TEAM-S-0007')).toBeInTheDocument();
  });

  it('says so when no team repository is open, and offers a way to open one', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: null }) });

    expect(await screen.findByText('No team repository is open')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Delivery' })).not.toBeInTheDocument();
    // The empty state points at a flow that exists, instead of asking the user
    // to "mount a team repository" by some means the UI does not have.
    expect(
      screen.getByRole('link', { name: /add or create a team repository/i }),
    ).toHaveAttribute('href', '/repos/add');
  });

  it('explains why "New board" is disabled instead of just disabling it', async () => {
    renderWithRouter({ index: BoardList, provider: new FakeProvider({ team: null }) });

    // Wait for the teams to load: until they do, the reason is "loading".
    await screen.findByText('No team repository is open');
    const button = screen.getByRole('button', { name: 'New board' });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('title', expect.stringMatching(/team repository/i));
  });
});
