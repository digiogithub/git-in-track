import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import type { RepoInfo } from '@/api/provider';
import { renderWithRouter } from '@/test/router';

import { WorkspaceHome } from './WorkspaceHome';

const readyRepo: RepoInfo = {
  id: 'repo-1',
  kind: 'project',
  name: 'acme-repo',
  location: 'acme-repo',
  docsFolder: 'docs',
  state: 'ready',
  projects: ['ACME'],
  lastIndexedAt: '2026-09-03T10:00:00Z',
};

const expiredRepo: RepoInfo = {
  ...readyRepo,
  id: 'repo-2',
  name: 'beta-repo',
  state: 'needs-permission',
  projects: [],
};

describe('WorkspaceHome', () => {
  it('states that files never leave the device', async () => {
    renderWithRouter({ index: WorkspaceHome, provider: new FakeProvider({ repos: [] }) });

    expect(
      await screen.findByText(/never leave your machine/i, undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it('offers both pickers and explains what this browser supports', async () => {
    renderWithRouter({ index: WorkspaceHome, provider: new FakeProvider({ repos: [] }) });

    // jsdom has neither API, which is exactly the unsupported-browser path:
    // capability detection runs before the picker is offered, nothing throws.
    expect(
      await screen.findByRole('button', { name: /open folder/i }, { timeout: 5000 }),
    ).toBeDisabled();
    expect(screen.getByRole('button', { name: /choose folder \(read-only\)/i })).toBeDisabled();
    expect(screen.getByText(/cannot open a local folder/i)).toBeInTheDocument();
  });

  it('shows an empty state when no repository is mounted', async () => {
    renderWithRouter({ index: WorkspaceHome, provider: new FakeProvider({ repos: [] }) });

    expect(
      await screen.findByText('No repositories yet', undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it('lists repositories with their state and project links', async () => {
    renderWithRouter({
      index: WorkspaceHome,
      provider: new FakeProvider({ repos: [readyRepo, expiredRepo] }),
    });

    expect(await screen.findByText('acme-repo', undefined, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.getByText('Ready')).toBeInTheDocument();
    expect(screen.getByText('Needs permission')).toBeInTheDocument();

    const backlog = screen.getByRole('link', { name: /ACME backlog/ });
    expect(backlog).toHaveAttribute('href', '/p/ACME/items');
    expect(screen.getByRole('link', { name: /ACME docs/ })).toHaveAttribute('href', '/p/ACME/kb');
  });

  it('reindexes a repository through the provider', async () => {
    const provider = new FakeProvider({ repos: [readyRepo] });
    const reindex = vi.spyOn(provider, 'reindex');
    renderWithRouter({ index: WorkspaceHome, provider });

    await screen.findByText('acme-repo', undefined, { timeout: 5000 });
    await userEvent.click(screen.getByRole('button', { name: /reindex/i }));

    await waitFor(() => {
      expect(reindex).toHaveBeenCalledWith('repo-1');
    });
  });

  it('removes a repository and falls back to the empty state', async () => {
    const provider = new FakeProvider({ repos: [readyRepo] });
    renderWithRouter({ index: WorkspaceHome, provider });

    await screen.findByText('acme-repo', undefined, { timeout: 5000 });
    await userEvent.click(screen.getByRole('button', { name: /remove/i }));

    expect(
      await screen.findByText('No repositories yet', undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it('offers one reconnect action for every expired folder', async () => {
    renderWithRouter({
      index: WorkspaceHome,
      provider: new FakeProvider({ repos: [expiredRepo] }),
    });

    expect(
      await screen.findByRole('button', { name: /reconnect folders \(1\)/i }, { timeout: 5000 }),
    ).toBeEnabled();
    expect(screen.getByText(/expired the permission for this folder/i)).toBeInTheDocument();
  });
});
