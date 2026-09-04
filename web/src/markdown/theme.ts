/**
 * The effective colour scheme, for the pieces of the renderer that cannot be
 * themed with CSS variables alone (Mermaid renders to SVG with baked colours).
 *
 * Three states, as in `src/index.css`: an explicit choice stamps `data-theme`
 * on `<html>`, and the default "system" setting stamps nothing.
 */

import { useEffect, useState } from 'react';

export type ThemeMode = 'light' | 'dark';

export function readThemeMode(): ThemeMode {
  if (typeof document === 'undefined') return 'light';
  const explicit = document.documentElement.dataset['theme'];
  if (explicit === 'dark' || explicit === 'light') return explicit;
  return typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
}

export function useThemeMode(): ThemeMode {
  const [mode, setMode] = useState<ThemeMode>(readThemeMode);

  useEffect(() => {
    const update = () => setMode(readThemeMode());

    const media = window.matchMedia?.('(prefers-color-scheme: dark)');
    media?.addEventListener('change', update);

    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    });

    update();
    return () => {
      media?.removeEventListener('change', update);
      observer.disconnect();
    };
  }, []);

  return mode;
}
