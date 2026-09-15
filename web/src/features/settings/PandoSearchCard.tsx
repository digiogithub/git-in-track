/**
 * Settings — semantic search over Pando (story GIT-US-0091, epic GIT-EP-0019).
 *
 * The card exists to answer one complaint: "semantic search returns nothing".
 * Everything it renders is a step of that diagnosis — where Pando is, whether
 * it answered *just now*, where the corpus was written, when it was last
 * exported and what the last reindex actually did — so that the answer is read
 * here rather than out of the companion's log.
 *
 * Three things shape it.
 *
 * **No token field, ever.** `GET /api/v1/search/settings` never reports a
 * credential and `PATCH` never accepts one: a token enters the companion from
 * its environment or its configuration file and stays in that process. So the
 * card says where a token comes from and offers no input for it, which is a
 * stronger promise than a masked field.
 *
 * **The knowledge-base half is honest.** Pando has no filesystem watcher for
 * the corpus (`KBWatch` is off by design), so without a REST URL a reindex
 * re-exports the corpus and waits for Pando's next import pass. The job says so
 * in `kbNote` and this card repeats it word for word rather than reporting a
 * completed index operation that did not happen.
 *
 * **Progress arrives, it is not polled.** `POST /search/reindex` answers `202`
 * with a job and the work runs in the background, reporting on the hub's
 * `search.progress` topic; the card follows that through the provider's event
 * seam and re-reads the settings once the job reaches a terminal phase.
 */

import { TriangleAlert } from 'lucide-react';
import { useCallback, useEffect, useId, useState } from 'react';

import {
  ProviderError,
  type SearchReindexPhase,
  type SearchSettings,
  type SearchSettingsPatch,
} from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Progress } from '@/components/ui/progress';
import { Switch } from '@/components/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useToast } from '@/components/ui/toast';

/** What a reindex phase is called on screen. */
const PHASE_LABELS: Record<SearchReindexPhase, string> = {
  export: 'Exporting the corpus',
  code: 'Indexing the source tree',
  kb: 'Knowledge base',
  completed: 'Finished',
  failed: 'Failed',
};

/** The phases a job is still working in. */
function isRunning(phase: SearchReindexPhase | undefined): boolean {
  return phase === 'export' || phase === 'code' || phase === 'kb';
}

/**
 * What a problem code means here. The two refusals are not failures to retry:
 * one says a job is already doing this, the other says there is nothing to
 * index yet, and each names a different thing for the reader to do.
 */
function searchSettingsMessage(error: unknown): string {
  if (!(error instanceof ProviderError)) {
    return error instanceof Error ? error.message : String(error);
  }
  switch (error.code) {
    case 'search_reindex_running':
      return 'A reindex is already running. It was left alone — wait for it to finish, then start another one.';
    case 'search_not_configured':
      return 'There is nothing to index yet: this companion has neither a corpus directory nor a Pando endpoint. Fill in the MCP URL below and save, then reindex.';
    case 'not_supported':
      return 'This runtime cannot configure semantic search. Run `gintrack serve` to use it.';
    case 'validation_failed':
      return error.message;
    default:
      return error.message;
  }
}

