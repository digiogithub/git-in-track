import { describe, expect, it, vi } from 'vitest';

import { BrowserProvider } from '@/api/browser-provider';
import { CompanionProvider, companionCapabilities } from '@/api/companion-provider';
import { createDataProvider, disposeProvider, whenProviderReady } from '@/api/provider-factory';

/** Companion options that touch neither the network nor a WebSocket. */
const offline = {
  baseUrl: 'http://127.0.0.1:7317',
  fetchImpl: vi.fn() as unknown as typeof fetch,
  capabilities: companionCapabilities,
  webSocketFactory: null,
};

describe('createDataProvider', () => {
  it('builds a browser provider in browser-only mode', () => {
    const provider = createDataProvider({ mode: 'browser' });

    expect(provider).toBeInstanceOf(BrowserProvider);
    expect(provider.kind).toBe('browser');
  });

  it('builds a companion provider in companion mode', async () => {
    const provider = createDataProvider({ mode: 'companion', companion: offline });

    expect(provider).toBeInstanceOf(CompanionProvider);
    expect(provider.kind).toBe('companion');
    expect(provider.capabilities).toEqual(companionCapabilities);

    await whenProviderReady(provider);
    disposeProvider(provider);
  });

  it('stays on the browser provider while detection is still running', () => {
    expect(createDataProvider({ mode: 'detecting' })).toBeInstanceOf(BrowserProvider);
  });

  it('reports capabilities that match the browser, before any folder is open', () => {
    // jsdom has no File System Access API, so the safe answer is read-only.
    expect(createDataProvider({ mode: 'browser' }).capabilities).toMatchObject({
      write: false,
      git: false,
      maxBatchWrite: 0,
    });
  });
});
