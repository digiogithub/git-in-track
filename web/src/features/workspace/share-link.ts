/**
 * Links to a page of this companion as seen from outside the machine
 * (ADR-027, docs/07 §5.1).
 *
 * Two shapes, and the difference between them is the whole security model:
 *
 * - a **plain** link is just an address; whoever opens it still has to be given
 *   the access token before the API answers them;
 * - an **access** link carries `?token=…`, which the app consumes on first load
 *   and strips from the URL. It is a credential: anyone who opens it can read
 *   and write every repository the companion has open.
 *
 * Neither asks the API for anything. The token comes from this tab's session
 * storage and goes nowhere except the clipboard.
 */

/** The public address of a path on this companion, with no credential in it. */
export function shareLink(url: string, path = ''): string {
  const base = url.replace(/\/+$/, '');
  const suffix = path === '' ? '/' : `/${path.replace(/^\/+/, '')}`;
  return `${base}${suffix}`;
}

/** The same address with the access token appended. This is a credential. */
export function accessLink(url: string, token: string, path = ''): string {
  return `${shareLink(url, path)}?token=${encodeURIComponent(token)}`;
}
