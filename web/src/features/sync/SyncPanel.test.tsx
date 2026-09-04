import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import { ProviderContext } from '@/api/provider-context';
import { SyncPanel } from '@/features/sync/SyncPanel';

/** Renders the panel against a provider, which is its only dependency. */
function renderPanel(provider = new FakeProvider()) {
  render(
    <ProviderContext.Provider value={provider}>
      <SyncPanel />
    </ProviderContext.Provider>,
  );
  return provider;
}

describe('SyncPanel', () => {
  it('shows a clean repository as up to date with its counters', async () => {
    renderPanel();

    expect(await screen.findByText('Up to date')).toBeInTheDocument();
    expect(screen.getByText('Branch')).toBeInTheDocument();
    expect(screen.getByText('main')).toBeInTheDocument();
    expect(screen.getByText('0 / 0')).toBeInTheDocument();
  });

  it('reports ahead and behind counts as a diverged repository', async () => {
    const provider = new FakeProvider();
    provider.syncStatuses = [
      {
        repo: 'demo',
        path: '/tmp/demo',
        git: true,
        pending: 2,
        status: {
          branch: 'main',
          detached: false,
          clean: false,
          dirty: ['docs/index.md'],
          trackedChanges: true,
          remote: 'origin',
          upstream: 'origin/main',
          ahead: 1,
          behind: 3,
          state: 'diverged',
        },
      },
    ];
    renderPanel(provider);

    expect(await screen.findByText('Diverged')).toBeInTheDocument();
    expect(screen.getByText('1 / 3')).toBeInTheDocument();
    expect(screen.getByText('1 file(s)')).toBeInTheDocument();
    expect(
      screen.getByText('2 edit(s) are waiting to be committed; a sync commits them first.'),
    ).toBeInTheDocument();
  });

  it('previews what a sync would move without changing anything', async () => {
    const provider = new FakeProvider();
    provider.syncResults = [
      {
        repo: 'demo',
        dryRun: true,
        strategy: 'rebase',
        phase: 'done',
        before: blankStatus(),
        after: blankStatus(),
        pulled: 0,
        pushed: 0,
        incoming: [{ sha: 'abcdef1234', subject: 'docs: teammate work' }],
        outgoing: [{ sha: '1234abcdef', subject: 'docs: mine' }],
        retries: 0,
        durationMs: 12,
      },
    ];
    renderPanel(provider);

    await userEvent.click(await screen.findByRole('button', { name: 'Preview' }));

    await waitFor(() => {
      expect(
        screen.getByText(
          'Preview: 1 commit(s) would come in, 1 would be pushed. Nothing was changed.',
        ),
      ).toBeInTheDocument();
    });
    expect(screen.getByText('docs: teammate work')).toBeInTheDocument();
    expect(screen.getByText('abcdef1')).toBeInTheDocument();
  });

  it('explains a rejected push and names the conflicted files', async () => {
    const provider = new FakeProvider();
    provider.syncResults = [
      {
        repo: 'demo',
        dryRun: false,
        strategy: 'rebase',
        phase: 'conflicts',
        before: blankStatus(),
        after: blankStatus(),
        pulled: 0,
        pushed: 0,
        conflicts: [{ path: 'docs/.pmngr/boards/team.md', kind: 'content' }],
        retries: 0,
        durationMs: 20,
        code: 'git_conflict',
        message: 'The rebase stopped on 1 conflicted file; nothing was pushed.',
      },
    ];
    renderPanel(provider);

    await userEvent.click(await screen.findByRole('button', { name: 'Sync' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('nothing was pushed');
    expect(alert).toHaveTextContent('docs/.pmngr/boards/team.md (content)');
  });

  it('explains a runtime that cannot sync instead of offering the buttons', async () => {
    const provider = new FakeProvider();
    provider.syncSettings = {
      pullStrategy: 'merge',
      pushOnSync: true,
      maxPushRetries: 1,
      supported: false,
      reason: 'Set a CORS proxy in Settings → Sync.',
    };
    renderPanel(provider);

    expect(await screen.findByRole('status')).toHaveTextContent('Set a CORS proxy');
    expect(screen.getByRole('button', { name: 'Sync' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled();
  });
});

/** A clean status, for the reports whose counters the test does not care about. */
function blankStatus() {
  return {
    branch: 'main',
    detached: false,
    clean: true,
    trackedChanges: false,
    remote: 'origin',
    upstream: 'origin/main',
    ahead: 0,
    behind: 0,
    state: 'up_to_date' as const,
  };
}
