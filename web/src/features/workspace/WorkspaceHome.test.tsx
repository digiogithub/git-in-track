import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider, sampleProject } from '@/api/fake-provider';
import { ProviderError, type ProjectSummary, type RepoInfo } from '@/api/provider';
import { useAppStore } from '@/app/store';
import { ToastProvider } from '@/components/ui/toast';
import { InboxNavLink } from '@/features/inbox/InboxNavLink';
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

describe('enabling a project inbox', () => {
  const triaged: ProjectSummary = {
    ...sampleProject,
    statuses: [{ id: 'triage', name: 'Triage', category: 'triage' }, ...sampleProject.statuses],
  };

  /** The workspace list next to the sidebar entry the button should reveal. */
  function HomeWithInboxLink() {
    return (
      <ToastProvider>
        <InboxNavLink project="ACME" />
        <WorkspaceHome />
      </ToastProvider>
    );
  }

  it('adds the triage status and shows the inbox link without a reload', async () => {
    const provider = new FakeProvider({
      repos: [readyRepo],
      projects: [{ ...sampleProject, configRev: 'sha256:1111111111111111' }],
    });
    const enable = vi.spyOn(provider, 'enableInbox');
    renderWithRouter({ index: HomeWithInboxLink, provider });

    const button = await screen.findByRole(
      'button',
      { name: 'Enable inbox for ACME' },
      { timeout: 5000 },
    );
    expect(screen.queryByRole('link', { name: /ACME inbox/ })).toBeNull();

    await userEvent.click(button);

    expect(await screen.findByRole('link', { name: /ACME inbox/ })).toHaveAttribute(
      'href',
      '/p/ACME/inbox',
    );
    expect(enable).toHaveBeenCalledWith({ project: 'ACME', rev: 'sha256:1111111111111111' });
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Enable inbox for ACME' })).toBeNull();
    });
  });

  it('is not offered for a project that already has an inbox', async () => {
    renderWithRouter({
      index: WorkspaceHome,
      provider: new FakeProvider({ repos: [readyRepo], projects: [triaged] }),
    });

    await screen.findByRole('link', { name: /ACME backlog/ }, { timeout: 5000 });
    expect(screen.queryByRole('button', { name: /enable inbox/i })).toBeNull();
  });

  it('is not offered in a read-only workspace or for a read-only project', async () => {
    const { unmount } = renderWithRouter({
      index: WorkspaceHome,
      provider: new FakeProvider({ repos: [readyRepo] }, { readOnly: true }),
    });
    await screen.findByRole('link', { name: /ACME backlog/ }, { timeout: 5000 });
    expect(screen.queryByRole('button', { name: /enable inbox/i })).toBeNull();
    unmount();

    renderWithRouter({
      index: WorkspaceHome,
      provider: new FakeProvider({
        repos: [readyRepo],
        projects: [{ ...sampleProject, writable: false }],
      }),
    });
    await screen.findByRole('link', { name: /ACME backlog/ }, { timeout: 5000 });
    expect(screen.queryByRole('button', { name: /enable inbox/i })).toBeNull();
  });

  it('reports a refusal in a toast', async () => {
    const provider = new FakeProvider({ repos: [readyRepo] });
    vi.spyOn(provider, 'enableInbox').mockRejectedValue(
      new ProviderError('triage_status_id_taken', 'ACME already has a status called triage'),
    );
    renderWithRouter({ index: HomeWithInboxLink, provider });

    await userEvent.click(
      await screen.findByRole('button', { name: 'Enable inbox for ACME' }, { timeout: 5000 }),
    );

    expect(await screen.findByText('The inbox of ACME could not be enabled')).toBeVisible();
    expect(screen.getByText('ACME already has a status called triage')).toBeVisible();
  });
});

describe('companion mode', () => {
  afterEach(() => {
    useAppStore.getState().reset();
  });

  it('replaces the browser folder picker with the companion guidance', async () => {
    useAppStore.getState().setMode('companion', '1.2.3');
    renderWithRouter({ index: WorkspaceHome, provider: new FakeProvider({ repos: [readyRepo] }) });

    expect(await screen.findByText(/gintrack add/i)).toBeVisible();
    expect(screen.queryByRole('button', { name: /open folder/i })).toBeNull();
    expect(screen.queryByText(/never leave your machine/i)).toBeNull();
  });
});
