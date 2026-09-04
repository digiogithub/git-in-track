/**
 * Settings — Runtime section (story GIT-US-0015) and commit on save
 * (story GIT-US-0020).
 *
 * Everything the user needs to understand which runtime they are on: the
 * detected mode, the companion version and URL, the state of the event socket,
 * the capabilities the UI branches on, and a way to re-run the probe. In
 * browser-only mode it explains what installing the companion would add.
 */

import { useState, useSyncExternalStore } from 'react';

import type { ConnectionState } from '@/api/companion-provider';
import { probeCompanionNow } from '@/api/detect';
import { clearToken, hasToken, onTokenChange, setToken } from '@/api/token';
import { useAppStore } from '@/app/store';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { GitSettingsCard } from '@/features/settings/GitSettingsCard';

/** Where the companion binary is published (docs/09-ci-cd-and-releases.md). */
const COMPANION_DOWNLOAD_URL = 'https://github.com/digiogithub/git-in-track/releases';

const CONNECTION_LABELS: Record<ConnectionState, string> = {
  idle: 'Not connected',
  connecting: 'Connecting…',
  open: 'Live (WebSocket)',
  reconnecting: 'Reconnecting…',
  polling: 'Polling (event socket unavailable)',
  closed: 'Closed',
};

export function SettingsPage() {
  const mode = useAppStore((state) => state.mode);
  const companionVersion = useAppStore((state) => state.companionVersion);
  const companionUrl = useAppStore((state) => state.companionUrl);
  const connection = useAppStore((state) => state.connection);
  const capabilities = useAppStore((state) => state.capabilities);
  const [checking, setChecking] = useState(false);

  const companion = mode === 'companion';

  const check = () => {
    setChecking(true);
    void probeCompanionNow().finally(() => {
      setChecking(false);
    });
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">Workspace, appearance, sync and agents.</p>
      </header>

      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0">
          <CardTitle>Runtime</CardTitle>
          <Button variant="outline" size="sm" onClick={check} disabled={checking}>
            {checking ? 'Checking…' : 'Check again'}
          </Button>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[10rem_1fr]">
            <dt className="text-muted-foreground">Mode</dt>
            <dd>
              <Badge variant={companion ? 'accent' : 'outline'}>{mode}</Badge>
            </dd>

            <dt className="text-muted-foreground">Companion version</dt>
            <dd>{companionVersion ?? 'not detected'}</dd>

            <dt className="text-muted-foreground">Companion URL</dt>
            <dd className="break-all">
              {companion ? (companionUrl === '' ? 'this origin' : (companionUrl ?? '—')) : '—'}
            </dd>

            <dt className="text-muted-foreground">Event stream</dt>
            <dd>{companion ? CONNECTION_LABELS[connection] : 'Not applicable in browser mode'}</dd>
          </dl>

          {companion ? null : (
            <p className="rounded-md border border-border bg-secondary/50 p-3 text-sm">
              <strong className="font-medium">Install the companion</strong> to get native indexing,
              file watching, git over SSH and full-text search.{' '}
              <a
                className="underline underline-offset-4"
                href={COMPANION_DOWNLOAD_URL}
                target="_blank"
                rel="noreferrer"
              >
                Download gintrack
              </a>{' '}
              and run <code>gintrack serve</code>; this tab picks it up on its own.
            </p>
          )}
        </CardContent>
      </Card>

      {companion ? <CompanionTokenCard /> : null}

      <GitSettingsCard />

      <Card>
        <CardHeader>
          <CardTitle>Capabilities</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
            <CapabilityRow label="Write" value={capabilities.write} />
            <CapabilityRow label="Git" value={capabilities.git} />
            <CapabilityRow label="SSH" value={capabilities.ssh} />
            <CapabilityRow label="File watching" value={capabilities.watch} />
            <CapabilityRow label="Full-text search" value={capabilities.fullTextSearch} />
            <CapabilityRow label="MCP" value={capabilities.mcp} />
            <CapabilityRow label="Open in editor" value={capabilities.openInEditor} />
            <CapabilityRow label="Max batch write" value={capabilities.maxBatchWrite} />
          </dl>
        </CardContent>
      </Card>
    </div>
  );
}

function CapabilityRow({ label, value }: { label: string; value: boolean | string | number }) {
  const text = typeof value === 'boolean' ? (value ? 'Yes' : 'No') : String(value);
  return (
    <div className="flex items-center justify-between gap-4 border-b border-border/60 py-1 last:border-0">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{text}</dd>
    </div>
  );
}

/**
 * The bearer token (docs/07-cli-and-api.md §5.1). The companion normally hands
 * it over in the URL; on the Vite dev server it is pasted once, and after a
 * `401` cleared it the UI asks for it here instead of failing silently.
 */
function CompanionTokenCard() {
  const auth = useAppStore((state) => state.companionAuth);
  const stored = useSyncExternalStore(onTokenChange, hasToken, () => false);
  const [value, setValue] = useState('');

  return (
    <Card>
      <CardHeader>
        <CardTitle>Companion access token</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        {auth === 'required' ? (
          <p role="alert" className="text-destructive">
            The companion rejected or is missing the access token. Copy the token printed by{' '}
            <code>gintrack serve</code> and paste it below.
          </p>
        ) : (
          <p className="text-muted-foreground">
            {stored
              ? 'A token is stored for this tab and sent as a bearer credential.'
              : 'No token stored yet.'}
          </p>
        )}

        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            setToken(value);
            setValue('');
          }}
        >
          <div className="min-w-[16rem] flex-1 space-y-1">
            <Label htmlFor="companion-token">Token</Label>
            <Input
              id="companion-token"
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={value}
              placeholder="s7Q1e…9Zk"
              onChange={(event) => {
                setValue(event.target.value);
              }}
            />
          </div>
          <Button type="submit" disabled={value.trim() === ''}>
            Save token
          </Button>
          {stored ? (
            <Button type="button" variant="ghost" onClick={clearToken}>
              Forget token
            </Button>
          ) : null}
        </form>
      </CardContent>
    </Card>
  );
}
