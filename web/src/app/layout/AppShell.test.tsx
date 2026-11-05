import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import { FakeProvider, sampleTeam } from '@/api/fake-provider';
import { AppShell } from '@/app/layout/AppShell';
import { useAppStore } from '@/app/store';
import { useUiPrefs } from '@/app/ui-prefs';
import { renderWithRouter } from '@/test/router';

function Blank() {
  return <div data-testid="route" />;
}

describe('AppShell', () => {
  beforeEach(() => {
    useAppStore.getState().reset();
    useUiPrefs.setState({ sidebarCollapsed: false, hiddenRepos: [], showTeamViews: true });
  });

  it('folds the sidebar down to a menu button and opens it again', async () => {
    const user = userEvent.setup();
    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    await screen.findByText('acme-platform', undefined, { timeout: 5000 });
    await user.click(screen.getByRole('button', { name: 'Collapse navigation' }));

    expect(screen.queryByRole('navigation', { name: 'Main' })).toBeNull();
    expect(useUiPrefs.getState().sidebarCollapsed).toBe(true);

    await user.click(screen.getByRole('button', { name: 'Open navigation' }));
    expect(screen.getByRole('navigation', { name: 'Main' })).toBeInTheDocument();
  });

  it('filters repositories by name or project key', async () => {
    const user = userEvent.setup();
    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    await screen.findByText('acme-platform', undefined, { timeout: 5000 });
    await user.type(screen.getByRole('searchbox', { name: 'Filter repositories' }), 'zzz');

    expect(screen.queryByText('acme-platform')).toBeNull();
    expect(screen.getByText(/No repository matches/)).toBeInTheDocument();

    await user.clear(screen.getByRole('searchbox', { name: 'Filter repositories' }));
    await user.type(screen.getByRole('searchbox', { name: 'Filter repositories' }), 'acme');
    expect(screen.getByText('acme-platform')).toBeInTheDocument();
  });

  it('hides a repository until "show hidden" is on', async () => {
    const user = userEvent.setup();
    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    await screen.findByText('acme-platform', undefined, { timeout: 5000 });
    await user.click(screen.getByRole('button', { name: 'Hide acme-platform' }));

    expect(screen.queryByText('acme-platform')).toBeNull();
    expect(useUiPrefs.getState().hiddenRepos).toHaveLength(1);

    await user.click(screen.getByRole('button', { name: /Show hidden \(1\)/ }));
    await user.click(screen.getByRole('button', { name: 'Show acme-platform' }));

    expect(useUiPrefs.getState().hiddenRepos).toHaveLength(0);
    expect(screen.getByText('acme-platform')).toBeInTheDocument();
  });

  it('shows the repositories of the active provider in the sidebar', async () => {
    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    expect(
      await screen.findByText('acme-platform', undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /ACME backlog/ })).toHaveAttribute(
      'href',
      '/p/ACME/items',
    );
  });

  it('explains the read-only fallback in one dismissible banner', async () => {
    renderWithRouter({
      index: Blank,
      root: AppShell,
      provider: new FakeProvider({ repos: [] }, { readOnly: true }),
    });

    expect(
      await screen.findByText(/Read-only session\./, undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(screen.getByText(/install the gintrack companion/i)).toBeInTheDocument();
    expect(screen.getByText('Read-only')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /dismiss read-only notice/i }));

    await waitFor(() => {
      expect(screen.queryByText(/Read-only session\./)).not.toBeInTheDocument();
    });
    expect(useAppStore.getState().readOnlyNoticeDismissed).toBe(true);
  });

  it('hides the banner when the provider can write', async () => {
    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    await screen.findByText('acme-platform', undefined, { timeout: 5000 });
    expect(screen.queryByText(/Read-only session\./)).not.toBeInTheDocument();
    expect(screen.queryByText('Read-only')).not.toBeInTheDocument();
  });

  it('shows the runtime in the mode badge, with the companion version in its tooltip', async () => {
    const store = useAppStore.getState();
    store.setMode('companion', '0.4.0');
    store.setCompanionUrl('http://127.0.0.1:7317');

    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    const badge = await screen.findByTestId('mode-badge');
    expect(badge).toHaveTextContent('companion');
    expect(badge).toHaveAttribute(
      'title',
      'Companion version 0.4.0 at http://127.0.0.1:7317 — native indexing and file watching enabled.',
    );
  });

  it('announces an upgrade in a dismissible, non-blocking notice', async () => {
    useAppStore.getState().setModeNotice('companion-detected');

    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    expect(
      await screen.findByText(/Companion detected — native indexing and file watching enabled\./),
    ).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /dismiss companion notice/i }));

    await waitFor(() => {
      expect(screen.queryByText(/Companion detected/)).not.toBeInTheDocument();
    });
    expect(useAppStore.getState().modeNotice).toBeNull();
  });

  it('lists the agent only when the runtime has one (story GIT-US-0057)', async () => {
    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    const nav = await screen.findByRole('navigation', { name: 'Main' });
    expect(within(nav).queryByRole('link', { name: 'Agent' })).toBeNull();
  });

  it('shows the agent entry when the capability is on', async () => {
    // The branch is on the capability, never on the provider kind: a fake with
    // a scripted agent is the same shape as a companion that has one.
    const provider = new FakeProvider({ agent: { events: [] } });
    renderWithRouter({ index: Blank, root: AppShell, provider });

    const nav = await screen.findByRole('navigation', { name: 'Main' });
    await waitFor(() => {
      expect(within(nav).getByRole('link', { name: 'Agent' })).toHaveAttribute('href', '/agent');
    });
  });

  it('asks for the companion token when one is missing', async () => {
    const store = useAppStore.getState();
    store.setMode('companion', '0.4.0');
    store.setCompanionAuth('required');

    renderWithRouter({ index: Blank, root: AppShell, provider: new FakeProvider() });

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('The companion needs an access token.');
    expect(within(alert).getByRole('link', { name: 'Settings' })).toHaveAttribute(
      'href',
      '/settings',
    );
  });

  describe('with team views turned off (GIT-US-0204)', () => {
    it('lists Boards, Retros and Metrics while team views are on', async () => {
      renderWithRouter({
        index: Blank,
        root: AppShell,
        provider: new FakeProvider({ team: sampleTeam }),
      });

      const nav = await screen.findByRole('navigation', { name: 'Main' });
      for (const name of ['Boards', 'Retros', 'Metrics']) {
        expect(within(nav).getByRole('link', { name })).toBeInTheDocument();
      }
    });

    it('drops the team entries from the navigation even with a team repository', async () => {
      useUiPrefs.setState({ showTeamViews: false });
      renderWithRouter({
        index: Blank,
        root: AppShell,
        provider: new FakeProvider({ team: sampleTeam }),
      });

      const nav = await screen.findByRole('navigation', { name: 'Main' });
      for (const name of ['Boards', 'Sprints', 'Retros', 'Metrics']) {
        expect(within(nav).queryByRole('link', { name })).toBeNull();
      }
      expect(within(nav).getByRole('link', { name: 'Workspace' })).toBeInTheDocument();
      expect(within(nav).getByRole('link', { name: 'Settings' })).toBeInTheDocument();
      expect(screen.getByTestId('route')).toBeInTheDocument();
    });

    it.each([
      '/boards',
      '/boards/main',
      '/sprints',
      '/retros',
      '/retros/ACME-TEAM-RT-0001',
      '/metrics',
      '/metrics/ACME-TEAM-SP-0001',
    ])('answers a direct visit to %s with a notice linking to Settings', async (path) => {
      useUiPrefs.setState({ showTeamViews: false });
      renderWithRouter({
        index: Blank,
        root: AppShell,
        provider: new FakeProvider({ team: sampleTeam }),
        initialPath: path,
      });

      const heading = await screen.findByRole('heading', { name: 'Team views are hidden' });
      expect(heading).toBeInTheDocument();
      expect(screen.queryByTestId('placeholder')).toBeNull();
      expect(
        screen.getByRole('link', { name: 'Turn team views back on in Settings' }),
      ).toHaveAttribute('href', '/settings');
    });

    it('renders the team page itself while team views are on', async () => {
      renderWithRouter({
        index: Blank,
        root: AppShell,
        provider: new FakeProvider({ team: sampleTeam }),
        initialPath: '/boards',
      });

      expect(await screen.findByTestId('placeholder')).toBeInTheDocument();
      expect(screen.queryByRole('heading', { name: 'Team views are hidden' })).toBeNull();
    });
  });
});