/** A timestamp the reader can place, or a dash when there has never been one. */
function when(value: string): string {
  if (value === '') return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

/** The editable half, so a reload is a state reset and nothing else. */
type Draft = {
  mcpUrl: string;
  restUrl: string;
  projectId: string;
  corpusDir: string;
  allowRemote: boolean;
};

function draftOf(settings: SearchSettings): Draft {
  return {
    mcpUrl: settings.mcpUrl,
    restUrl: settings.restUrl,
    projectId: settings.projectId,
    corpusDir: settings.corpusDir,
    allowRemote: settings.allowRemote,
  };
}

/**
 * Gate: the card exists only where the runtime has the settings surface at
 * all. Browser-only mode has no process to export a corpus or hold a Pando
 * token, so it has no card — not an empty one.
 */
export function PandoSearchCard() {
  const provider = useOptionalProvider();
  if (!provider?.capabilities.searchSettings) return null;
  return <PandoSearchSettings />;
}

function PandoSearchSettings() {
  const provider = useOptionalProvider();
  const { toast } = useToast();
  const [settings, setSettings] = useState<SearchSettings | null>(null);
  const [draft, setDraft] = useState<Draft>({
    mcpUrl: '',
    restUrl: '',
    projectId: '',
    corpusDir: '',
    allowRemote: false,
  });
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [starting, setStarting] = useState(false);
  /** The last `search.progress` frame, so the button reports live work. */
  const [progress, setProgress] = useState<{
    phase: SearchReindexPhase;
    percent: number;
    repo: string;
    message: string;
  } | null>(null);
  const mcpId = useId();
  const restId = useId();
  const projectFieldId = useId();
  const corpusId = useId();
  const remoteId = useId();

  const load = useCallback(async () => {
    if (!provider) return;
    const loaded = await provider.getSearchSettings();
    setSettings(loaded);
    setDraft(draftOf(loaded));
  }, [provider]);

  useEffect(() => {
    setError(null);
    void load().catch((cause: unknown) => {
      setError(searchSettingsMessage(cause));
    });
  }, [load]);

  // The job reports on the hub, so the card never polls. A terminal frame is
  // also the cue to re-read the settings: the finished job — its counts, its
  // `kbNote`, its per-repository errors — lives there, not in the frame.
  useEffect(() => {
    if (!provider) return undefined;
    return provider.subscribe((event) => {
      if (event.kind !== 'searchProgress') return;
      setProgress({
        phase: event.phase,
        percent: event.percent,
        repo: event.repoId,
        message: event.message,
      });
      if (!isRunning(event.phase)) {
        void load().catch((cause: unknown) => {
          setError(searchSettingsMessage(cause));
        });
      }
    });
  }, [provider, load]);

  if (!provider) return null;

  const save = () => {
    setSaving(true);
    setError(null);
    const patch: SearchSettingsPatch = {
      mcpUrl: draft.mcpUrl.trim(),
      restUrl: draft.restUrl.trim(),
      projectId: draft.projectId.trim(),
      corpusDir: draft.corpusDir.trim(),
      allowRemote: draft.allowRemote,
    };
    provider
      .updateSearchSettings(patch)
      .then((next) => {
        setSettings(next);
        setDraft(draftOf(next));
        toast({
          title: 'Semantic search settings saved',
          description: next.persisted
            ? 'Written to the configuration file.'
            : 'Applied to the running companion only — it was not written to disk, so a restart loses it.',
        });
      })
      .catch((cause: unknown) => {
        setError(searchSettingsMessage(cause));
        toast({ title: 'The change was not saved', variant: 'destructive' });
      })
      .finally(() => {
        setSaving(false);
      });
  };

  const reindex = () => {
    setStarting(true);
    setError(null);
    provider
      .reindexSearch()
      .then((job) => {
        setProgress({ phase: job.phase, percent: 0, repo: '', message: '' });
        setSettings((current) => (current === null ? current : { ...current, reindex: job }));
      })
      .catch((cause: unknown) => {
        setError(searchSettingsMessage(cause));
        toast({ title: 'The reindex did not start', variant: 'destructive' });
      })
      .finally(() => {
        setStarting(false);
      });
  };

  const job = settings?.reindex ?? null;
  const phase = progress?.phase ?? job?.phase;
  const running = starting || isRunning(phase);

  if (settings === null) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Semantic search (Pando)</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 text-sm text-muted-foreground">
          {error === null ? (
            'Loading…'
          ) : (
            <p role="alert" className="text-destructive">
              {error}
            </p>
          )}
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between space-y-0">
        <div className="space-y-1">
          <CardTitle>Semantic search (Pando)</CardTitle>
          <CardDescription>
            Where Pando is, where the exported corpus lives and whether it is current. Search falls
            back to the built-in index whenever this half is not answering.
          </CardDescription>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Badge variant={settings.backend === 'pando' ? 'accent' : 'outline'}>
            {settings.backend}
          </Badge>
          {settings.reachable === false ? <Badge variant="warning">degraded</Badge> : null}
        </div>
      </CardHeader>

      <CardContent className="space-y-5 text-sm">
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            save();
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1">
              <Label htmlFor={mcpId}>Pando MCP URL</Label>
              <Input
                id={mcpId}
                value={draft.mcpUrl}
                spellCheck={false}
                placeholder="http://127.0.0.1:9777/mcp"
                onChange={(event) => {
                  setDraft((current) => ({ ...current, mcpUrl: event.target.value }));
                }}
              />
              <p className="text-muted-foreground">
                Where <code>pando mcp-server</code> answers. Leaving it empty switches semantic
                search off and leaves the built-in index alone.
              </p>
            </div>

            <div className="space-y-1">
              <Label htmlFor={restId}>Pando REST URL</Label>
              <Input
                id={restId}
                value={draft.restUrl}
                spellCheck={false}
                placeholder="http://127.0.0.1:9778"
                onChange={(event) => {
                  setDraft((current) => ({ ...current, restUrl: event.target.value }));
                }}
              />
              <p className="text-muted-foreground">
                Optional, and it needs <code>pando serve</code>. With it, a reindex really reindexes
                the knowledge base and reports the counts; without it, the corpus is re-exported and
                waits for Pando&rsquo;s next import pass.
              </p>
            </div>

            <div className="space-y-1">
              <Label htmlFor={projectFieldId}>Code project id</Label>
              <Input
                id={projectFieldId}
                value={draft.projectId}
                spellCheck={false}
                placeholder="acme-api"
                onChange={(event) => {
                  setDraft((current) => ({ ...current, projectId: event.target.value }));
                }}
              />
              <p className="text-muted-foreground">
                The project the source-tree index lives under. Empty means Pando&rsquo;s own
                sanitized repository path.
              </p>
            </div>

            <div className="space-y-1">
              <Label htmlFor={corpusId}>Corpus directory</Label>
              <Input
                id={corpusId}
                value={draft.corpusDir}
                spellCheck={false}
                onChange={(event) => {
                  setDraft((current) => ({ ...current, corpusDir: event.target.value }));
                }}
              />
              <p className="text-muted-foreground">
                An absolute path. Each repository gets a folder of its own under it, which is the
                same path <code>gintrack agent init</code> writes into Pando&rsquo;s{' '}
                <code>KBPath</code>.
              </p>
            </div>
          </div>

          <div className="space-y-1">
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor={remoteId}>Allow a remote Pando</Label>
              <Switch
                id={remoteId}
                checked={draft.allowRemote}
                onCheckedChange={(allowRemote) => {
                  setDraft((current) => ({ ...current, allowRemote }));
                }}
              />
            </div>
            <p className="text-muted-foreground" data-testid="pando-remote-warning">
              <strong className="font-medium">
                A URL whose host is not a loopback address is refused, and there is no override but
                this switch.
              </strong>{' '}
              Pando&rsquo;s MCP transport exposes far more than search — file writes, shell
              execution, agent spawning — so a companion that could be pointed at a remote Pando
              would be a remote-code-execution gadget wearing a search feature&rsquo;s clothes. It
              exists for a future authenticated deployment and should stay off.
            </p>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={saving}>
              {saving ? 'Saving…' : 'Save settings'}
            </Button>
            <Button type="button" variant="outline" disabled={running} onClick={reindex}>
              {running ? 'Reindexing…' : 'Reindex now'}
            </Button>
          </div>
        </form>

        <p className="text-muted-foreground" data-testid="pando-token-note">
          No token is ever shown or changed here. Both Pando tokens are set in the configuration
          file or in <code>GINTRACK_PANDO_MCP_TOKEN</code> and{' '}
          <code>GINTRACK_PANDO_REST_TOKEN</code>, and they stay in the companion process.
        </p>

        <p data-testid="pando-reachability" role="status">
          {settings.reachable === null ? (
            <span className="text-muted-foreground">
              No Pando endpoint is configured, so nothing was probed.
            </span>
          ) : settings.reachable ? (
            <span className="text-muted-foreground">
              Pando answered at <code>{settings.mcpUrl}</code> when this page was read.
            </span>
          ) : (
            <span className="flex gap-2 text-destructive">
              <TriangleAlert aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
              <span>
                Pando did not answer at <code>{settings.mcpUrl}</code>
                {settings.reachableError === '' ? '.' : `: ${settings.reachableError}`}
              </span>
            </span>
          )}
        </p>

        <div className="space-y-2">
          <h3 className="font-medium">Exported corpus</h3>
          {settings.corpora.length === 0 ? (
            <p className="text-muted-foreground">
              No repository has exported a corpus yet. Reindex to write one.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Repository</TableHead>
                    <TableHead>Directory</TableHead>
                    <TableHead>Last export</TableHead>
                    <TableHead>Documents</TableHead>
                    <TableHead>Written</TableHead>
                    <TableHead>Removed</TableHead>
                    <TableHead>Skipped</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {settings.corpora.map((corpus) => (
                    <TableRow key={corpus.repo}>
                      <TableCell className="font-medium">{corpus.repo}</TableCell>
                      <TableCell className="break-all font-mono text-xs">{corpus.dir}</TableCell>
                      <TableCell>{when(corpus.last.at)}</TableCell>
                      <TableCell>{corpus.last.items + corpus.last.pages}</TableCell>
                      <TableCell>{corpus.last.written}</TableCell>
                      <TableCell>{corpus.last.removed}</TableCell>
                      <TableCell>{corpus.last.skipped}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
          <p className="text-muted-foreground" data-testid="pando-corpus-summary">
            {settings.documents} exported document{settings.documents === 1 ? '' : 's'}, last
            written {when(settings.lastExport ?? '')}.
          </p>
        </div>

        {progress === null ? null : (
          <div className="space-y-1" data-testid="pando-reindex-progress">
            <Progress value={progress.percent} label="Reindex progress" />
            <p className="text-muted-foreground">
              {PHASE_LABELS[progress.phase]}
              {progress.repo === '' ? '' : ` — ${progress.repo}`}
              {progress.message === '' ? '' : `: ${progress.message}`}
            </p>
          </div>
        )}

        {job === null ? null : (
          <div className="space-y-1" data-testid="pando-reindex-job">
            <p>
              Last reindex <code>{job.jobId}</code> — {PHASE_LABELS[job.phase]}
              {job.endedAt === undefined ? '' : ` at ${when(job.endedAt)}`}.
            </p>
            {job.kb === undefined ? null : (
              <p className="text-muted-foreground">
                Knowledge base: {job.kb.scanned} scanned, {job.kb.added} added, {job.kb.updated}{' '}
                updated, {job.kb.unchanged} unchanged, {job.kb.deleted} deleted.
              </p>
            )}
            {job.kbNote === undefined || job.kbNote === '' ? null : (
              <p className="text-muted-foreground">{job.kbNote}</p>
            )}
            {job.repos.map((repo) => (
              <p key={repo.repo} className="text-muted-foreground">
                {repo.repo}: {repo.export.written} written, {repo.export.removed} removed
                {repo.codeJob === undefined || repo.codeJob === ''
                  ? ''
                  : `, code job ${repo.codeJob}`}
                {repo.exportError === undefined || repo.exportError === ''
                  ? ''
                  : ` — export failed: ${repo.exportError}`}
                {repo.codeError === undefined || repo.codeError === ''
                  ? ''
                  : ` — code index failed: ${repo.codeError}`}
              </p>
            ))}
            {job.error === undefined || job.error === '' ? null : (
              <p className="text-destructive">{job.error}</p>
            )}
          </div>
        )}

        <p
          className="rounded-md border border-border bg-secondary/50 p-3"
          data-testid="pando-model-warning"
        >
          <strong className="font-medium">The embedding model is pinned configuration.</strong>{' '}
          Pando silently skips any chunk whose vector length differs from the query&rsquo;s — no
          error, no dimension guard — and the model is configured per Pando instance, not per
          corpus. Changing it therefore degrades recall invisibly for <em>every</em> consumer of
          that instance until a full reindex has re-embedded everything.
        </p>

        {error === null ? null : (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
