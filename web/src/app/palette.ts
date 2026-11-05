/**
 * Colour palette preference (GIT-US-0206): a second dimension next to the
 * light/dark/system mode in `theme.ts`, never a replacement for it.
 *
 * A palette is a set of token overrides in `src/index.css`, keyed on
 * `data-palette` on <html>; every palette has a light and a dark variant, so
 * the mode keeps deciding which of the two applies. `default` stamps nothing
 * and stores nothing, exactly like `system` does for the mode: the warm palette
 * is the one the bare `:root` rules already describe.
 *
 * The key and the values are read again by `public/theme-boot.js` before first
 * paint, so a reload never flashes the default palette. Change them there and
 * here together.
 */

import { useCallback, useSyncExternalStore } from 'react';

export const PALETTES = ['default', 'high-contrast', 'cool', 'colour-blind'] as const;

export type PalettePreference = (typeof PALETTES)[number];

export const PALETTE_STORAGE_KEY = 'gintrack:palette';

const listeners = new Set<() => void>();

export function isPalette(value: string | null | undefined): value is PalettePreference {
  return (PALETTES as readonly string[]).includes(value ?? '');
}

/** The stored palette, or `default` when nothing valid was stored or storage is barred. */
export function readPalettePreference(): PalettePreference {
  try {
    const stored = globalThis.localStorage?.getItem(PALETTE_STORAGE_KEY) ?? null;
    return isPalette(stored) ? stored : 'default';
  } catch {
    // Private modes and sandboxed frames throw on access; `default` is correct.
    return 'default';
  }
}

/** Stamps (or clears) `data-palette`. Everything else is a token swap in CSS. */
export function applyPalettePreference(palette: PalettePreference): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (palette === 'default') delete root.dataset['palette'];
  else root.dataset['palette'] = palette;
}

export function setPalettePreference(palette: PalettePreference): void {
  try {
    if (palette === 'default') globalThis.localStorage?.removeItem(PALETTE_STORAGE_KEY);
    else globalThis.localStorage?.setItem(PALETTE_STORAGE_KEY, palette);
  } catch {
    // The choice still applies to this tab; it just will not outlive it.
  }
  applyPalettePreference(palette);
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  // A second tab changing the palette should not leave this one behind.
  const onStorage = (event: StorageEvent) => {
    if (event.key !== PALETTE_STORAGE_KEY) return;
    applyPalettePreference(readPalettePreference());
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
}

/** `[palette, setPalette]`, kept in step with storage and other tabs. */
export function usePalettePreference(): [PalettePreference, (next: PalettePreference) => void] {
  const palette = useSyncExternalStore(subscribe, readPalettePreference, () => 'default' as const);
  const set = useCallback((next: PalettePreference) => {
    setPalettePreference(next);
  }, []);
  return [palette, set];
}
