import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { BrowserProvider } from '@/api/browser-provider';
import { FakeProvider } from '@/api/fake-provider';
import type { DataProvider } from '@/api/provider';
import { ProviderContext } from '@/api/provider-context';

import { UPDATE_DISMISSED_KEY, UpdateNotice } from './UpdateNotice';

function renderWithProviders(ui: ReactElement, { provider }: { provider: DataProvider }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ProviderContext.Provider value={provider}>{ui}</ProviderContext.Provider>
    </QueryClientProvider>,
  );
}

const newer = {
  current: '2.2.0',
  latest: '2.3.0',
  updateAvailable: true,
  url: 'https://example.test/releases/v2.3.0',
};

describe('UpdateNotice', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('shows the command when a newer release exists', async () => {
    renderWithProviders(<UpdateNotice />, { provider: new FakeProvider({ version: newer }) });

    expect(await screen.findByText(/gintrack 2\.3\.0 is available/)).toBeInTheDocument();
    expect(screen.getByText('gintrack update')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Release notes' })).toHaveAttribute('href', newer.url);
  });

  it('stays hidden when up to date', async () => {
    const provider = new FakeProvider();
    const spy = vi.spyOn(provider, 'getVersionStatus');
    renderWithProviders(<UpdateNotice />, { provider });

    await waitFor(() => expect(spy).toHaveBeenCalled());
    await waitFor(() => expect(spy.mock.results.length).toBeGreaterThan(0));
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('is dismissed for that version only and remembers it', async () => {
    const user = userEvent.setup();
    const { unmount } = renderWithProviders(<UpdateNotice />, {
      provider: new FakeProvider({ version: newer }),
    });

    await user.click(await screen.findByRole('button', { name: 'Dismiss update notice' }));
    expect(screen.queryByText(/is available/)).toBeNull();
    expect(window.localStorage.getItem(UPDATE_DISMISSED_KEY)).toBe('2.3.0');
    unmount();

    // Same version after a reload: still dismissed.
    const second = renderWithProviders(<UpdateNotice />, {
      provider: new FakeProvider({ version: newer }),
    });
    await waitFor(() => expect(window.localStorage.getItem(UPDATE_DISMISSED_KEY)).toBe('2.3.0'));
    expect(screen.queryByText(/is available/)).toBeNull();
    second.unmount();

    // A newer release is announced again.
    renderWithProviders(<UpdateNotice />, {
      provider: new FakeProvider({ version: { ...newer, latest: '2.4.0' } }),
    });
    expect(await screen.findByText(/gintrack 2\.4\.0 is available/)).toBeInTheDocument();
  });

  it('still dismisses when storage is blocked', async () => {
    const user = userEvent.setup();
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    renderWithProviders(<UpdateNotice />, { provider: new FakeProvider({ version: newer }) });

    await user.click(await screen.findByRole('button', { name: 'Dismiss update notice' }));
    expect(screen.queryByText(/is available/)).toBeNull();
    vi.restoreAllMocks();
  });

  it('is hidden when the runtime cannot tell (browser-only mode)', async () => {
    const provider = new FakeProvider({ version: { ...newer, supported: false } });
    const spy = vi.spyOn(provider, 'getVersionStatus');
    renderWithProviders(<UpdateNotice />, { provider });

    await waitFor(() => expect(spy).toHaveBeenCalled());
    await spy.mock.results[0]?.value;
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('the browser provider reports the check as unsupported', async () => {
    const status = await new BrowserProvider().getVersionStatus();
    expect(status.supported).toBe(false);
    expect(status.updateAvailable).toBe(false);
  });
});
