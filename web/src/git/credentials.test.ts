import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  createAuthCallback,
  createAuthFailureCallback,
  credentialScope,
  forgetCredentials,
  getSessionCredential,
  onCredentialRequests,
  rejectCredentialRequest,
  resolveCredentialRequest,
  sessionCredentialCount,
  suggestedUsername,
  type CredentialRequest,
} from './credentials';

/**
 * Browser-mode credentials (story GIT-US-0023, docs/06-git-sync.md §8.2).
 *
 * The last describe in this file is a milestone-5 exit criterion and is written
 * to be hard to regress: it spies on every storage API a browser has and fails
 * if a token so much as passes through one of them.
 */

/** The token every case uses. It must never leave memory. */
const TOKEN = 'ghp-test-token-value';

/** Collects the prompt queue the UI would render. */
function watchRequests(): { current: CredentialRequest[]; stop: () => void } {
  const state = { current: [] as CredentialRequest[], stop: () => undefined as void };
  state.stop = onCredentialRequests((requests) => {
    state.current = requests;
  });
  return state;
}

/** Answers the first pending request the way the dialog does. */
async function answerFirst(
  watcher: { current: CredentialRequest[] },
  token = TOKEN,
): Promise<CredentialRequest> {
  const request = await vi.waitFor(() => {
    const first = watcher.current[0];
    if (!first) throw new Error('no credential request is pending');
    return first;
  });
  resolveCredentialRequest(request.id, { username: request.suggestedUsername, token });
  return request;
}

describe('credentialScope', () => {
  const cases: { name: string; url: string; want: string }[] = [
    { name: 'an https remote scopes to its origin', url: 'https://git.test/a/b.git', want: 'https://git.test' },
    {
      name: 'the port is part of the scope',
      url: 'https://git.test:8443/a/b.git',
      want: 'https://git.test:8443',
    },
    { name: 'an ssh remote has no scope', url: 'git@github.com:a/b.git', want: '' },
    { name: 'nonsense has no scope', url: 'not a url', want: '' },
  ];
  for (const { name, url, want } of cases) {
    it(name, () => {
      expect(credentialScope(url)).toBe(want);
    });
  }
});

describe('suggestedUsername', () => {
  const cases: { host: string; want: string }[] = [
    { host: 'github.com', want: 'x-access-token' },
    { host: 'gitlab.com', want: 'oauth2' },
    { host: 'git.acme.test', want: 'token' },
  ];
  for (const { host, want } of cases) {
    it(`${host} expects ${want}`, () => {
      expect(suggestedUsername(host)).toBe(want);
    });
  }
});

describe('the session credential store', () => {
  let watcher: ReturnType<typeof watchRequests>;

  beforeEach(() => {
    forgetCredentials();
    watcher = watchRequests();
  });

  afterEach(() => {
    watcher.stop();
    forgetCredentials();
  });

  it('asks for nothing until a transport actually needs a credential', () => {
    createAuthCallback({ corsProxy: 'https://proxy.test' });
    expect(watcher.current).toHaveLength(0);
    expect(sessionCredentialCount()).toBe(0);
  });

  it('prompts on the first call and answers from memory afterwards', async () => {
    const onAuth = createAuthCallback();

    const first = onAuth('https://git.test/acme/web.git', {});
    await answerFirst(watcher);
    expect(await first).toEqual({ username: 'token', password: TOKEN });

    const second = await onAuth('https://git.test/acme/other.git', {});
    expect(second).toEqual({ username: 'token', password: TOKEN });
    expect(watcher.current).toHaveLength(0);
  });

  it('never offers a token to a host it was not entered for', async () => {
    const onAuth = createAuthCallback();

    const first = onAuth('https://git.test/acme/web.git', {});
    await answerFirst(watcher);
    await first;

    const other = onAuth('https://elsewhere.test/acme/web.git', {});
    const request = await vi.waitFor(() => {
      const pending = watcher.current[0];
      if (!pending) throw new Error('the other host must be asked about separately');
      return pending;
    });
    expect(request.host).toBe('elsewhere.test');
    expect(getSessionCredential('https://elsewhere.test/x.git')).toBeUndefined();
    rejectCredentialRequest(request.id);
    expect(await other).toEqual({ cancel: true });
  });

  it('tells the prompt which CORS proxy the token would travel through', async () => {
    const onAuth = createAuthCallback({ corsProxy: 'https://proxy.example.test' });
    const pending = onAuth('https://git.test/acme/web.git', {});
    const request = await answerFirst(watcher);
    expect(request.corsProxy).toBe('https://proxy.example.test');
    await pending;
  });

  it('redacts the remote URL it shows in the prompt', async () => {
    const onAuth = createAuthCallback();
    const pending = onAuth(`https://jose:${TOKEN}@git.test/acme/web.git`, {});
    const request = await answerFirst(watcher);
    expect(request.remoteUrl).toBe('https://***@git.test/acme/web.git');
    expect(request.remoteUrl).not.toContain(TOKEN);
    await pending;
  });

  it('cancels the operation when the prompt is dismissed', async () => {
    const onAuth = createAuthCallback();
    const pending = onAuth('https://git.test/acme/web.git', {});
    const request = await vi.waitFor(() => {
      const first = watcher.current[0];
      if (!first) throw new Error('no credential request is pending');
      return first;
    });
    rejectCredentialRequest(request.id);
    expect(await pending).toEqual({ cancel: true });
    expect(sessionCredentialCount()).toBe(0);
  });

  it('drops a credential the host refused instead of replaying it', async () => {
    const onAuth = createAuthCallback();
    const pending = onAuth('https://git.test/acme/web.git', {});
    await answerFirst(watcher);
    await pending;
    expect(sessionCredentialCount()).toBe(1);

    void createAuthFailureCallback()('https://git.test/acme/web.git', {});
    expect(sessionCredentialCount()).toBe(0);
    expect(getSessionCredential('https://git.test/acme/web.git')).toBeUndefined();
  });

  it('forgets everything on sign-out, pending prompts included', async () => {
    const onAuth = createAuthCallback();
    const kept = onAuth('https://git.test/acme/web.git', {});
    await answerFirst(watcher);
    await kept;

    const dangling = onAuth('https://other.test/acme/web.git', {});
    await vi.waitFor(() => {
      if (watcher.current.length === 0) throw new Error('the second prompt has not opened');
    });

    forgetCredentials();
    expect(await dangling).toEqual({ cancel: true });
    expect(sessionCredentialCount()).toBe(0);
    expect(watcher.current).toHaveLength(0);
  });
});

