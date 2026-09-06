/**
 * Settings — CORS proxy (story GIT-US-0042, docs/06-git-sync.md §6.3).
 *
 * Browser-only git needs a proxy to reach a git host at all, and choosing one is
 * a security decision the user has to make with the facts in front of them: any
 * proxy sees the repository traffic and every credential sent with it. So this
 * card says what is in effect and where it came from, and it never picks a
 * public proxy on its own.
 *
 * The one proxy that is offered automatically is the companion's, because it
 * runs on the user's own machine and adds no third party. When it is in effect
 * the card says so and the field stays empty, which is what makes clearing the
 * field fall back to it rather than to nothing.
 *
 * A saved URL is checked before it is trusted: §6.3 promises a preflight against
 * the repository's `info/refs`, and that is what the "Check" button runs.
 */

import { useCallback, useEffect, useState } from 'react';

import type { SyncRepoStatus, SyncSettings } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { preflightCorsProxy, type ProxyPreflight } from '@/git/companion-proxy';

/** The public proxy §6.3 allows only as an explicit choice. */
const PUBLIC_PROXY = 'https://cors.isomorphic-git.org';

export function SyncProxyCard() {
  const provider = useOptionalProvider();
  const [settings, setSettings] = useState<SyncSettings | null>(null);
  const [repos, setRepos] = useState<SyncRepoStatus[]>([]);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [checking, setChecking] = useState(false);
  const [checked, setChecked] = useState<ProxyPreflight | null>(null);

  const load = useCallback(async () => {
    if (!provider) return;
    const loaded = await provider.getSyncSettings();
    setSettings(loaded);
    setDraft(loaded.proxySource === 'configured' ? (loaded.corsProxy ?? '') : '');
    setRepos(await provider.getSyncStatus());
  }, [provider]);

  useEffect(() => {
    void load().catch((cause: unknown) => {
      setError(cause instanceof Error ? cause.message : String(cause));
    });
  }, [load]);

  if (!provider || !settings) return null;

  // The companion does its own networking, so the proxy is a browser-only
  // concern and the card would only confuse a companion-mode user.
  if (settings.proxySource === undefined) return null;

  const remote = repos.find((repo) => repo.status?.remoteUrl?.startsWith('http'))?.status
    ?.remoteUrl;

  const save = (value: string) => {
    setError(null);
    setChecked(null);
    provider
      .updateSyncSettings({ corsProxy: value })
      .then((next) => {
        setSettings(next);
        setDraft(next.proxySource === 'configured' ? (next.corsProxy ?? '') : '');
      })
      .catch((cause: unknown) => {
        setError(cause instanceof Error ? cause.message : String(cause));
      });
  };

  const check = () => {
    const proxy = settings.corsProxy;
    if (proxy === undefined || remote === undefined) return;
    setChecking(true);
    setChecked(null);
    void preflightCorsProxy(proxy, remote)
      .then(setChecked)
      .finally(() => {
        setChecking(false);
      });
  };

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle>CORS proxy</CardTitle>
        <Badge variant={settings.supported ? 'accent' : 'outline'}>
          {settings.proxySource === 'companion'
            ? 'Companion'
            : settings.proxySource === 'configured'
              ? 'Configured'
              : 'None'}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <p className="text-muted-foreground">
          Git hosts send no CORS headers, so a browser tab cannot fetch or push without a proxy that
          adds them. Whichever proxy you use sees this repository’s traffic and any token sent with
          it, so nothing is routed through one you did not choose.
        </p>

        <p role="status" className="rounded-md border border-border bg-secondary/50 p-3">
          {settings.reason ?? `In effect: ${settings.corsProxy ?? 'none'}`}
        </p>

        {settings.proxySource === 'companion' ? (
          <p className="text-muted-foreground">
            Using <code>{settings.corsProxy}</code>. Leave the field empty to keep it.
          </p>
        ) : null}

        <form
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            save(draft.trim());
          }}
        >
          <div className="space-y-1">
            <Label htmlFor="cors-proxy">Proxy URL</Label>
            <Input
              id="cors-proxy"
              value={draft}
              spellCheck={false}
              placeholder="https://git-proxy.your-team.example"
              onChange={(event) => {
                setDraft(event.target.value);
              }}
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <Button type="submit">Save proxy</Button>
            <Button
              type="button"
              variant="outline"
              disabled={!settings.supported || remote === undefined || checking}
              onClick={check}
            >
              {checking ? 'Checking…' : 'Check against a repository'}
            </Button>
            {draft.trim() === '' ? null : (
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  setDraft('');
                  save('');
                }}
              >
                Clear
              </Button>
            )}
          </div>
        </form>

        {checked === null ? null : (
          <p role="status" className={checked.ok ? 'text-muted-foreground' : 'text-destructive'}>
            {checked.ok
              ? `The proxy answered for ${remote ?? 'the repository'}; browser sync can use it.`
              : checked.reason}
          </p>
        )}

        {error === null ? null : (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}

        <details className="text-muted-foreground">
          <summary className="cursor-pointer">Other options</summary>
          <ul className="mt-2 list-disc space-y-1 pl-5">
            <li>
              Run <code>gintrack serve</code> and this tab uses the proxy the companion serves on
              this machine — no third party, no configuration.
            </li>
            <li>
              Self-host <code>@isomorphic-git/cors-proxy</code>, or use the nginx/Caddy recipe in{' '}
              <code>docs/06-git-sync.md</code> §6.3, which forwards only the three git endpoints to
              an allowlisted set of hosts.
            </li>
            <li>
              <code>{PUBLIC_PROXY}</code> is a public service run by the isomorphic-git project. It
              will see your traffic and any token you send. Use it only for public repositories, and
              only by typing it above.
            </li>
          </ul>
        </details>
      </CardContent>
    </Card>
  );
}
