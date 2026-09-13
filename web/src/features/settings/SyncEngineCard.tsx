/**
 * Settings — the background job engine (story GIT-US-0081, epic GIT-EP-0015).
 *
 * One card, two halves, because they answer two questions a person asks in the
 * same breath: "what is the queue doing" and "why is it doing it that slowly".
 * The knobs are on top and the queue below them, so tuning and its effect are
 * on one screen instead of in a log file.
 *
 * The queue is read from `GET /api/v1/sync/jobs` and refreshed by the
 * `sync.job.*` stream — never polled. The stream already exists; a timer on top
 * of it would fight with it for no new information, and the listing it triggers
 * is the engine's own consistent snapshot, which the stream (a client can miss
 * frames) is not.
 *
 * Two things this card must get right about trust. Every error string in the
 * table came from a third-party tracker, so it is rendered as **plain text** —
 * there is no Markdown path here, sanitised or otherwise. And the four knobs
 * are checked against the companion's own ranges *before* a request, so a value
 * that cannot work is refused with an inline message rather than a round trip
 * and a problem document.
 *
 * The card renders on the reported surface rather than on the provider kind: a
 * runtime whose sync settings carry no `engine` half has no engine, and a table
 * of zero jobs would claim there is a queue that happens to be empty.
 */

import { useCallback, useEffect, useId, useState } from 'react';

import {
  SYNC_ENGINE_RANGES,
  type SyncEngineSettings,
  type SyncJob,
  type SyncJobState,
  type SyncSettings,
} from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useToast } from '@/components/ui/toast';
import { syncJobMessage } from '@/features/sync/job-messages';
import {
  useCancelSyncJob,
  useRetrySyncJob,
  useSyncJobEvents,
  useSyncJobs,
} from '@/features/sync/queries';

/** The editable half of the card, so a reload is a state reset and nothing else. */
type Draft = {
  workers: string;
  batchSize: string;
  rate: string;
  maxAttempts: string;
  pushOnSync: boolean;
};

function draftOf(settings: SyncSettings, engine: SyncEngineSettings): Draft {
  return {
    workers: String(engine.workers),
    batchSize: String(engine.batchSize),
    rate: String(engine.rate),
    maxAttempts: String(engine.maxAttempts),
    pushOnSync: settings.pushOnSync,
  };
}

/** Which states a chip tints how. Colour is never the only signal: each is labelled. */
const STATE_VARIANT: Record<SyncJobState, 'default' | 'accent' | 'success' | 'destructive'> = {
  queued: 'default',
  running: 'accent',
  done: 'success',
  failed: 'destructive',
  cancelled: 'default',
};

/** Retry is for a job that stopped; cancel for one that has not started or is in flight. */
function canRetry(job: SyncJob): boolean {
  return job.state === 'failed' || job.state === 'cancelled';
}

function canCancel(job: SyncJob): boolean {
  return job.state === 'queued' || job.state === 'running';
}

/**
 * Validates one knob against the companion's own range, so the message names
 * the field and the bounds rather than saying "invalid".
 */
function checkRange(label: string, raw: string, range: { min: number; max: number }): string {
  const value = Number(raw);
  if (raw.trim() === '' || Number.isNaN(value) || !Number.isFinite(value)) {
    return `${label} must be a number.`;
  }
  if (value < range.min || value > range.max) {
    return `${label} must be between ${String(range.min)} and ${String(range.max)}.`;
  }
  return '';
}

export function SyncEngineCard() {
  const provider = useOptionalProvider();
  const [settings, setSettings] = useState<SyncSettings | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!provider) return;
    setSettings(await provider.getSyncSettings());
  }, [provider]);

  useEffect(() => {
    void load().catch((cause: unknown) => {
      setLoadError(cause instanceof Error ? cause.message : String(cause));
    });
  }, [load]);

  if (!provider || settings === null) return null;
  // No engine half, no engine: browser-only mode and any companion built
  // without one land here, and the card stays out of the page entirely.
  if (settings.engine === undefined) return null;

  return (
    <SyncEngine
      settings={settings}
      engine={settings.engine}
      loadError={loadError}
      onSaved={setSettings}
    />
  );
}

