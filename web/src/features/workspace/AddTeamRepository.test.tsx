import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import type { CreateTeamInput, MountInput } from '@/api/provider';
import { useAppStore } from '@/app/store';
import { clearVaultRegistry, MemoryVault, registerVault } from '@/fs';
import { renderWithRouter } from '@/test/router';

import { AddRepositoryPage } from './AddRepositoryPage';

/** A provider that records the team half of what the wizard asked it to do. */
class RecordingProvider extends FakeProvider {
  readonly mounts: MountInput[] = [];
  readonly createdTeams: CreateTeamInput[] = [];

  override mountRepo(input: MountInput) {
    this.mounts.push(input);
    return super.mountRepo(input);
  }

  override createTeam(input: CreateTeamInput) {
    this.createdTeams.push(input);
    return super.createTeam(input);
  }
}

const TEAM_YAML = 'schema: 1\nkey: ACME-TEAM\nname: ACME Delivery Team\n';

function pickFolder(files: Record<string, string>): string {
  const id = registerVault(new MemoryVault(files, { name: 'acme-team' }));
  useAppStore.getState().setPendingVault(id, 'acme-team');
  return id;
}

afterEach(() => {
  clearVaultRegistry();
  useAppStore.getState().setPendingVault(null);
});

describe('AddRepositoryPage, team repositories', () => {
  it('detects a team.yaml and mounts the folder as a team repository', async () => {
    const user = userEvent.setup();
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'team.yaml': TEAM_YAML });

    renderWithRouter({ index: AddRepositoryPage, provider });

    // Detection preselects the role the markers imply.
    expect(
      await screen.findByRole('radio', { name: /team repository/i }, { timeout: 5000 }),
    ).toBeChecked();
    expect(screen.getAllByText(/ACME Delivery Team/).length).toBeGreaterThan(0);

    await user.click(screen.getByRole('button', { name: /mount team repository/i }));

    await waitFor(() => {
      expect(provider.mounts).toEqual([expect.objectContaining({ kind: 'team' })]);
    });
    expect(provider.createdTeams).toEqual([]);
  });

  it('creates a team repository when the folder has no team.yaml', async () => {
    const user = userEvent.setup();
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'README.md': '# acme\n' });

    renderWithRouter({ index: AddRepositoryPage, provider });
    await screen.findByRole('radio', { name: /team repository/i }, { timeout: 5000 });

    await user.click(screen.getByRole('radio', { name: /team repository/i }));
    await user.type(screen.getByLabelText(/team key/i), 'ACME-TEAM');
    await user.type(screen.getByLabelText(/team name/i), 'ACME Delivery Team');
    await user.click(screen.getByRole('button', { name: /create team repository/i }));

    await waitFor(() => {
      expect(provider.createdTeams).toEqual([
        expect.objectContaining({ key: 'ACME-TEAM', name: 'ACME Delivery Team' }),
      ]);
    });
    // The folder is mounted as a team first: the core has to hold it before it
    // can write team.yaml into it.
    expect(provider.mounts[0]).toEqual(expect.objectContaining({ kind: 'team' }));
  });

  it('refuses a team key the grammar does not accept, without calling the provider', async () => {
    const user = userEvent.setup();
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'README.md': '# acme\n' });

    renderWithRouter({ index: AddRepositoryPage, provider });
    await screen.findByRole('radio', { name: /team repository/i }, { timeout: 5000 });

    await user.click(screen.getByRole('radio', { name: /team repository/i }));
    await user.type(screen.getByLabelText(/team key/i), 'A');
    await user.click(screen.getByRole('button', { name: /create team repository/i }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/2 to 16 characters/i);
    expect(provider.createdTeams).toEqual([]);
  });

  it('lets a project repository be registered as a team one deliberately', async () => {
    const user = userEvent.setup();
    const provider = new RecordingProvider({ repos: [], projects: [] });
    pickFolder({ 'docs/.pmngr/project.yaml': 'schema: 1\nkey: ACME\n' });

    renderWithRouter({ index: AddRepositoryPage, provider });
    await screen.findByRole('radio', { name: /project repository/i }, { timeout: 5000 });

    // Detection saw a backlog, so "project" is the default choice.
    expect(screen.getByRole('radio', { name: /project repository/i })).toBeChecked();

    await user.click(screen.getByRole('radio', { name: /team repository/i }));
    expect(screen.getByRole('button', { name: /create team repository/i })).toBeInTheDocument();
  });
});
