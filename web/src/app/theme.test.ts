import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { describe, expect, it } from 'vitest';

import {
  applyThemePreference,
  readThemePreference,
  setThemePreference,
  THEME_STORAGE_KEY,
} from '@/app/theme';

// Vitest runs with `web/` as its root, so these are the shipped files.
function repoFile(relative: string): string {
  return readFileSync(resolve(process.cwd(), relative), 'utf8');
}

describe('theme preference', () => {
  it('remembers an explicit choice and stamps the attribute', () => {
    setThemePreference('light');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    expect(document.documentElement.dataset['theme']).toBe('light');

    // A reload: only what storage holds is left to go on.
    delete document.documentElement.dataset['theme'];
    applyThemePreference(readThemePreference());
    expect(document.documentElement.dataset['theme']).toBe('light');
  });

  it('hands `system` back to the media query', () => {
    setThemePreference('dark');
    setThemePreference('system');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    expect(document.documentElement.dataset['theme']).toBeUndefined();
  });
});

describe('the pre-paint bootstrap', () => {
  // The companion serves the app under `script-src 'self'` with no
  // 'unsafe-inline' (internal/server/server.go), so an inline bootstrap is
  // dropped without an error and every reload falls back to the media query.
  it('is a served file, not an inline script', () => {
    const html = repoFile('index.html');
    expect(html).toContain('src="/theme-boot.js"');
    expect(html).not.toMatch(/<script(?![^>]*\ssrc=)[^>]*>/);
  });

  it('reads the same storage key the app writes', () => {
    expect(repoFile('public/theme-boot.js')).toContain(`'${THEME_STORAGE_KEY}'`);
  });
});