function SyncEngine({
  settings,
  engine,
  loadError,
  onSaved,
}: {
  settings: SyncSettings;
  engine: SyncEngineSettings;
  loadError: string | null;
  onSaved: (settings: SyncSettings) => void;
}) {
  const provider = useOptionalProvider();
  const { toast } = useToast();
  const formId = useId();
  const [draft, setDraft] = useState<Draft>(() => draftOf(settings, engine));
  const [invalid, setInvalid] = useState<string>('');
  const [saving, setSaving] = useState(false);

  const jobs = useSyncJobs();
  useSyncJobEvents();
  const retry = useRetrySyncJob();
  const cancel = useCancelSyncJob();

  const set = (patch: Partial<Draft>) => {
    setDraft((current) => ({ ...current, ...patch }));
    setInvalid('');
  };

  const save = () => {
    const problem =
      checkRange('Workers', draft.workers, SYNC_ENGINE_RANGES.workers) ||
      checkRange('Batch size', draft.batchSize, SYNC_ENGINE_RANGES.batchSize) ||
      checkRange('Rate limit', draft.rate, SYNC_ENGINE_RANGES.rate) ||
      checkRange('Max attempts', draft.maxAttempts, SYNC_ENGINE_RANGES.maxAttempts);
    if (problem !== '') {
      // Refused here, before any request: a value out of range cannot work and
      // the companion would only tell us the same thing more slowly.
      setInvalid(problem);
      return;
    }
    if (!provider) return;

    setSaving(true);
    provider
      .updateSyncSettings({
        pushOnSync: draft.pushOnSync,
        engine: {
          workers: Number(draft.workers),
          batchSize: Number(draft.batchSize),
          rate: Number(draft.rate),
          maxAttempts: Number(draft.maxAttempts),
        },
      })
      .then((next) => {
        onSaved(next);
        toast({
          title: 'Sync engine updated',
          description:
            next.persisted === true
              ? 'The change was written to the configuration file.'
              : 'The running engine took the change, but the configuration file has no sync.engine section yet: it will not survive a restart.',
        });
      })
      .catch((cause: unknown) => {
        toast({
          title: 'Could not update the sync engine',
          description: syncJobMessage(cause),
          variant: 'destructive',
        });
      })
      .finally(() => {
        setSaving(false);
      });
  };

  const act = (action: 'retry' | 'cancel', job: SyncJob) => {
    const mutation = action === 'retry' ? retry : cancel;
    mutation.mutate(job.id, {
      onSuccess: (next) => {
        toast({
          title: action === 'retry' ? 'Job re-queued' : 'Job cancelled',
          description:
            action === 'retry' && next.id !== job.id
              ? `A cancelled job cannot be resumed, so it was queued again as ${next.id}.`
              : `${job.kind} · ${job.id}`,
        });
      },
      onError: (cause: unknown) => {
        toast({
          title: action === 'retry' ? 'Could not retry the job' : 'Could not cancel the job',
          description: syncJobMessage(cause),
          variant: 'destructive',
        });
      },
    });
  };

  const counts = jobs.data?.counts;
  const rows = jobs.data?.jobs ?? [];

  return (
    <Card>
      <CardHeader className="space-y-1">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle>Sync engine</CardTitle>
          {counts === undefined ? null : (
            <div className="flex flex-wrap items-center gap-1.5" data-testid="sync-job-counts">
              <Badge variant="default">{counts.queued} queued</Badge>
              <Badge variant="accent">{counts.running} running</Badge>
              <Badge variant="success">{counts.done} done</Badge>
              {counts.failed === 0 ? null : (
                <Badge variant="destructive">{counts.failed} failed</Badge>
              )}
              {counts.cancelled === 0 ? null : (
                <Badge variant="outline">{counts.cancelled} cancelled</Badge>
              )}
            </div>
          )}
        </div>
        <CardDescription>
          The background queue imports, comment pushes and knowledge-base publishes run on. It keeps
          working while you use the app, and it survives a restart through its journal.
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-6">
        {loadError === null ? null : (
          <p role="alert" className="text-sm text-destructive">
            {loadError}
          </p>
        )}

        <div className="space-y-3">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Knob
              id={`${formId}-workers`}
              label="Workers"
              hint={`${String(SYNC_ENGINE_RANGES.workers.min)}–${String(SYNC_ENGINE_RANGES.workers.max)}, applied at once`}
              value={draft.workers}
              onChange={(value) => {
                set({ workers: value });
              }}
            />
            <Knob
              id={`${formId}-batch`}
              label="Batch size"
              hint={`${String(SYNC_ENGINE_RANGES.batchSize.min)}–${String(SYNC_ENGINE_RANGES.batchSize.max)}, applied at once`}
              value={draft.batchSize}
              onChange={(value) => {
                set({ batchSize: value });
              }}
            />
            <Knob
              id={`${formId}-rate`}
              label="Rate limit"
              hint="Requests per second; a negative value removes the limit"
              value={draft.rate}
              onChange={(value) => {
                set({ rate: value });
              }}
            />
            <Knob
              id={`${formId}-attempts`}
              label="Max attempts"
              hint="Fixed when the engine starts: it applies from the next restart"
              value={draft.maxAttempts}
              onChange={(value) => {
                set({ maxAttempts: value });
              }}
            />
          </div>

          <div className="flex items-start gap-2">
            <Checkbox
              id={`${formId}-push`}
              checked={draft.pushOnSync}
              onChange={(event) => {
                set({ pushOnSync: event.target.checked });
              }}
              className="mt-0.5"
            />
            <div>
              <Label htmlFor={`${formId}-push`}>Push after a successful sync</Label>
              <p className="text-xs text-muted-foreground">
                What the engine does once it has integrated: push straight away, or leave the
                commits local.
              </p>
            </div>
          </div>

          {invalid === '' ? null : (
            <p role="alert" className="text-sm text-destructive">
              {invalid}
            </p>
          )}

          <div className="flex items-center gap-2">
            <Button onClick={save} disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </Button>
            <span className="text-xs text-muted-foreground">
              {engine.running ? 'The engine is running.' : 'The engine is not running.'}
            </span>
          </div>
        </div>

        <section aria-label="Job queue" className="space-y-2">
          <h3 className="section-label">Queue</h3>
          {jobs.isError ? (
            <p role="alert" className="text-sm text-destructive">
              {syncJobMessage(jobs.error)}
            </p>
          ) : null}
          {jobs.isPending ? (
            <p className="text-sm text-muted-foreground">Loading the queue…</p>
          ) : null}
          {jobs.isSuccess && rows.length === 0 ? (
            <p className="empty-state">
              The queue is empty. Nothing is waiting and nothing failed.
            </p>
          ) : null}
          {rows.length > 0 ? (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Kind</TableHead>
                    <TableHead>Key</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead>Attempts</TableHead>
                    <TableHead>Next attempt</TableHead>
                    <TableHead>Last error</TableHead>
                    <TableHead>
                      <span className="sr-only">Actions</span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((job) => (
                    <TableRow key={job.id}>
                      <TableCell className="font-mono text-xs">{job.kind}</TableCell>
                      <TableCell className="font-mono text-xs">{job.key}</TableCell>
                      <TableCell>
                        <Badge variant={STATE_VARIANT[job.state]}>{job.state}</Badge>
                        {job.deadLetter === true ? (
                          <Badge variant="warning" size="sm">
                            dead letter
                          </Badge>
                        ) : null}
                      </TableCell>
                      <TableCell>{job.attempts}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {job.nextAttempt ?? '—'}
                      </TableCell>
                      {/* The tracker's own words: plain text, never markup. */}
                      <TableCell className="max-w-xs text-xs text-destructive">
                        {job.lastError?.message ?? ''}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            variant="outline"
                            size="sm"
                            disabled={!canRetry(job) || retry.isPending}
                            onClick={() => {
                              act('retry', job);
                            }}
                            aria-label={`Retry ${job.id}`}
                          >
                            Retry
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={!canCancel(job) || cancel.isPending}
                            onClick={() => {
                              act('cancel', job);
                            }}
                            aria-label={`Cancel ${job.id}`}
                          >
                            Cancel
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : null}
        </section>
      </CardContent>
    </Card>
  );
}

function Knob({
  id,
  label,
  hint,
  value,
  onChange,
}: {
  id: string;
  label: string;
  hint: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-1">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        value={value}
        onChange={(event) => {
          onChange(event.target.value);
        }}
      />
      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  );
}
