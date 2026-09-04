import { useEffect, useState } from 'react';

/** Resolves the effective colour scheme: `data-theme` wins over the media query. */
function resolveDark(): boolean {
  if (typeof document === 'undefined') return false;
  const explicit = document.documentElement.dataset.theme;
  if (explicit === 'dark') return true;
  if (explicit === 'light') return false;
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
}

/**
 * Tracks the app theme the way `src/index.css` defines it: an explicit
 * `data-theme` attribute on `<html>`, otherwise `prefers-color-scheme`.
 */
export function useIsDarkTheme(): boolean {
  const [dark, setDark] = useState(resolveDark);

  useEffect(() => {
    const update = () => {
      setDark(resolveDark());
    };
    const media = window.matchMedia?.('(prefers-color-scheme: dark)');
    media?.addEventListener?.('change', update);
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    });
    update();
    return () => {
      media?.removeEventListener?.('change', update);
      observer.disconnect();
    };
  }, []);

  return dark;
}
