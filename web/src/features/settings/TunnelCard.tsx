/**
 * Settings — Public tunnel (`gintrack serve` over a Cloudflare quick tunnel).
 *
 * The companion listens on `127.0.0.1` and has read and write access to the
 * user's real repositories. A tunnel puts exactly that on the public internet,
 * with one bearer token between a stranger and the working copies, so this card
 * is written as a security surface first and a convenience second:
 *
 * - nothing is enabled on its own; the switch is the only thing that opens it;
 * - while the tunnel is up the card carries a standing, unmissable notice that
 *   the workspace is publicly reachable;
 * - the prominent share action copies the bare URL, which is safe to paste
 *   anywhere. The link that carries the token is a second, quieter control with
 *   the warning next to it, and the token is read from this tab's session
 *   storage — it never travels in an API response and is never logged;
 * - the URL is known while the state is still `starting`, before DNS has
 *   propagated, so it is shown as "propagating" rather than as a live link.
 *
 * The hostname changes on every enable, so nothing here is cached: the card
 * reads the status, and polls it while the tunnel is coming up.
 */

import { useCallback, useEffect, useState } from 'react';

import type { TunnelStatus } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { getToken } from '@/api/token';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { accessLink } from '@/features/workspace/share-link';

/** How often a `starting` tunnel is re-read while it comes up. */
export const TUNNEL_POLL_INTERVAL_MS = 2000;

/** Reads before a tunnel that never settles is reported as stuck. */
export const TUNNEL_POLL_ATTEMPTS = 30;

const STATE_LABELS: Record<TunnelStatus['state'], string> = {
  off: 'Off',
  starting: 'Starting…',
  connected: 'Public',
  reconnecting: 'Reconnecting…',
  error: 'Failed',
};

/** What a screen reader hears when the state changes. */
const STATE_ANNOUNCEMENTS: Record<TunnelStatus['state'], string> = {
  off: 'The tunnel is off. This workspace is only reachable on this machine.',
  starting: 'The tunnel is starting. The address is not reachable yet.',
  connected: 'The tunnel is connected. This workspace is reachable from the internet.',
  reconnecting: 'The tunnel is reconnecting.',
  error: 'The tunnel failed.',
};

export type TunnelCardProps = {
  /** Test seam: the poll cadence while the tunnel is `starting`. */
  pollIntervalMs?: number;
  /** Test seam: how many polls a `starting` tunnel gets before it is stuck. */
  pollAttempts?: number;
};

