/**
 * Sync panel (`/sync`) — story GIT-US-0021, docs/05-web-app.md §5 and
 * docs/06-git-sync.md §4.
 *
 * One row per repository: branch, the ahead/behind counters, the uncommitted
 * set and a state word. "Preview" is a dry run — it fetches, which is
 * read-only, and lists what would move without changing anything. "Sync" runs
 * fetch, integrate and push.
 *
 * Both runtimes render from the same shapes, and the panel branches on
 * `settings.supported`, never on the mode: browser-only mode without a CORS
 * proxy says so, with the reason, instead of offering a button that fails.
 */

import { useCallback, useEffect, useState } from 'react';

import type { SyncRepoStatus, SyncResult, SyncSettings, SyncState } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { CredentialPrompt } from '@/features/sync/CredentialPrompt';
import { forgetCredentials, sessionCredentialCount } from '@/git/credentials';

/** How each state reads to a human, and how loud it looks. */
const STATE_LABELS: Record<
  SyncState,
  { label: string; variant: 'default' | 'outline' | 'accent' | 'destructive' }
> = {
  up_to_date: { label: 'Up to date', variant: 'outline' },
  ahead: { label: 'Ahead', variant: 'accent' },
  behind: { label: 'Behind', variant: 'accent' },
  diverged: { label: 'Diverged', variant: 'accent' },
  dirty: { label: 'Uncommitted changes', variant: 'default' },
  conflicted: { label: 'Conflicts', variant: 'destructive' },
  in_progress: { label: 'Rebase in progress', variant: 'destructive' },
  detached: { label: 'Detached HEAD', variant: 'destructive' },
  no_remote: { label: 'No remote', variant: 'outline' },
  no_upstream: { label: 'No upstream branch', variant: 'outline' },
};

export function SyncPanel() {
  const provider = useOptionalProvider();
  const [repos, setRepos] = useState<SyncRepoStatus[]>([]);
  const [settings, setSettings] = useState<SyncSettings | null>(null);
  const [results, setResults] = useState<Record<string, SyncResult>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tokens, setTokens] = useState(0);

  const load = useCallback(async () => {
    if (!provider) return;
    setSettings(await provider.getSyncSettings());
    setRepos(await provider.getSyncStatus());
    setTokens(sessionCredentialCount());
  }, [provider]);

  useEffect(() => {
    void load().catch((cause: unknown) => {
      setError(cause instanceof Error ? cause.message : String(cause));
    });
  }, [load]);

  if (!provider) return null;

  const run = (repoId: string, dryRun: boolean) => {
    setBusy(`${repoId}:${dryRun ? 'preview' : 'sync'}`);
    setError(null);
    provider
      .sync(repoId, { dryRun })
      .then(async (reports) => {
        const report = reports[0];
        if (report) setResults((current) => ({ ...current, [repoId]: report }));
        if (!dryRun) await load();
      })
      .catch((cause: unknown) => {
        setError(cause instanceof Error ? cause.message : String(cause));
      })
      .finally(() => {
        setBusy(null);
        setTokens(sessionCredentialCount());
      });
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Sync</h1>
        <p className="text-sm text-muted-foreground">
          Fetch everyone else’s work, {settings?.pullStrategy ?? 'rebase'} yours on top of it and
          push. Nothing here can lose a commit: a run that fails leaves your files exactly as they
          are and says what to do next.
        </p>
      </header>

      {settings && !settings.supported ? (
        <p role="status" className="rounded-md border border-border bg-secondary/50 p-3 text-sm">
          {settings.reason}
        </p>
      ) : null}

      {error ? (
        <p
          role="alert"
          className="rounded-md border border-destructive/40 p-3 text-sm text-destructive"
        >
          {error}
        </p>
      ) : null}

      {repos.length === 0 ? (
        <p className="text-sm text-muted-foreground">No repository is open in this workspace.</p>
      ) : null}

      {repos.map((repo) => (
        <RepoRow
          key={repo.repo}
          repo={repo}
          result={results[repo.repo]}
          busy={busy}
          enabled={settings?.supported !== false}
          onRun={run}
        />
      ))}

      {tokens > 0 ? (
        <p className="flex items-center gap-3 text-sm text-muted-foreground">
          <span>
            {tokens} git token(s) are held in this tab’s memory. They are never written to storage
            and a reload forgets them.
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              forgetCredentials();
              setTokens(0);
            }}
          >
            Forget tokens
          </Button>
        </p>
      ) : null}

      <CredentialPrompt />
    </div>
  );
}

