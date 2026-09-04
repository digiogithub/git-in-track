import { describe, expect, it } from 'vitest';

import { BrowserProvider } from '@/api/browser-provider';
import { createDataProvider } from '@/api/provider-factory';

describe('createDataProvider', () => {
  it('builds a browser provider in browser-only mode', () => {
    const provider = createDataProvider({ mode: 'browser' });

    expect(provider).toBeInstanceOf(BrowserProvider);
    expect(provider.kind).toBe('browser');
  });

  it('still builds a browser provider in companion mode until Phase 2 lands', () => {
    expect(createDataProvider({ mode: 'companion' })).toBeInstanceOf(BrowserProvider);
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
