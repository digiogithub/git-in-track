import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { companionCapabilities } from '@/api/companion-provider';
import { resetTokenCache } from '@/api/token';
import { useAppStore } from '@/app/store';
import { SettingsPage } from '@/features/settings/SettingsPage';

describe('SettingsPage runtime section', () => {
  beforeEach(() => {
    useAppStore.getState().reset();
    globalThis.sessionStorage.clear();
    resetTokenCache();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('explains browser-only mode and offers the companion', async () => {
    useAppStore.getState().setMode('browser', null);
    const fetchImpl = vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch'));
    vi.stubGlobal('fetch', fetchImpl);

    render(<SettingsPage />);

    expect(screen.getByText('browser')).toBeInTheDocument();
    expect(screen.getByText('not detected')).toBeInTheDocument();
    expect(screen.getByText(/Install the companion/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Download gintrack/ })).toHaveAttribute(
      'href',
      'https://github.com/digiogithub/git-in-track/releases',
    );
    expect(screen.queryByLabelText('Token')).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }));

    await waitFor(() => {
      expect(fetchImpl).toHaveBeenCalled();
    });
    expect(fetchImpl.mock.calls[0]?.[0]).toMatch(/\/api\/v1\/health$/);
  });

  it('reports the companion version, URL, socket state and capabilities', () => {
    const store = useAppStore.getState();
    store.setMode('companion', '0.4.0');
    store.setCompanionUrl('http://127.0.0.1:7317');
    store.setConnection('open');
    store.setCapabilities({
      ...companionCapabilities,
      fullTextSearch: 'bleve',
      maxBatchWrite: 200,
    });

    render(<SettingsPage />);

    expect(screen.getByText('companion')).toBeInTheDocument();
    expect(screen.getByText('0.4.0')).toBeInTheDocument();
    expect(screen.getByText('http://127.0.0.1:7317')).toBeInTheDocument();
    expect(screen.getByText('Live (WebSocket)')).toBeInTheDocument();
    expect(screen.getByText('bleve')).toBeInTheDocument();
    expect(screen.getByText('200')).toBeInTheDocument();
    expect(screen.queryByText(/Install the companion/)).not.toBeInTheDocument();
  });

  it('names the degraded event stream instead of failing silently', () => {
    const store = useAppStore.getState();
    store.setMode('companion', '0.4.0');
    store.setConnection('polling');

    render(<SettingsPage />);

    expect(screen.getByText('Polling (event socket unavailable)')).toBeInTheDocument();
  });

  it('asks for the token when the companion rejected it', async () => {
    const store = useAppStore.getState();
    store.setMode('companion', '0.4.0');
    store.setCompanionAuth('required');

    render(<SettingsPage />);

    expect(screen.getByRole('alert')).toHaveTextContent(/rejected or is missing the access token/);
    expect(screen.getByRole('button', { name: 'Save token' })).toBeDisabled();

    await userEvent.type(screen.getByLabelText('Token'), 's7Q1e9Zk');
    await userEvent.click(screen.getByRole('button', { name: 'Save token' }));

    expect(globalThis.sessionStorage.getItem('gintrack:companion-token')).toBe('s7Q1e9Zk');
    expect(await screen.findByRole('button', { name: 'Forget token' })).toBeInTheDocument();
  });
});