/**
 * The milestone-5 exit criterion: no credential is ever written to disk or to
 * `localStorage`. In a browser "disk" is the storage APIs, so every one of them
 * is spied on — including the ones this module does not even import, because
 * the point is to fail if a future change starts using one.
 */
describe('no credential is ever persisted', () => {
  const calls: string[] = [];
  let cookies: string[] = [];

  beforeEach(() => {
    forgetCredentials();
    calls.length = 0;
    cookies = [];

    vi.spyOn(Storage.prototype, 'setItem').mockImplementation((key: string, value: string) => {
      calls.push(`setItem:${key}=${value}`);
    });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation((key: string) => {
      calls.push(`getItem:${key}`);
      return null;
    });
    vi.stubGlobal('indexedDB', {
      open: (name: string) => {
        calls.push(`indexedDB.open:${name}`);
        return {};
      },
      databases: () => Promise.resolve([]),
    });
    vi.spyOn(document, 'cookie', 'set').mockImplementation((value: string) => {
      cookies.push(value);
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    forgetCredentials();
  });

  it('touches no storage API while prompting, storing and using a token', async () => {
    const watcher = watchRequests();
    const onAuth = createAuthCallback({ corsProxy: 'https://proxy.example.test' });

    const pending = onAuth('https://git.test/acme/web.git', {});
    await answerFirst(watcher);
    const auth = await pending;
    expect(auth).toEqual({ username: 'token', password: TOKEN });

    // Used again from memory, then forgotten.
    await onAuth('https://git.test/acme/web.git', {});
    forgetCredentials();
    watcher.stop();

    expect(calls).toEqual([]);
    expect(cookies).toEqual([]);
    for (const entry of [...calls, ...cookies]) {
      expect(entry).not.toContain(TOKEN);
    }
  });

  it('leaves nothing behind in localStorage or sessionStorage', async () => {
    // The spies above make a write visible; these assertions make a leak
    // visible even if some other module wrote it for us.
    const watcher = watchRequests();
    const onAuth = createAuthCallback();
    const pending = onAuth('https://git.test/acme/web.git', {});
    await answerFirst(watcher);
    await pending;
    watcher.stop();

    const serialized = JSON.stringify([
      { ...globalThis.localStorage },
      { ...globalThis.sessionStorage },
    ]);
    expect(serialized).not.toContain(TOKEN);
  });

  it('keeps the token out of the console', async () => {
    const spies = (['log', 'info', 'warn', 'error', 'debug'] as const).map((level) =>
      vi.spyOn(console, level).mockImplementation(() => undefined),
    );
    const watcher = watchRequests();
    const onAuth = createAuthCallback();
    const pending = onAuth('https://git.test/acme/web.git', {});
    await answerFirst(watcher);
    await pending;
    void createAuthFailureCallback()('https://git.test/acme/web.git', {});
    watcher.stop();

    for (const spy of spies) {
      for (const call of spy.mock.calls) {
        expect(JSON.stringify(call)).not.toContain(TOKEN);
      }
    }
  });
});
