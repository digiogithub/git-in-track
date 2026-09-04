/**
 * Settings — Commit on save (story GIT-US-0020, docs/06-git-sync.md §3.3).
 *
 * One card, both runtimes. It reads and writes the settings through the
 * `DataProvider`, so the companion persists them in its configuration file and
 * the browser keeps them per workspace, and the UI branches on
 * `settings.supported` rather than on the mode: a runtime that cannot commit
 * yet says so instead of offering a switch that would do nothing.
 */

import { useCallback, useEffect, useState } from 'react';

import type { GitRepoStatus, GitSettings } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { renderCommitMessage } from '@/git/message';

/** The edit the preview is rendered from, so the user sees a real message. */
const PREVIEW_FIELDS = {
  itemId: 'ACME-US-0042',
  title: 'Login with SSO',
  type: 'story',
  status: 'in_progress',
  prevStatus: 'todo',
  projectKey: 'ACME',
  board: 'team-alpha',
  action: 'move',
  date: '2026-09-04',
} as const;

export function GitSettingsCard() {
  const provider = useOptionalProvider();
  const [settings, setSettings] = useState<GitSettings | null>(null);
  const [repos, setRepos] = useState<GitRepoStatus[]>([]);
  const [template, setTemplate] = useState('');
  const [debounce, setDebounce] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    if (!provider) return;
    const loaded = await provider.getGitSettings();
    setSettings(loaded);
    setTemplate(loaded.messageTemplate);
    setDebounce(String(loaded.commitDebounceMs));
    setRepos(await provider.getGitStatus());
  }, [provider]);

  useEffect(() => {
    void load().catch((cause: unknown) => {
      setError(cause instanceof Error ? cause.message : String(cause));
    });
  }, [load]);

  if (!provider || !settings) return null;

  const apply = (patch: Parameters<typeof provider.updateGitSettings>[0]) => {
    setSaving(true);
    setError(null);
    provider
      .updateGitSettings(patch)
      .then((next) => {
        setSettings(next);
        setTemplate(next.messageTemplate);
        setDebounce(String(next.commitDebounceMs));
      })
      .catch((cause: unknown) => {
        setError(cause instanceof Error ? cause.message : String(cause));
      })
      .finally(() => {
        setSaving(false);
      });
  };

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle>Commit on save</CardTitle>
        <Badge variant={settings.commitOnSave ? 'accent' : 'outline'}>
          {settings.commitOnSave ? 'On' : 'Off'}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-4 text-sm">
        <p className="text-muted-foreground">
          Every edit is committed to the repository it belongs to, a short moment after you stop
          typing, so a burst of keystrokes becomes one commit. It is off by default and never
          pushes anything on its own.
        </p>

        {settings.supported ? null : (
          <p role="status" className="rounded-md border border-border bg-secondary/50 p-3">
            {settings.reason}
          </p>
        )}

        <div className="flex items-center justify-between gap-4">
          <Label htmlFor="commit-on-save">Commit each save</Label>
          <Switch
            id="commit-on-save"
            checked={settings.commitOnSave}
            disabled={!settings.supported || saving}
            onCheckedChange={(checked) => {
              apply({ commitOnSave: checked });
            }}
          />
        </div>

        <form
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            const parsed = Number(debounce);
            if (!Number.isFinite(parsed) || parsed < 0) {
              setError('The batching window must be a number of milliseconds, and not negative.');
              return;
            }
            apply({ messageTemplate: template, commitDebounceMs: parsed });
          }}
        >
          <div className="space-y-1">
            <Label htmlFor="commit-template">Message template</Label>
            <Input
              id="commit-template"
              value={template}
              spellCheck={false}
              onChange={(event) => {
                setTemplate(event.target.value);
              }}
            />
            <p className="text-xs text-muted-foreground">
              Placeholders: <code>{'{{action}}'}</code>, <code>{'{{id}}'}</code>,{' '}
              <code>{'{{type}}'}</code>, <code>{'{{title}}'}</code>, <code>{'{{status}}'}</code>,{' '}
              <code>{'{{project}}'}</code>, <code>{'{{board}}'}</code>.
            </p>
          </div>

          <div className="space-y-1">
            <Label htmlFor="commit-debounce">Batching window (ms)</Label>
            <Input
              id="commit-debounce"
              inputMode="numeric"
              value={debounce}
              onChange={(event) => {
                setDebounce(event.target.value);
              }}
            />
          </div>

          <Button type="submit" disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </form>

        {error === null ? null : (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}

        <div className="space-y-1">
          <span className="text-muted-foreground">Preview</span>
          <pre className="overflow-x-auto rounded-md border border-border bg-secondary/40 p-3 text-xs">
            {previewOf(template)}
          </pre>
        </div>

        <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-[10rem_1fr]">
          <dt className="text-muted-foreground">Backend</dt>
          <dd>
            {settings.resolvedBackend}
            {settings.gitVersion === undefined ? '' : ` (git ${settings.gitVersion})`}
          </dd>
          <dt className="text-muted-foreground">Waiting to commit</dt>
          <dd>{settings.pending}</dd>
        </dl>

        {repos.length === 0 ? null : (
          <ul className="space-y-1">
            {repos.map((repo) => (
              <li key={repo.repo} className="flex flex-wrap items-baseline gap-2">
                <span className="font-medium">{repo.repo}</span>
                {repo.git ? (
                  <span className="text-muted-foreground">
                    {repo.identityError === undefined
                      ? (repo.identity ?? repo.backend)
                      : repo.identityError}
                  </span>
                ) : (
                  <span className="text-muted-foreground">{repo.reason}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

/** Renders the sample message, or the reason the template cannot render. */
function previewOf(template: string): string {
  try {
    const message = renderCommitMessage(template, {
      ...PREVIEW_FIELDS,
      tool: 'gintrack (companion)',
    });
    return message.body === '' ? message.subject : `${message.subject}\n\n${message.body}`;
  } catch (error) {
    return error instanceof Error ? error.message : String(error);
  }
}
