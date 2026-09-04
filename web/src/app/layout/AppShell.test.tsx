import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import { AppShell } from '@/app/layout/AppShell';
import { useAppStore } from '@/app/store';
import { renderWithRouter } from '@/test/router';

function Blank() {
  return <div data-testid="route" />;
}

describe('AppShell', () => {
  beforeEach(() => {
    useAppStore.getState().reset();
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
});
