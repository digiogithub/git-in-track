/**
 * "Import from YouTrack" (story GIT-US-0059, task GIT-T-0108).
 *
 * The dialog is one linear flow with four stops — pick, preview, run, summary —
 * and it is linear on purpose: an import writes items into a git repository,
 * and the step that makes that safe is the one where the user sees exactly what
 * would be written *before* anything is. Preview and run are given the same
 * options object, so "what preview showed me" and "what run did" cannot drift.
 *
 * Running is not a spinner. The companion enqueues the import on its job engine
 * and answers a job id; the progress strip is fed by the `sync.job.*` events,
 * which are coalesced server-side to one frame per 500 ms per group with
 * terminal frames never throttled — so this component adds no throttling of its
 * own and treats a jump in the counts, or a missing intermediate frame, as
 * normal. A runtime that runs the import inline answers the finished result
 * instead and the strip is skipped; both end at the same summary.
 *
 * The summary is per issue rather than a count, because a partial failure is
 * the interesting case: eighteen issues landed and two did not, and the two are
 * the only ones worth a person's time. Their error text comes from the job the
 * engine recorded, already redacted, and is rendered as plain text.
 *
 * The dialog writes nothing itself. It calls the import operations and lets the
 * vault do the writing, which is what keeps one implementation of "import an
 * issue" behind REST, MCP and the CLI alike.
 */

import { useEffect, useState } from 'react';

import type {
  YouTrackImportIssueResult,
  YouTrackImportOptions,
  YouTrackImportPreviewResult,
  YouTrackImportResult,
  YouTrackIssue,
  YouTrackIssuePreset,
  YouTrackScope,
} from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Progress } from '@/components/ui/progress';
import { TooltipProvider } from '@/components/ui/tooltip';
import { FeatureLink } from '@/features/backlog/FeatureLink';
import { youtrackMessage } from '@/features/settings/youtrack-messages';
import { useSyncJob, useSyncJobEvents } from '@/features/sync/queries';
import { defaultImportOptions, type ImportOptionsDraft } from '@/features/youtrack/import-model';
import { ImportOptions } from '@/features/youtrack/ImportOptions';
import { ImportPreviewTable } from '@/features/youtrack/ImportPreviewTable';
import { ImportQueryBar } from '@/features/youtrack/ImportQueryBar';
import { useYouTrackImportPreview, useYouTrackImportRun } from '@/features/youtrack/queries';

/** Where the flow is. `running` is the only state a person cannot leave. */
type Phase = 'pick' | 'running' | 'summary';

/** What the progress strip knows, straight from the last frame it saw. */
type RunProgress = {
  jobId: string;
  processed: number;
  total: number;
  /** Empty until a terminal frame arrives: `done`, `cancelled` or `failed`. */
  finished: '' | 'done' | 'cancelled' | 'failed';
  /** The failure text of a terminal `sync.job.failed` frame. */
  error: string;
};

export type ImportDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The git-in-track project the issues are imported into. */
  projectKey: string;
};

