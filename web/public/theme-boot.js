/*
 * Applies the stored theme before first paint, so a light-mode user is never
 * flashed a dark page — and, more importantly, so the choice survives a reload
 * at all: nothing else stamps `data-theme` early enough.
 *
 * This is a file rather than an inline script because the companion serves the
 * app under `script-src 'self'` (internal/server/server.go): an inline
 * bootstrap is dropped by the browser without a visible error, and every reload
 * silently falls back to `prefers-color-scheme`.
 *
 * The key and the three values are the contract in `src/app/theme.ts`; `system`
 * stores nothing and lets the media query decide. Change them together.
 */
(function () {
  try {
    var stored = window.localStorage.getItem('gintrack:theme');
    if (stored === 'dark' || stored === 'light') {
      document.documentElement.dataset.theme = stored;
    }
  } catch (error) {
    /* Storage can throw in private modes; the media query still works. */
  }
})();