export function TunnelCard({
  pollIntervalMs = TUNNEL_POLL_INTERVAL_MS,
  pollAttempts = TUNNEL_POLL_ATTEMPTS,
}: TunnelCardProps = {}) {
  const provider = useOptionalProvider();
  const [status, setStatus] = useState<TunnelStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refused, setRefused] = useState(false);
  const [busy, setBusy] = useState(false);
  const [stuck, setStuck] = useState(false);
  const [copied, setCopied] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!provider) return;
    setStatus(await provider.getTunnel());
  }, [provider]);

  useEffect(() => {
    void load().catch((cause: unknown) => {
      setError(messageOf(cause));
    });
  }, [load]);

  const state = status?.state;

  // A quick tunnel hands out the hostname before the edge answers on it, so
  // the card follows the status until it settles. Bounded, so a tunnel that
  // never comes up stops polling and says so instead of running forever.
  useEffect(() => {
    if (!provider || state !== 'starting') return;
    let left = pollAttempts;
    const timer = setInterval(() => {
      if (left <= 0) {
        clearInterval(timer);
        setStuck(true);
        return;
      }
      left -= 1;
      void provider
        .getTunnel()
        .then(setStatus)
        .catch((cause: unknown) => {
          setError(messageOf(cause));
        });
    }, pollIntervalMs);
    return () => {
      clearInterval(timer);
    };
  }, [provider, state, pollIntervalMs, pollAttempts]);

  if (!provider || !status) return null;

  // A runtime that cannot tunnel says so by answering `supported: false`, and
  // the card disappears rather than offering a switch that would do nothing.
  if (!status.supported) return null;

  const toggle = (enabled: boolean) => {
    setBusy(true);
    setError(null);
    setRefused(false);
    setStuck(false);
    setCopied(null);
    provider
      .setTunnel(enabled)
      .then(setStatus)
      .catch((cause: unknown) => {
        if (cause instanceof ProviderError && cause.code === 'tunnel_requires_token') {
          setRefused(true);
          return;
        }
        setError(messageOf(cause));
      })
      .finally(() => {
        setBusy(false);
      });
  };

  const copy = (text: string, label: string) => {
    setError(null);
    void navigator.clipboard
      ?.writeText(text)
      .then(() => {
        setCopied(label);
      })
      .catch(() => {
        setError('This browser refused clipboard access; select the address and copy it by hand.');
      });
  };

  const token = getToken();
  const live = status.state === 'connected' || status.state === 'reconnecting';
  const running = live || status.state === 'starting';

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle>Public tunnel</CardTitle>
        <Badge variant={running ? 'accent' : 'outline'}>{STATE_LABELS[status.state]}</Badge>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <p className="text-muted-foreground">
          A quick tunnel publishes this companion through{' '}
          {status.provider === '' ? 'a tunnelling service' : status.provider} and gives you a
          temporary <code>https</code> address to share. The app itself is served without
          authentication, so anyone holding the address loads the interface; only the{' '}
          <code>/api/v1</code> calls behind it require the access token. A new address is minted
          every time you turn the tunnel on, and the previous one stops working.
        </p>

        {running ? (
          <p
            role="alert"
            className="rounded-md border border-destructive/60 bg-destructive/10 p-3 text-destructive"
          >
            <strong className="font-semibold">This workspace is on the public internet.</strong>{' '}
            Anyone with the address can reach this companion, and the access token is the only thing
            standing between them and read and write access to every repository you have open. Turn
            the tunnel off as soon as you are done sharing.
          </p>
        ) : null}

        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="tunnel-enabled">Share over a public URL</Label>
          <Switch id="tunnel-enabled" checked={running} disabled={busy} onCheckedChange={toggle} />
        </div>

        <p aria-live="polite" role="status" className="sr-only">
          {STATE_ANNOUNCEMENTS[status.state]}
        </p>

        {refused ? (
          <p className="rounded-md border border-border bg-secondary/50 p-3">
            This companion was started with authentication disabled, so there is no token to guard
            the API and a tunnel would publish your repositories to anyone who found the address.
            Restart <code>gintrack serve</code> without <code>--token none</code> — it prints a
            token on start — and the tunnel becomes available.
          </p>
        ) : null}

        {status.tokenConfigured || running ? null : (
          <p className="text-muted-foreground">
            Authentication is disabled on this companion, so the tunnel cannot be opened.
          </p>
        )}

        {status.url === '' ? null : (
          <div className="space-y-2">
            <p className="break-all font-medium">{status.url}</p>

            {status.state === 'starting' ? (
              <p role="status" className="rounded-md border border-border bg-secondary/50 p-3">
                The address exists but is still propagating through DNS. It usually answers within a
                few seconds — wait for “Public” before you share it.
              </p>
            ) : null}

            {status.state === 'error' && status.error !== '' ? (
              <p role="alert" className="text-destructive">
                {status.error}
              </p>
            ) : null}

            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                disabled={!live}
                onClick={() => {
                  copy(status.url, 'public URL');
                }}
              >
                Copy public URL
              </Button>
              <span className="text-xs text-muted-foreground">
                The plain address. Whoever opens it still has to enter the access token.
              </span>
            </div>

            <div className="space-y-1 rounded-md border border-border p-3">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={!live || token === null}
                onClick={() => {
                  if (token === null) return;
                  copy(accessLink(status.url, token), 'link with access');
                }}
              >
                Copy link with access
              </Button>
              <p className="text-xs text-destructive">
                <strong className="font-semibold">This link is a credential.</strong> It carries
                your access token, and anyone who opens it can read and write every repository this
                companion has open — no password asked. Send it only to someone you would hand your
                working copy to, and never over a channel you would not put a password on.
              </p>
              {token === null ? (
                <p className="text-xs text-muted-foreground">
                  No token is stored in this tab, so there is no access link to copy.
                </p>
              ) : null}
            </div>
          </div>
        )}

        {copied === null ? null : (
          <p role="status" className="text-muted-foreground">
            Copied the {copied} to the clipboard.
          </p>
        )}

        {stuck ? (
          <p role="alert" className="text-destructive">
            The tunnel has not come up. Turn it off and on again, or check that this machine can
            reach the tunnelling service.
          </p>
        ) : null}

        {error === null ? null : (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}

        {live ? (
          <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[10rem_1fr]">
            <dt className="text-muted-foreground">Edge connections</dt>
            <dd>{status.connections}</dd>
            {status.since === null ? null : (
              <>
                <dt className="text-muted-foreground">Open since</dt>
                <dd>{status.since}</dd>
              </>
            )}
          </dl>
        ) : null}
      </CardContent>
    </Card>
  );
}


function messageOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}
