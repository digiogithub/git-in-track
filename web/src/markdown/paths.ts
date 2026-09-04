/**
 * Vault path helpers. Pure string maths on POSIX-style, forward-slash paths:
 * the frontend never imports `node:path` (it runs in a browser tab).
 */

/** Directory part of a vault path (`docs/a/b.md` → `docs/a`, `index.md` → ''). */
export function dirname(path: string): string {
  const i = path.lastIndexOf('/');
  return i === -1 ? '' : path.slice(0, i);
}

/** File name part of a vault path (`docs/a/b.md` → `b.md`). */
export function basename(path: string): string {
  const i = path.lastIndexOf('/');
  return i === -1 ? path : path.slice(i + 1);
}

/** File name without its extension (`docs/a/b.md` → `b`). */
export function stem(path: string): string {
  const name = basename(path);
  const i = name.lastIndexOf('.');
  return i <= 0 ? name : name.slice(0, i);
}

/** Collapses `.` and `..` segments and duplicate slashes. */
export function normalizePath(path: string): string {
  const out: string[] = [];
  for (const segment of path.split('/')) {
    if (segment === '' || segment === '.') continue;
    if (segment === '..') {
      out.pop();
      continue;
    }
    out.push(segment);
  }
  return out.join('/');
}

/** Resolves `relative` against the directory of `basePath`. */
export function resolveFrom(basePath: string, relative: string): string {
  if (relative.startsWith('/')) return normalizePath(relative);
  const base = dirname(basePath);
  return normalizePath(base ? `${base}/${relative}` : relative);
}

const EXTERNAL = /^[a-z][a-z0-9+.-]*:/i;

/** True for `https://…`, `mailto:…`, `data:…` and protocol-relative `//host`. */
export function isExternalUrl(url: string): boolean {
  return EXTERNAL.test(url) || url.startsWith('//');
}

/** True for a link that stays inside the current document (`#section`). */
export function isFragmentUrl(url: string): boolean {
  return url.startsWith('#');
}
