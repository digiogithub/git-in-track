import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { FakeProvider } from '@/api/fake-provider';
import { useOptionalProvider } from '@/api/provider-context';
import { AppProviders } from '@/app/providers';
import { useAppStore } from '@/app/store';

function ProviderProbe() {
  const provider = useOptionalProvider();
  return <p>{provider ? `provider: ${provider.kind}` : 'no provider'}</p>;
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
});
