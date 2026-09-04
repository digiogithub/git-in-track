/**
 * Browser-mode git credentials (story GIT-US-0023, docs/06-git-sync.md §8.2).
 *
 * A browser tab has no credential helper and no ssh-agent, so the only way to
 * reach a private repository over HTTPS is a personal access token. This module
 * is the whole of how one is handled, and it is deliberately small:
 *
 * - **Memory only.** The token lives in a module-level `Map` inside this
 *   closure. It is never written to `localStorage`, `sessionStorage`,
 *   IndexedDB, a cookie, a URL or a file — there is no code path here that can,
 *   and `credentials.test.ts` asserts on the storage APIs themselves.
 * - **Per session.** A reload starts an empty map, and `forgetCredentials()` is
 *   what sign-out, unmounting a repository and the "Forget tokens" button call.
 * - **Scoped to one remote.** The map is keyed by the origin the token was
 *   entered for, so a token given for `git.acme.test` is never offered to
 *   `github.com`.
 * - **Asked for only when a transport needs one.** Nothing prompts at mount
 *   time; the prompt opens when isomorphic-git calls `onAuth`, which happens
 *   when a host actually asks.
 * - **The CORS proxy is disclosed.** A browser fetch or push goes through the
 *   configured proxy, which therefore sees the request and its `Authorization`
 *   header. The prompt names that proxy before the token is typed (§6.3).
 *
 * Nothing here logs: a token must not reach the console either.
 */

import type { AuthCallback, AuthFailureCallback } from 'isomorphic-git';

import { redactUrl } from './browser-sync';

/** A credential for one host: HTTP basic, with the token as the password. */
export type GitCredential = {
  /** The username half the host expects next to the token. */
  username: string;
  /** The token. It is never persisted, logged or put in a URL. */
  token: string;
};

/** What the prompt has to tell the user before they type a token. */
export type CredentialRequest = {
  /** Identifies this request while it is pending. */
  id: number;
  /** The origin the token is for, and the key it is scoped to. */
  origin: string;
  /** The host, for the prompt's wording. */
  host: string;
  /** The remote URL, already redacted. */
  remoteUrl: string;
  /** The username the host expects; the user can change it. */
  suggestedUsername: string;
  /** The configured CORS proxy this credential would travel through. */
  corsProxy?: string;
};

/** The reason an auth prompt was dismissed rather than answered. */
export const CANCELLED_MESSAGE =
  'No token was provided for this repository, so nothing was fetched or pushed. ' +
  'Your files are untouched.';

/** In-memory store, keyed by origin. Cleared by a reload and by sign-out. */
const credentials = new Map<string, GitCredential>();

/** Requests waiting for the prompt to answer them. */
const pending = new Map<number, (credential: GitCredential | null) => void>();

/** Subscribers rendering the prompt. */
const listeners = new Set<(requests: CredentialRequest[]) => void>();

/** The open requests, in the order they were made. */
let queue: CredentialRequest[] = [];

let nextId = 1;

/**
 * The origin a credential is scoped to. Anything that is not an absolute HTTP
 * URL gets no scope at all, which means no stored credential can match it.
 */
export function credentialScope(url: string): string {
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return '';
    return parsed.origin;
  } catch {
    return '';
  }
}

/** The host a credential is for, for the prompt's wording. */
export function credentialHost(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

/**
 * The username a host expects next to a token in HTTP basic auth, matching what
 * the companion uses (docs/06 §7.3). The user can override it in the prompt.
 */
export function suggestedUsername(host: string): string {
  if (host.includes('github')) return 'x-access-token';
  if (host.includes('gitlab')) return 'oauth2';
  return 'token';
}

/** The credential remembered for a URL's origin, if any. */
export function getSessionCredential(url: string): GitCredential | undefined {
  const scope = credentialScope(url);
  return scope === '' ? undefined : credentials.get(scope);
}

/** Remembers a credential for one origin, for this session only. */
export function setSessionCredential(url: string, credential: GitCredential): void {
  const scope = credentialScope(url);
  if (scope === '') return;
  credentials.set(scope, credential);
}

/** Forgets the credential of one origin, after the host refused it. */
export function forgetCredential(url: string): void {
  const scope = credentialScope(url);
  if (scope !== '') credentials.delete(scope);
}

/**
 * Forgets every credential of this session. Sign-out, unmounting a repository
 * and closing the tab all end here; a reload needs no help, since the map only
 * ever existed in memory.
 */
export function forgetCredentials(): void {
  credentials.clear();
  for (const [id, resolve] of pending) {
    pending.delete(id);
    resolve(null);
  }
  queue = [];
  emit();
}

/** How many origins have a token in memory. The UI shows the count, not the token. */
export function sessionCredentialCount(): number {
  return credentials.size;
}

/** Subscribes to the pending-prompt queue. */
export function onCredentialRequests(
  listener: (requests: CredentialRequest[]) => void,
): () => void {
  listeners.add(listener);
  listener(queue);
  return () => {
    listeners.delete(listener);
  };
}

function emit(): void {
  const snapshot = [...queue];
  for (const listener of [...listeners]) listener(snapshot);
}

/** Answers a pending request with a credential the user typed. */
export function resolveCredentialRequest(id: number, credential: GitCredential): void {
  const resolve = pending.get(id);
  if (!resolve) return;
  pending.delete(id);
  queue = queue.filter((request) => request.id !== id);
  emit();
  resolve(credential);
}

/** Dismisses a pending request; the operation then fails as unauthenticated. */
export function rejectCredentialRequest(id: number): void {
  const resolve = pending.get(id);
  if (!resolve) return;
  pending.delete(id);
  queue = queue.filter((request) => request.id !== id);
  emit();
  resolve(null);
}

/** Opens a prompt for one URL and resolves with what the user typed. */
function askForCredential(url: string, corsProxy?: string): Promise<GitCredential | null> {
  const host = credentialHost(url);
  const request: CredentialRequest = {
    id: nextId++,
    origin: credentialScope(url),
    host,
    remoteUrl: redactUrl(url),
    suggestedUsername: suggestedUsername(host),
    ...(corsProxy === undefined || corsProxy === '' ? {} : { corsProxy }),
  };
  return new Promise<GitCredential | null>((resolve) => {
    pending.set(request.id, resolve);
    queue = [...queue, request];
    emit();
  });
}

/**
 * The `onAuth` isomorphic-git calls when a host asks for a credential.
 *
 * It answers from memory when this origin already has one, and otherwise opens
 * the prompt. Cancelling returns `{ cancel: true }`, which makes the library
 * fail the operation instead of retrying forever.
 */
export function createAuthCallback(opts: { corsProxy?: string } = {}): AuthCallback {
  return async (url) => {
    const known = getSessionCredential(url);
    if (known) return { username: known.username, password: known.token };
    const supplied = await askForCredential(url, opts.corsProxy);
    if (!supplied) return { cancel: true };
    setSessionCredential(url, supplied);
    return { username: supplied.username, password: supplied.token };
  };
}

/**
 * The `onAuthFailure` isomorphic-git calls when the host refused what we sent.
 * The rejected credential is dropped so the next attempt asks again rather than
 * replaying something the host already said no to.
 */
export function createAuthFailureCallback(): AuthFailureCallback {
  return (url) => {
    forgetCredential(url);
    return { cancel: true };
  };
}

// A closing tab must not leave a token reachable to anything that outlives the
// page, and a bfcache restore must start from an empty map.
if (typeof globalThis.addEventListener === 'function') {
  globalThis.addEventListener('pagehide', () => {
    forgetCredentials();
  });
}
