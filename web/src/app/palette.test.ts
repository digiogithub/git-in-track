/// <reference types="node" />

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import {
  applyPalettePreference,
  PALETTE_STORAGE_KEY,
  PALETTES,
  readPalettePreference,
  setPalettePreference,
  usePalettePreference,
} from '@/app/palette';
import { THEME_STORAGE_KEY } from '@/app/theme';

// Vitest runs with `web/` as its root, so this is the shipped file.
const bootScript = readFileSync(resolve(process.cwd(), 'public/theme-boot.js'), 'utf8');

/** Runs the pre-paint bootstrap the way `index.html` does, on a fresh <html>. */
function boot(): void {
  delete document.documentElement.dataset['theme'];
  delete document.documentElement.dataset['palette'];
  // The shipped file, run as the browser runs it: as a classic script.
  // eslint-disable-next-line @typescript-eslint/no-implied-eval
  const run = new Function(bootScript) as () => void;
  run();
}

function reset(): void {
  localStorage.clear();
  delete document.documentElement.dataset['theme'];
  delete document.documentElement.dataset['palette'];
}

describe('palette preference', () => {
  beforeEach(reset);
  afterEach(reset);

  it('defaults to the default palette and stamps nothing', () => {
    expect(readPalettePreference()).toBe('default');
    applyPalettePreference(readPalettePreference());
    expect(document.documentElement.dataset['palette']).toBeUndefined();
  });

  it('remembers a palette under its own key and stamps data-palette', () => {
    setPalettePreference('high-contrast');
    expect(localStorage.getItem(PALETTE_STORAGE_KEY)).toBe('high-contrast');
    expect(document.documentElement.dataset['palette']).toBe('high-contrast');
    expect(PALETTE_STORAGE_KEY).not.toBe(THEME_STORAGE_KEY);

    // A reload: only what storage holds is left to go on.
    delete document.documentElement.dataset['palette'];
    applyPalettePreference(readPalettePreference());
    expect(document.documentElement.dataset['palette']).toBe('high-contrast');
  });

  it('removes the key and the attribute when going back to default', () => {
    setPalettePreference('cool');
    setPalettePreference('default');
    expect(localStorage.getItem(PALETTE_STORAGE_KEY)).toBeNull();
    expect(document.documentElement.dataset['palette']).toBeUndefined();
  });

  it.each(['neon', '', 'HIGH-CONTRAST', '{"palette":"cool"}'])(
    'falls back to default for a corrupt stored value %j',
    (value) => {
      localStorage.setItem(PALETTE_STORAGE_KEY, value);
      expect(readPalettePreference()).toBe('default');
    },
  );

  it('leaves the mode alone', () => {
    document.documentElement.dataset['theme'] = 'dark';
    setPalettePreference('colour-blind');
    expect(document.documentElement.dataset['theme']).toBe('dark');
  });

  it('follows a change made in another tab', () => {
    const { result } = renderHook(() => usePalettePreference());
    expect(result.current[0]).toBe('default');

    act(() => {
      localStorage.setItem(PALETTE_STORAGE_KEY, 'cool');
      window.dispatchEvent(new StorageEvent('storage', { key: PALETTE_STORAGE_KEY }));
    });
    expect(result.current[0]).toBe('cool');
    expect(document.documentElement.dataset['palette']).toBe('cool');

    // The theme key changing is not this store's business.
    act(() => {
      window.dispatchEvent(new StorageEvent('storage', { key: THEME_STORAGE_KEY }));
    });
    expect(result.current[0]).toBe('cool');
  });

  it('updates subscribers in this tab when set', () => {
    const { result } = renderHook(() => usePalettePreference());
    act(() => {
      result.current[1]('high-contrast');
    });
    expect(result.current[0]).toBe('high-contrast');
  });
});

describe('the pre-paint bootstrap and palettes', () => {
  beforeEach(reset);
  afterEach(reset);

  it('knows every palette the app can store', () => {
    for (const palette of PALETTES) {
      if (palette === 'default') continue;
      expect(bootScript).toContain(`'${palette}'`);
    }
    expect(bootScript).toContain(`'${PALETTE_STORAGE_KEY}'`);
  });

  it.each(PALETTES.filter((palette) => palette !== 'default'))(
    'stamps data-palette=%s before first paint',
    (palette) => {
      localStorage.setItem(PALETTE_STORAGE_KEY, palette);
      boot();
      expect(document.documentElement.dataset['palette']).toBe(palette);
    },
  );

  it('stamps nothing for a missing or corrupt palette', () => {
    boot();
    expect(document.documentElement.dataset['palette']).toBeUndefined();
    localStorage.setItem(PALETTE_STORAGE_KEY, 'neon"><script>');
    boot();
    expect(document.documentElement.dataset['palette']).toBeUndefined();
  });

  it('applies mode and palette together and independently', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'dark');
    localStorage.setItem(PALETTE_STORAGE_KEY, 'cool');
    boot();
    expect(document.documentElement.dataset['theme']).toBe('dark');
    expect(document.documentElement.dataset['palette']).toBe('cool');

    localStorage.setItem(THEME_STORAGE_KEY, 'bogus');
    boot();
    expect(document.documentElement.dataset['theme']).toBeUndefined();
    expect(document.documentElement.dataset['palette']).toBe('cool');
  });
});