export function ImportDialog({ open, onOpenChange, projectKey }: ImportDialogProps) {
  const scope: YouTrackScope = { projectKey };
  const [phase, setPhase] = useState<Phase>('pick');
  const [text, setText] = useState('');
  const [preset, setPreset] = useState<YouTrackIssuePreset>('');
  const [selected, setSelected] = useState<YouTrackIssue[]>([]);
  const [options, setOptions] = useState<ImportOptionsDraft>(defaultImportOptions);
  const [preview, setPreview] = useState<YouTrackImportPreviewResult | null>(null);
  const [result, setResult] = useState<YouTrackImportResult | null>(null);
  const [progress, setProgress] = useState<RunProgress | null>(null);
  const [error, setError] = useState<string | null>(null);

  const previewMutation = useYouTrackImportPreview(scope);
  const runMutation = useYouTrackImportRun(scope);

  // Every frame of the job being watched, unthrottled. A frame for another job
  // is not this dialog's business, and `resync` carries no job at all.
  useSyncJobEvents((event) => {
    setProgress((current) => {
      if (current === null || event.id !== current.jobId) return current;
      if (event.phase === 'failed') {
        return { ...current, finished: 'failed', error: event.error };
      }
      if (event.phase === 'done') {
        return {
          ...current,
          processed: event.total === 0 ? current.processed : event.total,
          total: event.total === 0 ? current.total : event.total,
          finished: event.state === 'cancelled' ? 'cancelled' : 'done',
        };
      }
      return {
        ...current,
        processed: Math.max(current.processed, event.processed),
        total: event.total === 0 ? current.total : event.total,
      };
    });
  });

  // The events carry counts, never the per-issue outcome, so the job itself is
  // read once it is over: `lastError` is the redacted text of the failure.
  const watched = progress !== null && progress.finished !== '' && result === null;
  const job = useSyncJob(progress?.jobId ?? '', watched);

  useEffect(() => {
    if (progress === null || progress.finished === '') return;
    setPhase('summary');
  }, [progress]);

  const options_: YouTrackImportOptions = {
    project: projectKey,
    ids: selected.map((issue) => issue.idReadable),
    ...options,
  };

  const reset = () => {
    setPhase('pick');
    setText('');
    setPreset('');
    setSelected([]);
    setOptions(defaultImportOptions);
    setPreview(null);
    setResult(null);
    setProgress(null);
    setError(null);
    previewMutation.reset();
    runMutation.reset();
  };

  const close = () => {
    onOpenChange(false);
    reset();
  };

  const runPreview = () => {
    setError(null);
    previewMutation.mutate(options_, {
      onSuccess: (plan) => {
        setPreview(plan);
      },
      onError: (cause: unknown) => {
        setError(youtrackMessage(cause));
      },
    });
  };

  const runImport = () => {
    setError(null);
    setPhase('running');
    runMutation.mutate(options_, {
      onSuccess: (run) => {
        if (run.result !== null) {
          // The runtime ran the import inline: there is nothing to watch.
          setResult(run.result);
          setPhase('summary');
          return;
        }
        setProgress({
          jobId: run.jobId,
          processed: 0,
          total: preview?.issues.length ?? selected.length,
          finished: '',
          error: '',
        });
      },
      onError: (cause: unknown) => {
        setError(youtrackMessage(cause));
        setPhase('pick');
      },
    });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          onOpenChange(true);
          return;
        }
        // A run is off the request already: closing the dialog abandons the
        // view of it, never the import.
        close();
      }}
    >
      <DialogContent className="w-[min(56rem,calc(100vw-2rem))]">
        <TooltipProvider delayDuration={200}>
          <DialogHeader>
            <DialogTitle>Import from YouTrack</DialogTitle>
            <DialogDescription>
              Pick the issues, see exactly what would be created or updated, then run the import
              into <strong>{projectKey}</strong>.
            </DialogDescription>
          </DialogHeader>

          <div className="max-h-[70vh] space-y-5 overflow-y-auto pr-1">
            {error === null ? null : (
              <p
                role="alert"
                className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
              >
                {error}
              </p>
            )}

            {phase === 'pick' ? (
              <>
                <ImportQueryBar
                  text={text}
                  onTextChange={setText}
                  preset={preset}
                  onPresetChange={(next) => {
                    setPreset(next);
                    // A new query invalidates the plan that came from the old one.
                    setPreview(null);
                  }}
                  selected={selected}
                  onSelectedChange={(next) => {
                    setSelected(next);
                    setPreview(null);
                  }}
                  scope={scope}
                />

                <ImportOptions
                  value={options}
                  onChange={(next) => {
                    setOptions(next);
                    // The plan was computed for the previous options.
                    setPreview(null);
                  }}
                  disabled={previewMutation.isPending}
                />

                {preview === null ? null : (
                  <section aria-label="Import preview" className="space-y-2">
                    <h3 className="text-base font-semibold tracking-tight">Preview</h3>
                    <ImportPreviewTable issues={preview.issues} warnings={preview.warnings ?? []} />
                  </section>
                )}
              </>
            ) : null}

            {phase === 'running' ? (
              <RunProgressStrip progress={progress} pending={runMutation.isPending} />
            ) : null}

            {phase === 'summary' ? (
              <ImportSummary
                projectKey={projectKey}
                result={result}
                progress={progress}
                jobError={job.data?.lastError?.message ?? ''}
              />
            ) : null}
          </div>

          <footer className="mt-5 flex flex-wrap items-center justify-end gap-2">
            {phase === 'pick' ? (
              <>
                <Button variant="ghost" onClick={close}>
                  Cancel
                </Button>
                <Button
                  variant="outline"
                  onClick={runPreview}
                  disabled={selected.length === 0 || previewMutation.isPending}
                >
                  {previewMutation.isPending ? 'Previewing…' : 'Preview'}
                </Button>
                <Button
                  variant="accent"
                  onClick={runImport}
                  disabled={preview === null || preview.issues.length === 0}
                >
                  Run import
                </Button>
              </>
            ) : null}
            {phase === 'running' ? (
              <Button variant="ghost" onClick={close}>
                Close
              </Button>
            ) : null}
            {phase === 'summary' ? (
              <>
                <Button variant="ghost" onClick={reset}>
                  Import more
                </Button>
                <Button variant="accent" onClick={close}>
                  Done
                </Button>
              </>
            ) : null}
          </footer>
        </TooltipProvider>
      </DialogContent>
    </Dialog>
  );
}