/** One repository row with its counters, its actions and its last report. */
function RepoRow({
  repo,
  result,
  busy,
  enabled,
  onRun,
}: {
  repo: SyncRepoStatus;
  result: SyncResult | undefined;
  busy: string | null;
  enabled: boolean;
  onRun: (repoId: string, dryRun: boolean) => void;
}) {
  const status = repo.status;
  const state = status?.state ?? 'no_remote';
  const tone = STATE_LABELS[state];

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2">
          {repo.repo}
          <Badge variant={tone.variant}>{tone.label}</Badge>
        </CardTitle>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={!repo.git || !enabled || busy !== null}
            onClick={() => {
              onRun(repo.repo, true);
            }}
          >
            {busy === `${repo.repo}:preview` ? 'Previewing…' : 'Preview'}
          </Button>
          <Button
            size="sm"
            disabled={!repo.git || !enabled || busy !== null}
            onClick={() => {
              onRun(repo.repo, false);
            }}
          >
            {busy === `${repo.repo}:sync` ? 'Syncing…' : 'Sync'}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        {repo.git && status ? (
          <dl className="grid grid-cols-2 gap-x-6 gap-y-1 sm:grid-cols-4">
            <Field label="Branch" value={status.branch} />
            <Field label="Remote" value={status.upstream ?? status.remote ?? '—'} />
            <Field label="Ahead / behind" value={`${status.ahead} / ${status.behind}`} />
            <Field
              label="Uncommitted"
              value={status.clean ? 'none' : `${status.dirty?.length ?? 0} file(s)`}
            />
          </dl>
        ) : (
          <p className="text-muted-foreground">{repo.reason}</p>
        )}

        {repo.pending > 0 ? (
          <p className="text-muted-foreground">
            {repo.pending} edit(s) are waiting to be committed; a sync commits them first.
          </p>
        ) : null}

        {result ? <Report result={result} /> : null}
      </CardContent>
    </Card>
  );
}

/** One labelled counter. */
function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-muted-foreground">{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

/** The last run's report: the preview, or the failure and what to do about it. */
function Report({ result }: { result: SyncResult }) {
  if (result.code) {
    return (
      <div role="alert" className="space-y-2 rounded-md border border-destructive/40 p-3">
        <p className="text-destructive">{result.message}</p>
        {result.conflicts && result.conflicts.length > 0 ? (
          <ul className="list-inside list-disc text-muted-foreground">
            {result.conflicts.map((conflict) => (
              <li key={conflict.path}>
                {conflict.path} ({conflict.kind})
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    );
  }
  const incoming = result.incoming ?? [];
  const outgoing = result.outgoing ?? [];
  return (
    <div className="space-y-2 rounded-md border border-border p-3">
      <p>
        {result.dryRun
          ? `Preview: ${incoming.length} commit(s) would come in, ${outgoing.length} would be pushed. Nothing was changed.`
          : `Pulled ${result.pulled}, pushed ${result.pushed}.`}
      </p>
      {incoming.length > 0 ? (
        <ul className="list-inside list-disc text-muted-foreground">
          {incoming.map((commit) => (
            <li key={commit.sha}>
              <code>{commit.sha.slice(0, 7)}</code> {commit.subject}
            </li>
          ))}
        </ul>
      ) : null}
      {(result.warnings ?? []).map((warning) => (
        <p key={warning} className="text-muted-foreground">
          {warning}
        </p>
      ))}
    </div>
  );
}
