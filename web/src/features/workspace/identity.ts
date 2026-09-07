import { useCallback, useSyncExternalStore } from 'react';

/**
 * Who this browser is, for the writes a session records against a person:
 * retro notes, retro votes and retro comments (docs/04 §9.1).
 *
 * A companion shared over a tunnel is opened by people the workspace has no
 * account for — there is no login, and there never will be, because the product
 * has no server-side identity. What it has instead is a handle each visitor
 * chooses once, kept in `localStorage` so the same laptop is the same person
 * across sessions, and sent as the author of every write they make. It is a
 * label, not a credential: it says whose sticky note this is, and nothing else.
 */

/** Key the chosen handle is remembered under, per browser profile. */
export const IDENTITY_STORAGE_KEY = 'gintrack:identity';

/** The shape a handle must have to pass the retro's own validation. */
const HANDLE_RE = /^[a-z0-9][a-z0-9-]{0,31}$/;

type Listener = () => void;

const listeners = new Set<Listener>();

/** `undefined` means "not read from storage yet"; `''` means "not chosen". */
let cached: string | undefined;

function read(): string {
  try {
    return globalThis.localStorage?.getItem(IDENTITY_STORAGE_KEY) ?? '';
  } catch {
    // Private modes and sandboxed iframes throw on access.
    return '';
  }
}

function write(handle: string): void {
  try {
    if (handle === '') globalThis.localStorage?.removeItem(IDENTITY_STORAGE_KEY);
    else globalThis.localStorage?.setItem(IDENTITY_STORAGE_KEY, handle);
  } catch {
    // The in-memory copy still serves this tab.
  }
}

/**
 * Turns whatever a person typed into a handle the retro file accepts: "José F.
 * Rives" becomes `jose-f-rives`. A name that reduces to nothing keeps the
 * caller honest by returning the empty string rather than a made-up handle.
 */
export function toHandle(name: string): string {
  const slug = name
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32)
    .replace(/-+$/g, '');
  return HANDLE_RE.test(slug) ? slug : '';
}

/** The handle this browser writes as, or `''` when nobody chose one yet. */
export function getIdentity(): string {
  if (cached === undefined) cached = read();
  return cached;
}

/** Remembers a handle for this browser. An empty name forgets the current one. */
export function setIdentity(name: string): void {
  const handle = toHandle(name);
  if (handle === cached) return;
  cached = handle;
  write(handle);
  for (const listener of [...listeners]) listener();
}

function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export type Identity = {
  /** The handle every write of this browser is attributed to; `''` when unset. */
  handle: string;
  /** True once somebody named themselves: what gates writing at all. */
  known: boolean;
  /** Chooses a handle. The name is slugified before it is stored. */
  identify: (name: string) => void;
  /** Forgets the handle, so the next visitor of this browser names themselves. */
  forget: () => void;
};

/** The identity of this browser, re-rendering every reader when it changes. */
export function useIdentity(): Identity {
  const handle = useSyncExternalStore(subscribe, getIdentity, () => '');
  const identify = useCallback((name: string) => setIdentity(name), []);
  const forget = useCallback(() => setIdentity(''), []);
  return { handle, known: handle !== '', identify, forget };
}

/** Test seam: drops the in-memory copy so the next read hits storage again. */
export function resetIdentityCache(): void {
  cached = undefined;
}
