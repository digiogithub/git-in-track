import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider, type FakeSearch } from '@/api/fake-provider';
import type { RepoInfo, SearchCodeIndex, SearchIndexedRepo } from '@/api/provider';
import { useAppStore } from '@/app/store';
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

/** The settings row of `readyRepo`, with the given code-index state. */
function indexedRow(status?: SearchCodeIndex['status']): SearchIndexedRepo {
  return {
    repo: 'repo-1',
    root: '/home/dana/src/acme-repo',
    docs: ['docs'],
    items: 3,
    pages: 1,
    comments: 0,
    ...(status === undefined ? {} : { code: { project: 'home_dana_src_acme-repo', status } }),
  };
}

function semanticProvider(search: FakeSearch = {}): FakeProvider {
  return new FakeProvider({ repos: [readyRepo], search });
}

describe('semantic search per repository (GIT-US-0101)', () => {
  it('is absent where the runtime has no search settings', async () => {
    renderWithRouter({ index: WorkspaceHome, provider: new FakeProvider({ repos: [readyRepo] }) });

    await screen.findByText('acme-repo', undefined, { timeout: 5000 });
    expect(screen.queryByText(/semantic search/i)).toBeNull();
  });

  it.each([
    ['registered', /semantic search on/i],
    ['indexing', /semantic search indexing/i],
  ] as const)('shows a %s repository without an enable button', async (status, label) => {
    renderWithRouter({
      index: WorkspaceHome,
      provider: semanticProvider({ settings: { indexed: [indexedRow(status)] } }),
    });

    expect(await screen.findByText(label, undefined, { timeout: 5000 })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /enable semantic search/i })).toBeNull();
  });

  it.each(['off', 'unavailable'] as const)(
    'offers to enable a repository whose index is %s',
    async (status) => {
      renderWithRouter({
        index: WorkspaceHome,
        provider: semanticProvider({ settings: { indexed: [indexedRow(status)] } }),
      });

      expect(
        await screen.findByText(`Semantic search ${status}`, undefined, { timeout: 5000 }),
      ).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /enable semantic search/i })).toBeEnabled();
    },
  );

  it('links to the settings card when Pando is not configured', async () => {
    renderWithRouter({
      index: WorkspaceHome,
      provider: semanticProvider({
        settings: { configured: false, mcpUrl: '', indexed: [indexedRow('off')] },
      }),
    });

    const link = await screen.findByRole(
      'link',
      { name: /set up semantic search/i },
      { timeout: 5000 },
    );
    expect(link).toHaveAttribute('href', '/settings#semantic-search');
    expect(screen.queryByRole('button', { name: /enable semantic search/i })).toBeNull();
  });

  it('reindexes only that repository and follows the job to its end', async () => {
    const provider = semanticProvider({
      settings: { indexed: [indexedRow('unavailable')] },
      reindexJob: { repos: [{ repo: 'repo-1' }] },
    });
    const reindexSearch = vi.spyOn(provider, 'reindexSearch');
    const reindexAll = vi.spyOn(provider, 'reindex');
    renderWithRouter({ index: WorkspaceHome, provider });

    await userEvent.click(
      await screen.findByRole('button', { name: /enable semantic search/i }, { timeout: 5000 }),
    );

    await waitFor(() => {
      expect(reindexSearch).toHaveBeenCalledWith('repo-1');
    });
    expect(reindexAll).not.toHaveBeenCalled();
    expect(await screen.findByText(/semantic search indexing/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /enable semantic search/i })).toBeNull();

    // The job fails for this repository: the terminal frame re-reads the
    // settings, and the row offers the switch again.
    provider.finishSearchReindex({
      phase: 'failed',
      repos: [{ repo: 'repo-1', codeError: 'refused' }],
    });
    act(() => {
      provider.emitEvent({
        kind: 'searchProgress',
        operationId: 'reindex-1',
        repoId: '',
        phase: 'failed',
        percent: 100,
        done: 1,
        total: 1,
        message: '',
      });
    });

    expect(
      await screen.findByText('Semantic search unavailable', undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /enable semantic search/i })).toBeEnabled();
  });

  it('says so when a reindex is already running', async () => {
    renderWithRouter({
      index: WorkspaceHome,
      provider: semanticProvider({
        settings: { indexed: [indexedRow('off')] },
        reindexError: { code: 'search_reindex_running', message: 'running' },
      }),
    });

    await userEvent.click(
      await screen.findByRole('button', { name: /enable semantic search/i }, { timeout: 5000 }),
    );

    expect(await screen.findByRole('alert')).toHaveTextContent(/already running/i);
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
