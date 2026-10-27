import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { FakeProvider, type FakeData } from '@/api/fake-provider';
import { ProviderContext } from '@/api/provider-context';
import { clearToken, resetTokenCache, setToken } from '@/api/token';
import { recipientSummary } from '@/features/settings/tunnel-recipients';
import { TunnelCard } from '@/features/settings/TunnelCard';

/** Renders the card against a fake provider; the poll is sped up to be testable. */
function renderCard(data: FakeData = {}, pollIntervalMs = 5) {
  const provider = new FakeProvider(data);
  render(
    <ProviderContext.Provider value={provider}>
      <TunnelCard pollIntervalMs={pollIntervalMs} />
    </ProviderContext.Provider>,
  );
  return provider;
}

/** jsdom has no clipboard; this one records what the card copied. */
function stubClipboard() {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText },
    configurable: true,
    writable: true,
  });
  return writeText;
}

const SWITCH = { name: 'Share over a public URL' };

describe('recipientSummary', () => {
  it.each([
    [{ emails: 1, domains: 0 }, '1 allowed address'],
    [{ emails: 3, domains: 0 }, '3 allowed addresses'],
    [{ emails: 0, domains: 2 }, '2 domain rules'],
    [{ emails: 2, domains: 1 }, '2 allowed addresses and 1 domain rule'],
    [{ emails: 0, domains: 0 }, 'no allowed recipients'],
  ])('summarizes %o as %s', (recipients, want) => {
    expect(recipientSummary(recipients)).toBe(want);
  });
});

describe('TunnelCard', () => {
  beforeEach(() => {
    resetTokenCache();
    setToken('s3cret-token');
  });

  afterEach(() => {
    clearToken();
    resetTokenCache();
  });

  it('starts off, with nothing published and no address to share', async () => {
    renderCard();

    expect(await screen.findByText('Off')).toBeInTheDocument();
    expect(screen.getByRole('switch', SWITCH)).not.toBeChecked();
    expect(screen.queryByRole('button', { name: 'Copy public URL' })).toBeNull();
    expect(screen.queryByText(/on the public internet/)).toBeNull();
  });

  it('hides itself entirely on a runtime that cannot tunnel', async () => {
    const provider = renderCard({ tunnel: { supported: false } });

    // Let the mount read settle inside act, so the absence below is a decision
    // the card made and not a render that has not happened yet.
    await act(async () => {
      await provider.getTunnel();
    });
    await waitFor(() => {
      expect(screen.queryByText('Public tunnel')).toBeNull();
    });
    expect(screen.queryByRole('switch', SWITCH)).toBeNull();
  });

  it('shows the address as propagating while the tunnel is starting', async () => {
    // A poll far away, so the tunnel is observed in the state it is handed out
    // in: an address that exists and is not reachable yet.
    renderCard({}, 60_000);
    const toggle = await screen.findByRole('switch', SWITCH);

    await userEvent.click(toggle);

    expect(await screen.findByText(/still propagating through DNS/)).toBeInTheDocument();
    // The URL exists before the edge answers on it, so sharing it is refused.
    expect(screen.getByRole('button', { name: 'Copy public URL' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Copy link with access' })).toBeDisabled();
  });

  it('polls until the tunnel connects and then warns that the workspace is public', async () => {
    const provider = renderCard();

    await userEvent.click(await screen.findByRole('switch', SWITCH));

    expect(await screen.findByText('Public')).toBeInTheDocument();
    expect(screen.getByText(/This workspace is on the public internet\./)).toBeInTheDocument();
    expect(screen.queryByText(/still propagating through DNS/)).toBeNull();
    await expect(provider.getTunnel()).resolves.toMatchObject({ state: 'connected' });
  });

  it('says a public tunnel is not protected and how to protect it', async () => {
    renderCard();

    expect(await screen.findByText('Not protected by Cloudflare Access.')).toBeInTheDocument();
    expect(screen.getByText('server.tunnel.allowedEmails')).toBeInTheDocument();
    expect(screen.getByText(/--tunnel-allow-email/)).toBeInTheDocument();
  });

  it('shows a protected tunnel with its recipient counts, before and after it opens', async () => {
    renderCard({ tunnel: { protected: true, allowedRecipients: { emails: 2, domains: 1 } } });

    expect(await screen.findByText('Protected by Cloudflare Access.')).toBeInTheDocument();
    expect(screen.getByText(/2 allowed addresses and 1 domain rule/)).toBeInTheDocument();
    expect(screen.queryByText('Not protected by Cloudflare Access.')).toBeNull();

    await userEvent.click(screen.getByRole('switch', SWITCH));

    expect(await screen.findByText('Public')).toBeInTheDocument();
    // Still on the internet, still behind the token: the warning stays, worded
    // for the gate in front of it.
    expect(screen.getByText(/This workspace is on the public internet\./)).toBeInTheDocument();
    expect(screen.getByText(/Only visitors who pass the one-time PIN/)).toBeInTheDocument();
    expect(screen.getByText(/2 allowed addresses and 1 domain rule/)).toBeInTheDocument();
  });

  it('copies the bare URL, without the token in it', async () => {
    const writeText = stubClipboard();
    const provider = renderCard();

    await userEvent.click(await screen.findByRole('switch', SWITCH));
    await userEvent.click(await screen.findByRole('button', { name: 'Copy public URL' }));

    const { url } = await provider.getTunnel();
    expect(writeText).toHaveBeenCalledWith(url);
    expect(writeText.mock.calls[0]?.[0]).not.toContain('s3cret-token');
  });

  it('copies the access link with the session token and warns that it is a credential', async () => {
    const writeText = stubClipboard();
    const provider = renderCard();

    await userEvent.click(await screen.findByRole('switch', SWITCH));
    await userEvent.click(await screen.findByRole('button', { name: 'Copy link with access' }));

    const { url } = await provider.getTunnel();
    expect(writeText).toHaveBeenCalledWith(`${url}/?token=s3cret-token`);
    expect(screen.getByText(/This link is a credential\./)).toBeInTheDocument();
  });

  it('offers no access link when this tab holds no token', async () => {
    clearToken();
    renderCard();

    await userEvent.click(await screen.findByRole('switch', SWITCH));

    expect(await screen.findByRole('button', { name: 'Copy link with access' })).toBeDisabled();
    expect(screen.getByText(/No token is stored in this tab/)).toBeInTheDocument();
  });

  it('explains the refusal when the companion runs without authentication', async () => {
    const provider = renderCard({ tunnel: { tokenConfigured: false } });

    await userEvent.click(await screen.findByRole('switch', SWITCH));

    expect(await screen.findByText(/started with authentication disabled/)).toBeInTheDocument();
    await expect(provider.getTunnel()).resolves.toMatchObject({ state: 'off', url: '' });
  });

  it('turns the tunnel off again and drops the address', async () => {
    const provider = renderCard();
    const toggle = await screen.findByRole('switch', SWITCH);

    await userEvent.click(toggle);
    await screen.findByText('Public');
    await userEvent.click(screen.getByRole('switch', SWITCH));

    expect(await screen.findByText('Off')).toBeInTheDocument();
    expect(screen.queryByText(/on the public internet/)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Copy public URL' })).toBeNull();
    await expect(provider.getTunnel()).resolves.toMatchObject({ state: 'off', url: '' });
  });
});
