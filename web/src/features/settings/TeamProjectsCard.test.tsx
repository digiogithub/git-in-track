import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import { FakeProvider, sampleBoard, sampleProject, sampleTeam } from '@/api/fake-provider';
import type { ProjectSummary, TeamSummary } from '@/api/provider';
import { useAppStore } from '@/app/store';
import { renderWithRouter } from '@/test/router';

import { TeamProjectsCard } from './TeamProjectsCard';

/**
 * Managing a team's project list from settings (story GIT-US-0037).
 *
 * What matters is the link the card makes visible: a registered repository is
 * connected to a `team.yaml` entry by project key alone, so the candidate list
 * offers what this machine has indexed and the entry it writes is cloned from
 * then on. The two refusals — a duplicate key and a project boards still
 * reference — have to be readable, not silent.
 */

/** A second locally indexed project, not yet declared by the team. */
const toolsProject: ProjectSummary = {
  ...sampleProject,
  key: 'TOOLS',
  name: 'Internal Tools',
  docsPath: 'documentation',
  vaultId: 'repo-2',
};

/** A team declaring one project, with a second one indexed but unconnected. */
function oneProjectTeam(): FakeProvider {
  const team: TeamSummary = {
    ...sampleTeam,
    projects: [sampleTeam.projects[0]!],
  };
  const provider = new FakeProvider({
    team,
    projects: [{ ...sampleProject, vaultId: 'repo-1' }, toolsProject],
    boards: [sampleBoard],
    repos: [
      {
        id: 'repo-1',
        kind: 'project',
        name: 'acme-platform',
        location: 'acme-platform',
        docsFolder: 'docs',
        state: 'ready',
        projects: ['ACME'],
      },
      {
        id: 'repo-2',
        kind: 'project',
        name: 'internal-tools',
        location: 'internal-tools',
        docsFolder: 'documentation',
        state: 'ready',
        projects: ['TOOLS'],
      },
    ],
  });
  provider.syncStatuses = [
    {
      repo: 'repo-2',
      path: 'internal-tools',
      git: true,
      pending: 0,
      status: {
        branch: 'trunk',
        detached: false,
        clean: true,
        trackedChanges: false,
        remoteUrl: 'https://github.com/acme/internal-tools.git',
        ahead: 0,
        behind: 0,
        state: 'up_to_date',
      },
    },
  ];
  return provider;
}

function renderCard(provider: FakeProvider) {
  return renderWithRouter({ index: TeamProjectsCard, provider });
}

describe('TeamProjectsCard', () => {
  beforeEach(() => {
    globalThis.localStorage.clear();
    useAppStore.getState().reset();
  });

  it('says which entries are cloned and which render from a snapshot', async () => {
    renderCard(new FakeProvider({ team: sampleTeam, boards: [sampleBoard] }));

    const list = await screen.findByRole('list', { name: 'Declared projects' });
    const rows = within(list).getAllByRole('listitem');
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByText('Cloned')).toBeInTheDocument();
    expect(within(rows[1]!).getByText('From snapshot')).toBeInTheDocument();
  });

  it('offers the locally indexed projects and pre-fills the entry', async () => {
    renderCard(oneProjectTeam());

    const picker = await screen.findByLabelText(/Registered repository/);
    await waitFor(() => {
      expect(within(picker).getByRole('option', { name: /Internal Tools/ })).toBeInTheDocument();
    });
    // A project the team already declares is not offered twice.
    expect(within(picker).queryByRole('option', { name: /ACME Platform/ })).not.toBeInTheDocument();

    await userEvent.selectOptions(picker, 'TOOLS');

    expect(screen.getByLabelText(/Project key/)).toHaveValue('TOOLS');
    expect(screen.getByLabelText(/^Name/)).toHaveValue('Internal Tools');
    expect(screen.getByLabelText(/Docs folder/)).toHaveValue('documentation');
    expect(screen.getByLabelText(/Repository URL/)).toHaveValue(
      'https://github.com/acme/internal-tools.git',
    );
    expect(screen.getByLabelText(/Default branch/)).toHaveValue('trunk');
  });

  it('declares the picked project and shows it in the list', async () => {
    const provider = oneProjectTeam();
    renderCard(provider);

    const picker = await screen.findByLabelText(/Registered repository/);
    await waitFor(() => {
      expect(within(picker).getByRole('option', { name: /Internal Tools/ })).toBeInTheDocument();
    });
    await userEvent.selectOptions(picker, 'TOOLS');
    await userEvent.click(screen.getByRole('button', { name: /Add project/ }));

    const list = await screen.findByRole('list', { name: 'Declared projects' });
    await waitFor(() => {
      expect(within(list).getByText('Internal Tools')).toBeInTheDocument();
    });
    const team = await provider.getTeam();
    expect(team?.projects.map((p) => p.key)).toEqual(['ACME', 'TOOLS']);
  });

  it('refuses a key the team already declares', async () => {
    renderCard(oneProjectTeam());

    await screen.findByLabelText(/Project key/);
    await userEvent.type(screen.getByLabelText(/Project key/), 'ACME');
    await userEvent.type(screen.getByLabelText(/Repository URL/), 'https://x/y.git');
    await userEvent.type(screen.getByLabelText(/Docs folder/), 'docs');

    expect(await screen.findByRole('alert')).toHaveTextContent(/already declares ACME/);
    expect(screen.getByRole('button', { name: /Add project/ })).toBeDisabled();
  });

  it('refuses to remove a project boards and sprints reference, then forces it', async () => {
    const provider = new FakeProvider({ team: sampleTeam, boards: [sampleBoard] });
    renderCard(provider);

    await userEvent.click(await screen.findByRole('button', { name: 'Remove ACME' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/still point at project ACME/);
    await expect(provider.getTeam()).resolves.toMatchObject({
      projects: [{ key: 'ACME' }, { key: 'WEB' }],
    });

    await userEvent.click(screen.getByRole('button', { name: 'Remove anyway' }));

    await waitFor(async () => {
      const team = await provider.getTeam();
      expect(team?.projects.map((p) => p.key)).toEqual(['WEB']);
    });
  });

  it('surfaces a clone that declares a different key', async () => {
    const team: TeamSummary = {
      ...sampleTeam,
      projects: [
        {
          ...sampleTeam.projects[0]!,
          diagnostics: [
            {
              code: 'W-TEAM-KEY-MISMATCH',
              severity: 'warning',
              message: 'the clone declares key "PLATFORM" but team.yaml lists it as "ACME"',
            },
          ],
        },
      ],
    };
    renderCard(new FakeProvider({ team, boards: [sampleBoard] }));

    expect(await screen.findByText('W-TEAM-KEY-MISMATCH')).toBeInTheDocument();
    expect(screen.getByText(/lists it as "ACME"/)).toBeInTheDocument();
  });

  it('says there is nothing to manage while no team is open', async () => {
    renderCard(new FakeProvider({ teams: [], boards: [] }));

    expect(await screen.findByText(/No team repository is open/)).toBeInTheDocument();
  });
});
