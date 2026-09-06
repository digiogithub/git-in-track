/**
 * The companion's git CORS proxy (docs/06-git-sync.md §6.3, story GIT-US-0042).
 *
 * Browser-only git cannot reach a git host directly: the smart-HTTP endpoints
 * send no `Access-Control-Allow-Origin`, so the tab is refused before it reads
 * a byte. A proxy that adds those headers is the only way in, and every proxy
 * on the public internet sees the traffic and any token sent with it.
 *
 * That is why this module exists: when the companion is running on the user's
 * own machine, it already forwards those three requests at `/cors-proxy/`, and
 * a proxy on `127.0.0.1` is the one proxy that adds no third party at all. It
 * becomes the default the moment the companion is detected, and it is the only
 * default there will ever be — a public proxy is always an explicit choice the
 * user types in Settings, never something this module picks for them.
 *
 * Two credentials are in play and they must not be confused:
 *
 * - the **companion's** bearer token authenticates the tab to the proxy and
 *   travels in `X-Gintrack-Token`. It is never sent anywhere else;
 * - the **git host's** credential is what isomorphic-git puts in
 *   `Authorization`, and the proxy forwards exactly that header upstream.
 *
 * Keeping them in separate headers is what lets the companion forward one and
 * strip the other.
 */

import { COMPANION_ORIGIN } from '@/api/detect';
import { getToken } from '@/api/token';
import { useAppStore } from '@/app/store';

/** Where the companion mounts the proxy. */
export const COMPANION_PROXY_PATH = '/cors-proxy';

/** The header the companion's own bearer token travels in. */
export const PROXY_TOKEN_HEADER = 'X-Gintrack-Token';

/**
 * The companion's proxy URL, or `null` when it cannot be used.
 *
 * It needs both halves: a detected companion and a token for it. Without the
 * token every proxied request would come back `401` and the user would see a
 * sync that fails instead of one that was never offered.
 */
export function companionProxyUrl(): string | null {
  const { mode, companionUrl } = useAppStore.getState();
  if (mode !== 'companion') return null;
  if (getToken() === null) return null;

  const base = companionUrl ?? '';
  if (base === '') {
    const origin = globalThis.location?.origin;
    return origin === undefined
      ? `${COMPANION_ORIGIN}${COMPANION_PROXY_PATH}`
      : `${origin}${COMPANION_PROXY_PATH}`;
  }
  return `${base.replace(/\/+$/, '')}${COMPANION_PROXY_PATH}`;
}

/** Whether a configured proxy URL is the companion's own. */
export function isCompanionProxy(proxy: string | undefined): boolean {
  if (proxy === undefined || proxy === '') return false;
  const own = companionProxyUrl();
  if (own === null) return false;
  return proxy.replace(/\/+$/, '') === own.replace(/\/+$/, '');
}

/**
 * The headers a request through `proxy` needs.
 *
 * The companion's token is added only for the companion's own proxy. Sending it
 * to a proxy someone else runs would hand that host a credential for the user's
 * machine, which is exactly the thing §6.3 refuses to do silently.
 */
export function proxyAuthHeaders(proxy: string | undefined): Record<string, string> {
  if (!isCompanionProxy(proxy)) return {};
  const token = getToken();
  return token === null ? {} : { [PROXY_TOKEN_HEADER]: token };
}

/** Why the companion's proxy is offered as the default. */
export const COMPANION_PROXY_REASON =
  'The companion is running on this machine and forwards git requests at ' +
  `${COMPANION_PROXY_PATH}/, so browser-only sync works with no extra infrastructure and no third party. ` +
  'It forwards only /info/refs, /git-upload-pack and /git-receive-pack, and only to the hosts your registered ' +
  'repositories already use.';

/** The outcome of a proxy preflight. */
export type ProxyPreflight = {
  ok: boolean;
  /** The HTTP status the proxy answered with, when it answered at all. */
  status?: number;
  /** A sentence to show the user; empty when the proxy works. */
  reason?: string;
};

/** What a preflight needs. */
export type PreflightOptions = {
  /** Injected for tests. */
  fetchImpl?: typeof fetch;
  /** Hard timeout; a proxy that hangs is a proxy that does not work. */
  timeoutMs?: number;
};

/** How long a preflight waits before calling the proxy unusable. */
export const PREFLIGHT_TIMEOUT_MS = 8000;

/**
 * Turns `https://host/org/repo.git` into the path isomorphic-git would build:
 * the proxy, then the remote with its scheme removed.
 */
export function proxyTarget(proxy: string, remoteUrl: string): string {
  const base = proxy.replace(/\/+$/, '');
  const rest = remoteUrl.replace(/^https?:\/\//, '').replace(/\/+$/, '');
  return `${base}/${rest}/info/refs?service=git-upload-pack`;
}

/**
 * Validates a proxy at save time by asking it for the repository's `info/refs`,
 * which is what §6.3 promises and what turns "the URL looks like a proxy" into
 * "the proxy answers for this repository".
 *
 * A `401` counts as a working proxy: the request reached the git host and the
 * host asked for a credential, which is the proxy doing its job. Anything else
 * is reported with the status, so the user sees "the proxy refuses this host"
 * rather than a sync that fails later for an unexplained reason.
 */
export async function preflightCorsProxy(
  proxy: string,
  remoteUrl: string,
  options: PreflightOptions = {},
): Promise<ProxyPreflight> {
  const { fetchImpl = globalThis.fetch, timeoutMs = PREFLIGHT_TIMEOUT_MS } = options;
  if (typeof fetchImpl !== 'function') {
    return { ok: false, reason: 'This runtime has no fetch, so the proxy cannot be checked.' };
  }
  if (!/^https?:\/\//.test(proxy)) {
    return { ok: false, reason: 'The CORS proxy must be an http:// or https:// URL.' };
  }
  if (!/^https?:\/\//.test(remoteUrl)) {
    return {
      ok: false,
      reason:
        'A proxy can only be checked against an HTTPS remote; an SSH remote cannot be used from a tab.',
    };
  }

  const controller = new AbortController();
  const timer = setTimeout(() => {
    controller.abort();
  }, timeoutMs);
  try {
    const response = await fetchImpl(proxyTarget(proxy, remoteUrl), {
      method: 'GET',
      mode: 'cors',
      credentials: 'omit',
      headers: proxyAuthHeaders(proxy),
      signal: controller.signal,
    });
    if (response.ok || response.status === 401) return { ok: true, status: response.status };
    return {
      ok: false,
      status: response.status,
      reason: `The proxy answered ${response.status} for ${remoteUrl}. ${explainStatus(response.status)}`,
    };
  } catch {
    return {
      ok: false,
      reason:
        'The proxy could not be reached, or it sent no CORS headers. Check the URL, and that the host it runs on is up.',
    };
  } finally {
    clearTimeout(timer);
  }
}

/** Turns the companion's own refusals into something a user can act on. */
function explainStatus(status: number): string {
  switch (status) {
    case 401:
      return 'It needs this companion’s bearer token; paste it in Settings → Runtime.';
    case 403:
      return 'It refuses this host. The companion forwards only to hosts your registered repositories use — add it to `git.corsProxy.allowedHosts`.';
    case 404:
      return 'The repository was not found at that URL.';
    case 501:
      return 'This companion has the CORS proxy switched off (`git.corsProxy.enabled: false`).';
    default:
      return 'Check the proxy’s own logs.';
  }
}
