/**
 * The companion's CORS proxy as the zero-configuration default (GIT-US-0042,
 * docs/06-git-sync.md §6.3).
 *
 * The tests that matter here are the refusals: the companion's token must reach
 * the companion's proxy and nothing else, and a proxy must never be adopted for
 * a tab that has no companion or no token — a sync that would be offered and
 * then fail with a 401 is worse than one that says why it cannot run.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { clearToken, resetTokenCache, setToken } from '@/api/token';
import { useAppStore } from '@/app/store';
import {
  COMPANION_PROXY_PATH,
  PROXY_TOKEN_HEADER,
  companionProxyUrl,
  isCompanionProxy,
  preflightCorsProxy,
  proxyAuthHeaders,
  proxyTarget,
} from '@/git/companion-proxy';
import { readSyncSettings, writeSyncSettings } from '@/git/settings-store';

const COMPANION = 'http://127.0.0.1:7317';

function asCompanion(): void {
  useAppStore.getState().setMode('companion', '0.4.0');
  useAppStore.getState().setCompanionUrl(COMPANION);
  setToken('secret-token');
}

beforeEach(() => {
  resetTokenCache();
  clearToken();
  useAppStore.getState().setMode('browser', null);
  useAppStore.getState().setCompanionUrl(null);
  globalThis.localStorage?.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('companionProxyUrl', () => {
  it('is null in browser-only mode with no companion', () => {
    expect(companionProxyUrl()).toBeNull();
  });

  it('is null when the companion is detected but no token is known', () => {
    useAppStore.getState().setMode('companion', '0.4.0');
    useAppStore.getState().setCompanionUrl(COMPANION);
    expect(companionProxyUrl()).toBeNull();
  });

  it('is the companion mount point once both halves are present', () => {
    asCompanion();
    expect(companionProxyUrl()).toBe(`${COMPANION}${COMPANION_PROXY_PATH}`);
  });

  it('tolerates a trailing slash on the companion URL', () => {
    asCompanion();
    useAppStore.getState().setCompanionUrl(`${COMPANION}/`);
    expect(companionProxyUrl()).toBe(`${COMPANION}${COMPANION_PROXY_PATH}`);
  });
});

describe('proxyAuthHeaders', () => {
  it('sends the companion token only to the companion proxy', () => {
    asCompanion();
    expect(proxyAuthHeaders(`${COMPANION}${COMPANION_PROXY_PATH}`)).toEqual({
      [PROXY_TOKEN_HEADER]: 'secret-token',
    });
  });

  it('never sends it to a third-party proxy', () => {
    asCompanion();
    expect(proxyAuthHeaders('https://cors.isomorphic-git.org')).toEqual({});
    expect(proxyAuthHeaders('https://proxy.evil.test/cors-proxy')).toEqual({});
    expect(proxyAuthHeaders(undefined)).toEqual({});
  });

  it('never puts the companion token in Authorization', () => {
    asCompanion();
    const headers = proxyAuthHeaders(`${COMPANION}${COMPANION_PROXY_PATH}`);
    expect(Object.keys(headers)).not.toContain('Authorization');
  });

  it('recognizes the companion proxy whatever the trailing slash', () => {
    asCompanion();
    expect(isCompanionProxy(`${COMPANION}${COMPANION_PROXY_PATH}/`)).toBe(true);
    expect(isCompanionProxy('https://proxy.example.test')).toBe(false);
  });
});

describe('the sync settings adopt the companion proxy', () => {
  it('stays unsupported with no companion and no configured proxy', () => {
    const settings = readSyncSettings('adopt-test');
    expect(settings.supported).toBe(false);
    expect(settings.proxySource).toBe('none');
    expect(settings.corsProxy).toBeUndefined();
  });

  it('becomes supported the moment the companion is detected', () => {
    asCompanion();
    const settings = readSyncSettings('adopt-test');
    expect(settings.supported).toBe(true);
    expect(settings.proxySource).toBe('companion');
    expect(settings.corsProxy).toBe(`${COMPANION}${COMPANION_PROXY_PATH}`);
  });

  it('lets a configured proxy win over the companion', () => {
    asCompanion();
    const saved = writeSyncSettings({ corsProxy: 'https://proxy.example.test' }, 'adopt-test');
    expect(saved.proxySource).toBe('configured');
    expect(saved.corsProxy).toBe('https://proxy.example.test');
  });

  it('falls back to the companion when the configured proxy is cleared', () => {
    asCompanion();
    writeSyncSettings({ corsProxy: 'https://proxy.example.test' }, 'adopt-test');
    const cleared = writeSyncSettings({ corsProxy: '' }, 'adopt-test');
    expect(cleared.proxySource).toBe('companion');
    expect(cleared.corsProxy).toBe(`${COMPANION}${COMPANION_PROXY_PATH}`);
    // The companion's URL must not have been frozen into storage: a tab that
    // later runs without the companion has to be unsupported again.
    useAppStore.getState().setMode('browser', null);
    useAppStore.getState().setCompanionUrl(null);
    expect(readSyncSettings('adopt-test').proxySource).toBe('none');
  });

  it('never adopts a public proxy on its own', () => {
    const settings = readSyncSettings('adopt-test');
    expect(settings.corsProxy).toBeUndefined();
    expect(JSON.stringify(settings)).not.toContain('isomorphic-git.org');
  });
});

describe('preflightCorsProxy', () => {
  const remote = 'https://git.acme.test/acme/web.git';

  it('builds the URL isomorphic-git would build', () => {
    expect(proxyTarget('https://proxy.test/', remote)).toBe(
      'https://proxy.test/git.acme.test/acme/web.git/info/refs?service=git-upload-pack',
    );
  });

  it('accepts a proxy that answers the advertisement', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response('001e#', { status: 200 }));
    await expect(
      preflightCorsProxy('https://proxy.test', remote, { fetchImpl }),
    ).resolves.toMatchObject({
      ok: true,
    });
  });

  it('accepts a 401: the request reached the host, which asked for a credential', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response('', { status: 401 }));
    await expect(
      preflightCorsProxy('https://proxy.test', remote, { fetchImpl }),
    ).resolves.toMatchObject({
      ok: true,
      status: 401,
    });
  });

  it('reports a proxy that refuses the host', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(new Response('', { status: 403 }));
    const result = await preflightCorsProxy('https://proxy.test', remote, { fetchImpl });
    expect(result.ok).toBe(false);
    expect(result.reason).toContain('403');
  });

  it('reports a proxy that cannot be reached', async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new TypeError('failed to fetch'));
    const result = await preflightCorsProxy('https://proxy.test', remote, { fetchImpl });
    expect(result.ok).toBe(false);
    expect(result.reason).toContain('could not be reached');
  });

  it('refuses to check an SSH remote', async () => {
    const fetchImpl = vi.fn();
    const result = await preflightCorsProxy(
      'https://proxy.test',
      'git@git.acme.test:acme/web.git',
      {
        fetchImpl,
      },
    );
    expect(result.ok).toBe(false);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it('refuses a proxy that is not an http URL', async () => {
    const fetchImpl = vi.fn();
    const result = await preflightCorsProxy('proxy.test', remote, { fetchImpl });
    expect(result.ok).toBe(false);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it('sends the companion token when the proxy is the companion', async () => {
    asCompanion();
    const fetchImpl = vi.fn().mockResolvedValue(new Response('', { status: 200 }));
    await preflightCorsProxy(`${COMPANION}${COMPANION_PROXY_PATH}`, remote, { fetchImpl });
    const init = fetchImpl.mock.calls[0]?.[1] as RequestInit;
    expect(init.headers).toEqual({ [PROXY_TOKEN_HEADER]: 'secret-token' });
    expect(init.credentials).toBe('omit');
  });

  it('sends no token to a proxy the user configured', async () => {
    asCompanion();
    const fetchImpl = vi.fn().mockResolvedValue(new Response('', { status: 200 }));
    await preflightCorsProxy('https://proxy.example.test', remote, { fetchImpl });
    const init = fetchImpl.mock.calls[0]?.[1] as RequestInit;
    expect(init.headers).toEqual({});
  });
});
