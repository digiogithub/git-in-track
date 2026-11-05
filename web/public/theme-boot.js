/*
 * Applies the stored theme and colour palette before first paint, so a
 * light-mode user is never flashed a dark page — and, more importantly, so the
 * choice survives a reload at all: nothing else stamps `data-theme` or
 * `data-palette` early enough.
 *
 * This is a file rather than an inline script because the companion serves the
 * app under `script-src 'self'` (internal/server/server.go): an inline
 * bootstrap is dropped by the browser without a visible error, and every reload
 * silently falls back to `prefers-color-scheme`.
 *
 * The keys and their values are the contract in `src/app/theme.ts` and
 * `src/app/palette.ts`; `system` and `default` store nothing and stamp
 * nothing. Anything else in storage (a typo, a palette from a newer build) is
 * ignored, which leaves the default palette. Change them together.
 */
(function () {
  var root = document.documentElement;
  try {
    var stored = window.localStorage.getItem('gintrack:theme');
    if (stored === 'dark' || stored === 'light') {
      root.dataset.theme = stored;
    }
  } catch (error) {
    /* Storage can throw in private modes; the media query still works. */
  }
  try {
    var palette = window.localStorage.getItem('gintrack:palette');
    if (palette === 'high-contrast' || palette === 'cool' || palette === 'colour-blind') {
      root.dataset.palette = palette;
    }
  } catch (error) {
    /* The bare `:root` tokens are the default palette. */
  }
})();
