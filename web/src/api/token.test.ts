import { beforeEach, describe, expect, it, vi } from 'vitest';

import {
  authorizationHeader,
  captureTokenFromUrl,
  clearToken,
  getToken,
  hasToken,
  onTokenChange,
  resetTokenCache,
  setToken,
  TOKEN_STORAGE_KEY,
  withTokenQuery,
} from '@/api/token';

beforeEach(() => {
  globalThis.sessionStorage.clear();
  resetTokenCache();
  globalThis.history.replaceState({}, '', '/');
});

describe('captureTokenFromUrl', () => {
  it('reads ?token=, persists it and strips it from the URL', () => {
    globalThis.history.replaceState({}, '', '/p/ACME/items?token=s7Q1e9Zk&status=todo#top');

    expect(captureTokenFromUrl()).toBe('s7Q1e9Zk');
    expect(getToken()).toBe('s7Q1e9Zk');
    expect(globalThis.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBe('s7Q1e9Zk');
    // The rest of the URL survives; only the credential is gone.
    expect(globalThis.location.search).toBe('?status=todo');
    expect(globalThis.location.hash).toBe('#top');
  });

  it('returns null and keeps the URL when there is no token', () => {
    globalThis.history.replaceState({}, '', '/settings?tab=runtime');

    expect(captureTokenFromUrl()).toBeNull();
    expect(hasToken()).toBe(false);
    expect(globalThis.location.search).toBe('?tab=runtime');
  });
});

describe('token storage', () => {
  it('survives a reload within the session and clears on demand', () => {
    setToken('abc');
    resetTokenCache();
    expect(getToken()).toBe('abc');

    clearToken();
    expect(getToken()).toBeNull();
    expect(globalThis.sessionStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull();
  });

  it('notifies listeners on every change', () => {
    const listener = vi.fn();
    const stop = onTokenChange(listener);

    setToken('abc');
    setToken('abc');
    clearToken();
    stop();
    setToken('def');

    expect(listener.mock.calls).toEqual([['abc'], [null]]);
  });

  it('exposes the token as a header and as a WebSocket query parameter', () => {
    expect(authorizationHeader()).toEqual({});
    expect(withTokenQuery('ws://127.0.0.1:7317/api/v1/events')).toBe(
      'ws://127.0.0.1:7317/api/v1/events',
    );

    setToken('s7Q1e/9Zk');

    expect(authorizationHeader()).toEqual({ Authorization: 'Bearer s7Q1e/9Zk' });
    expect(withTokenQuery('ws://127.0.0.1:7317/api/v1/events')).toBe(
      'ws://127.0.0.1:7317/api/v1/events?token=s7Q1e%2F9Zk',
    );
    expect(withTokenQuery('ws://x/events?a=1')).toBe('ws://x/events?a=1&token=s7Q1e%2F9Zk');
  });
});
