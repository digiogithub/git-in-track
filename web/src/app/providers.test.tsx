import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { companionCapabilities } from '@/api/companion-provider';
import { FakeProvider } from '@/api/fake-provider';
import { useOptionalProvider } from '@/api/provider-context';
import { AppProviders, type DetectionOptions } from '@/app/providers';
import { useAppStore } from '@/app/store';

function ProviderProbe() {
  const provider = useOptionalProvider();
  return <p>{provider ? `provider: ${provider.kind}` : 'no provider'}</p>;
}

function NoticeProbe() {
  const notice = useAppStore((state) => state.modeNotice);
  return <p>{`notice: ${notice ?? 'none'}`}</p>;
}

function healthResponse(): Response {
  return {
    ok: true,
    status: 200,
    json: () => Promise.resolve({ status: 'ok', version: '0.4.0', uptimeSeconds: 12 }),
  } as unknown as Response;
}

/** Companion options that touch neither the network nor a WebSocket. */
function offlineDetection(fetchImpl: typeof fetch): DetectionOptions {
  return {
    fetchImpl,
    intervalMs: 20,
    companion: {
      baseUrl: 'http://127.0.0.1:7317',
      fetchImpl: vi.fn<typeof fetch>(),
      capabilities: companionCapabilities,
      webSocketFactory: null,
    },
  };
}

describe('AppProviders', () => {
  it('renders children against an injected provider', async () => {
    render(
      <AppProviders provider={new FakeProvider()}>
        <ProviderProbe />
      </AppProviders>,
    );

    expect(
      await screen.findByText('provider: browser', undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
  });

  it('detects the mode and builds a provider when none is injected', async () => {
    useAppStore.getState().reset();

    render(
      <AppProviders>
        <ProviderProbe />
      </AppProviders>,
    );

    // No companion answers in jsdom, so detection settles on browser-only mode.
    expect(
      await screen.findByText('provider: browser', undefined, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(useAppStore.getState().mode).toBe('browser');
    expect(useAppStore.getState().capabilities.write).toBe(false);
  });

  it('upgrades a running tab to the companion and announces it', async () => {
    useAppStore.getState().reset();
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValue(healthResponse());

    render(
      <AppProviders detection={offlineDetection(fetchImpl)}>
        <ProviderProbe />
        <NoticeProbe />
      </AppProviders>,
    );

    // Boot: no companion answers, so the browser provider is built.
    expect(await screen.findByText('provider: browser')).toBeInTheDocument();

    // A later probe finds `gintrack serve`: the provider is swapped in place.
    expect(await screen.findByText('provider: companion')).toBeInTheDocument();
    expect(await screen.findByText('notice: companion-detected')).toBeInTheDocument();
    await waitFor(() => {
      expect(useAppStore.getState().mode).toBe('companion');
    });
    expect(useAppStore.getState().companionVersion).toBe('0.4.0');
    expect(useAppStore.getState().companionUrl).toBe('http://127.0.0.1:7317');
    expect(useAppStore.getState().capabilities.watch).toBe(true);
  });

  it('downgrades again when the companion disappears', async () => {
    useAppStore.getState().reset();
    const fetchImpl = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(healthResponse())
      .mockRejectedValue(new TypeError('Failed to fetch'));

    render(
      <AppProviders detection={offlineDetection(fetchImpl)}>
        <ProviderProbe />
        <NoticeProbe />
      </AppProviders>,
    );

    expect(await screen.findByText('provider: companion')).toBeInTheDocument();
    expect(await screen.findByText('provider: browser')).toBeInTheDocument();
    expect(await screen.findByText('notice: companion-lost')).toBeInTheDocument();
  });
});
