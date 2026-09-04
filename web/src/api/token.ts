/**
 * Companion bearer token (docs/07-cli-and-api.md §5.1).
 *
 * `gintrack serve` prints a token on start and every route except
 * `GET /api/v1/health` requires it. Two ways in:
 *
 * - the companion serves the app with `?token=…` appended, which this module
 *   consumes on first load and immediately strips from the URL with
 *   `history.replaceState` so it never reaches the history stack, a bookmark or
 *   an `Referer` header;
 * - the user pastes it in Settings when the app runs on the Vite dev server.
 *
 * It is kept in `sessionStorage` (per tab, gone when the tab closes) rather
 * than `localStorage`: a token that outlives the session is a token that
 * outlives the companion that issued it.
 */

import type { Unsubscribe } from '@/api/provider';

/** Session storage key. Namespaced like every other key in the app. */
export const TOKEN_STORAGE_KEY = 'gintrack:companion-token';

/** Query parameter the companion appends when it serves the bundle. */
export const TOKEN_QUERY_PARAM = 'token';

type TokenListener = (token: string | null) => void;

const listeners = new Set<TokenListener>();

/** `undefined` means "not read from storage yet"; `null` means "no token". */
let cached: string | null | undefined;

function readStorage(): string | null {
  try {
    return globalThis.sessionStorage?.getItem(TOKEN_STORAGE_KEY) ?? null;
  } catch {
    // Private modes and sandboxed iframes throw on access.
    return null;
  }
}

function writeStorage(token: string | null): void {
  try {
    if (token === null) globalThis.sessionStorage?.removeItem(TOKEN_STORAGE_KEY);
    else globalThis.sessionStorage?.setItem(TOKEN_STORAGE_KEY, token);
  } catch {
    // The in-memory copy still serves this session.
  }
}

function emit(token: string | null): void {
  for (const listener of [...listeners]) listener(token);
}

/** The token for this session, or `null` when none is known. */
export function getToken(): string | null {
  if (cached === undefined) cached = readStorage();
  return cached;
}

export function hasToken(): boolean {
  return getToken() !== null;
}

/** Stores a token and notifies listeners. An empty string clears it. */
export function setToken(token: string): void {
  const value = token.trim();
  if (value === '') {
    clearToken();
    return;
  }
  if (cached === value) return;
  cached = value;
  writeStorage(value);
  emit(value);
}

/**
 * Forgets the token. Called on a `401` so the next render can ask for a new
 * one instead of retrying a rejected credential forever.
 */
export function clearToken(): void {
  if (cached === null) return;
  cached = null;
  writeStorage(null);
  emit(null);
}

/** Notifies on every token change, including the clear caused by a `401`. */
export function onTokenChange(listener: TokenListener): Unsubscribe {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Reads `?token=…` on first load, persists it and rewrites the URL without it.
 * Returns the token it found, or `null` when the URL carried none.
 */
export function captureTokenFromUrl(): string | null {
  const location = globalThis.location as Location | undefined;
  if (!location) return null;

  let url: URL;
  try {
    url = new URL(location.href);
  } catch {
    return null;
  }

  const token = url.searchParams.get(TOKEN_QUERY_PARAM);
  if (token === null) return null;

  url.searchParams.delete(TOKEN_QUERY_PARAM);
  try {
    globalThis.history?.replaceState(
      globalThis.history.state,
      '',
      `${url.pathname}${url.search}${url.hash}`,
    );
  } catch {
    // Not being able to tidy the URL is not a reason to drop the token.
  }

  const value = token.trim();
  if (value === '') return null;
  setToken(value);
  return value;
}

/** `Authorization` header for REST calls; empty when no token is known. */
export function authorizationHeader(): Record<string, string> {
  const token = getToken();
  return token === null ? {} : { Authorization: `Bearer ${token}` };
}

/**
 * Browsers cannot set headers on `WebSocket`, so the token travels as the
 * documented `?token=` query parameter (docs/07-cli-and-api.md §5.1).
 */
export function withTokenQuery(url: string): string {
  const token = getToken();
  if (token === null) return url;
  const separator = url.includes('?') ? '&' : '?';
  return `${url}${separator}${TOKEN_QUERY_PARAM}=${encodeURIComponent(token)}`;
}

/** Test seam: drops the in-memory copy so the next read hits storage again. */
export function resetTokenCache(): void {
  cached = undefined;
}
