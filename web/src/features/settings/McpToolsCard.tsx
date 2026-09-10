/**
 * Settings — MCP write tools.
 *
 * The MCP server ships read-only: an agent can list, read and search, and the
 * tools that create or edit items are not even advertised. Turning them on is a
 * grant, so it is a switch a person flips deliberately and never a default.
 *
 * It exists as a setting and not only as `gintrack mcp --allow-write` because
 * the flag is in the wrong place: an agent runtime is configured with a bare
 * `gintrack mcp`, often in a file the user does not own, so the only practical
 * way to enable writes once is to store the choice where every stdio server
 * reads it. That is what this card does — the companion writes
 * `mcp.allowWrite` to its configuration file, and an agent picks it up the next
 * time its MCP server starts.
 */

import { useCallback, useEffect, useState } from 'react';

import type { McpSettings } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';

export function McpToolsCard() {
  const provider = useOptionalProvider();
  const [settings, setSettings] = useState<McpSettings | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (!provider) return;
    setSettings(await provider.getMcpSettings());
  }, [provider]);

  useEffect(() => {
    void load().catch((cause: unknown) => {
      setError(messageOf(cause));
    });
  }, [load]);

  if (!provider || !settings) return null;

  // A runtime with no MCP surface at all — browser-only mode, or a companion
  // started without a configuration file — says so by answering
  // `supported: false`, and the card disappears rather than offering a switch
  // that would change nothing.
  if (!settings.supported) return null;

  const toggle = (allowWrite: boolean) => {
    setBusy(true);
    setError(null);
    provider
      .setMcpWriteTools(allowWrite)
      .then(setSettings)
      .catch((cause: unknown) => {
        setError(messageOf(cause));
      })
      .finally(() => {
        setBusy(false);
      });
  };

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle>Agent tools (MCP)</CardTitle>
        <Badge variant={settings.allowWrite ? 'accent' : 'outline'}>
          {settings.allowWrite ? 'Read and write' : 'Read-only'}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <p className="text-muted-foreground">
          Agents reach this workspace over the Model Context Protocol — <code>gintrack mcp</code>{' '}
          for a local agent, <code>POST /mcp</code> for one speaking to this companion. Reading is
          always allowed. With writing on, the tools that create and edit items —{' '}
          <code>create_task</code>, <code>create_story</code>, <code>update_item</code>,{' '}
          <code>move_on_board</code> and the rest — are advertised too.
        </p>

        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="mcp-allow-write">Let agents create and edit items</Label>
          <Switch
            id="mcp-allow-write"
            checked={settings.allowWrite}
            disabled={busy}
            onCheckedChange={toggle}
          />
        </div>

        {settings.allowWrite ? (
          <p className="rounded-md border border-border bg-secondary/50 p-3">
            <strong className="font-medium">Every agent you run can now edit the backlog.</strong>{' '}
            The changes are ordinary file edits in your working copies: they show up in the UI as
            they happen and in <code>git diff</code> afterwards, so nothing is hidden — but nothing
            asks you first either.
          </p>
        ) : null}

        <p className="text-muted-foreground">
          {settings.persisted
            ? `Saved to ${settings.configPath === '' ? 'the configuration file' : settings.configPath}. An agent that already has an MCP server running picks the change up when that server restarts — for most clients, when you restart the client.`
            : 'This companion has no configuration file to write to, so the change applies to this process only and is gone when it stops.'}
        </p>

        {settings.http ? (
          <p className="text-muted-foreground">
            This companion also serves the endpoint at <code>/mcp</code>, where the change is
            already live; a connected client reconnects once.
          </p>
        ) : null}

        {error === null ? null : (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function messageOf(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}