/**
 * The progress strip. It says what it knows and nothing more: a count that has
 * not moved for a second is normal, because progress is coalesced at the
 * source, and a total of zero is a job whose size the engine has not reported.
 */
function RunProgressStrip({
  progress,
  pending,
}: {
  progress: RunProgress | null;
  pending: boolean;
}) {
  if (progress === null) {
    return (
      <p role="status" className="text-sm text-muted-foreground">
        {pending ? 'Queueing the import…' : 'Waiting for the job engine…'}
      </p>
    );
  }
  return (
    <div role="status" className="space-y-2">
      <p className="text-sm">
        Importing {String(progress.processed)} of {String(progress.total)} issues…
      </p>
      <Progress value={progress.processed} max={progress.total} label="Import progress" />
      <p className="text-xs text-muted-foreground">
        The import runs in the background. Closing this dialog does not stop it — the queue in
        Settings shows it, and a toast will say when it finishes.
      </p>
    </div>
  );
}

/**
 * The result, per issue. A created item links to itself; a failed one carries
 * the engine's message as plain text.
 */
function ImportSummary({
  projectKey,
  result,
  progress,
  jobError,
}: {
  projectKey: string;
  result: YouTrackImportResult | null;
  progress: RunProgress | null;
  jobError: string;
}) {
  if (progress?.finished === 'cancelled') {
    return (
      <p role="status" className="text-sm">
        The import was cancelled. Whatever had already been written is in the backlog; nothing else
        was.
      </p>
    );
  }

  // A job that failed outright never produced a per-issue result: the engine's
  // own message is everything there is to say, and it is already redacted.
  if (result === null) {
    const failed = progress?.finished === 'failed';
    const message = jobError !== '' ? jobError : (progress?.error ?? '');
    return (
      <div role="status" className="space-y-2 text-sm">
        <p>
          {failed
            ? 'The import failed.'
            : `The import finished: ${String(progress?.processed ?? 0)} issues processed.`}
        </p>
        {message === '' ? null : (
          <p className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-destructive">
            {message}
          </p>
        )}
        <p className="text-xs text-muted-foreground">
          The queue in Settings holds the job, its attempts and its last error.
        </p>
      </div>
    );
  }

  return (
    <div role="status" className="space-y-3">
      <p className="flex flex-wrap items-center gap-2 text-sm" data-testid="import-summary-counts">
        <Badge variant="success">{result.created} created</Badge>
        <Badge variant="info">{result.updated} updated</Badge>
        {result.failed === 0 ? null : <Badge variant="destructive">{result.failed} failed</Badge>}
      </p>

      <ul aria-label="Imported issues" className="space-y-1 text-sm">
        {result.issues.map((issue) => (
          <SummaryRow key={issue.youtrackId} projectKey={projectKey} issue={issue} />
        ))}
      </ul>
    </div>
  );
}

function SummaryRow({
  projectKey,
  issue,
}: {
  projectKey: string;
  issue: YouTrackImportIssueResult;
}) {
  const failed = issue.error !== undefined && issue.error !== '';
  return (
    <li className="flex flex-wrap items-baseline gap-2 border-b border-border py-1 last:border-b-0">
      <span className="font-mono text-xs text-muted-foreground">{issue.youtrackId}</span>
      {failed ? (
        <>
          <Badge variant="destructive" size="sm">
            Failed
          </Badge>
          {/* Third-party text: plain, never Markdown and never HTML. */}
          <span className="text-destructive">{issue.error}</span>
        </>
      ) : (
        <>
          <Badge variant={issue.action === 'create' ? 'success' : 'info'} size="sm">
            {issue.action === 'create' ? 'Created' : 'Updated'}
          </Badge>
          {issue.itemId === undefined || issue.itemId === '' ? null : (
            <FeatureLink
              to="/p/$project/items/$id"
              params={{ project: projectKey, id: issue.itemId }}
              className="font-mono text-xs text-accent underline underline-offset-4"
            >
              {issue.itemId}
            </FeatureLink>
          )}
        </>
      )}
    </li>
  );
}
