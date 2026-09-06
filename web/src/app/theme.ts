/**
 * Theme preference: light, dark or system.
 *
 * The *tokens* already support all three (src/index.css); this is the piece
 * that lets a person choose. An explicit choice stamps `data-theme` on <html>
 * and is remembered in localStorage; `system` removes the attribute and hands
 * the decision back to `prefers-color-scheme`.
 *
 * The same key is read by the inline script in `index.html` before first paint,
 * which is what keeps a dark-mode user from being flashed a bright page on
 * every load. Change the key here and there together.
 */

import { useCallback, useSyncExternalStore } from 'react';

export type ThemePreference = 'light' | 'dark' | 'system';

export const THEME_STORAGE_KEY = 'gintrack:theme';

const listeners = new Set<() => void>();

function isPreference(value: string | null): value is ThemePreference {
  return value === 'light' || value === 'dark' || value === 'system';
}

/** The stored choice, or `system` when nothing was chosen or storage is barred. */
export function readThemePreference(): ThemePreference {
  try {
    const stored = globalThis.localStorage?.getItem(THEME_STORAGE_KEY) ?? null;
    return isPreference(stored) ? stored : 'system';
  } catch {
    // Private modes and sandboxed frames throw on access; `system` is correct.
    return 'system';
  }
}

/** Stamps (or clears) `data-theme`. Everything else is a token swap in CSS. */
export function applyThemePreference(preference: ThemePreference): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (preference === 'system') delete root.dataset['theme'];
  else root.dataset['theme'] = preference;
}

export function setThemePreference(preference: ThemePreference): void {
  try {
    if (preference === 'system') globalThis.localStorage?.removeItem(THEME_STORAGE_KEY);
    else globalThis.localStorage?.setItem(THEME_STORAGE_KEY, preference);
  } catch {
    // The choice still applies to this tab; it just will not outlive it.
  }
  applyThemePreference(preference);
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  // A second tab changing the preference should not leave this one behind.
  const onStorage = (event: StorageEvent) => {
    if (event.key !== THEME_STORAGE_KEY) return;
    applyThemePreference(readThemePreference());
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
}

/** `[preference, setPreference]`, kept in step with storage and other tabs. */
export function useThemePreference(): [ThemePreference, (next: ThemePreference) => void] {
  const preference = useSyncExternalStore(subscribe, readThemePreference, () => 'system' as const);
  const set = useCallback((next: ThemePreference) => {
    setThemePreference(next);
  }, []);
  return [preference, set];
}
