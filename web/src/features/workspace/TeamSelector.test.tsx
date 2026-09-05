import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import { FakeProvider, sampleBoard, sampleTeam } from '@/api/fake-provider';
import type { TeamSummary } from '@/api/provider';
import { useAppStore } from '@/app/store';
import { BoardList } from '@/features/boards/BoardList';
import { renderWithRouter } from '@/test/router';

import { TeamSelector } from './TeamSelector';

/**
 * The active team of a workspace holding several team repositories
 * (story GIT-US-0036). What matters is that the choice reaches the calls that
 * read boards, sprints and retros, and that it survives a reload.
 */

/** A second team repository, sharing the workspace with `sampleTeam`. */
const platformTeam: TeamSummary = {
  ...sampleTeam,
  key: 'PLATFORM-TEAM',
  name: 'Platform Team',
  vaultId: 'repo-platform',
};

/** Two teams, each holding one board of its own. */
function twoTeams(): FakeProvider {
  return new FakeProvider({
    teams: [sampleTeam, platformTeam],
    boards: [
      { ...sampleBoard, team: 'ACME-TEAM' },
      {
        ...sampleBoard,
        team: 'PLATFORM-TEAM',
        id: 'platform',
        title: 'Platform',
        projects: ['ACME'],
      },
    ],
  });
}

describe('TeamSelector', () => {
  beforeEach(() => {
    globalThis.localStorage.clear();
    useAppStore.getState().reset();
  });

  it('renders nothing while the workspace holds a single team', async () => {
    renderWithRouter({
      index: TeamSelector,
      provider: new FakeProvider({ team: sampleTeam, boards: [sampleBoard] }),
    });

    await expect(
      screen.findByRole('combobox', { name: 'Team' }, { timeout: 1000 }),
    ).rejects.toThrow();
  });

  it('offers every open team and marks the active one', async () => {
    renderWithRouter({ index: TeamSelector, provider: twoTeams() });

    const select = await screen.findByRole('combobox', { name: 'Team' });
    expect(select).toHaveValue('ACME-TEAM');
    expect(screen.getByRole('option', { name: 'ACME Delivery Team' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Platform Team' })).toBeInTheDocument();
  });

  it('switching the team shows the other team artifacts', async () => {
    // The board index carries the selector itself, which is the point: the
    // switch and the list it drives are on the same screen.
    renderWithRouter({ index: BoardList, provider: twoTeams() });

    expect(await screen.findByRole('link', { name: 'Delivery' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Platform' })).not.toBeInTheDocument();

    const select = await screen.findByRole('combobox', { name: 'Team' });
    await userEvent.selectOptions(select, 'PLATFORM-TEAM');

    expect(await screen.findByRole('link', { name: 'Platform' })).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByRole('link', { name: 'Delivery' })).not.toBeInTheDocument(),
    );
  });

  it('remembers the choice across a reload of the app', async () => {
    const first = renderWithRouter({ index: TeamSelector, provider: twoTeams() });
    await userEvent.selectOptions(
      await screen.findByRole('combobox', { name: 'Team' }),
      'PLATFORM-TEAM',
    );
    first.unmount();

    // A reload keeps the browser storage and loses the store, which is exactly
    // what mounting the app again with a fresh store does.
    useAppStore.getState().reset();
    renderWithRouter({ index: TeamSelector, provider: twoTeams() });

    await waitFor(async () =>
      expect(await screen.findByRole('combobox', { name: 'Team' })).toHaveValue('PLATFORM-TEAM'),
    );
  });

  it('falls back to the first team when the remembered one is gone', async () => {
    globalThis.localStorage.setItem('gintrack:active-team:browser', 'RETIRED-TEAM');
    renderWithRouter({ index: TeamSelector, provider: twoTeams() });

    expect(await screen.findByRole('combobox', { name: 'Team' })).toHaveValue('ACME-TEAM');
  });
});
