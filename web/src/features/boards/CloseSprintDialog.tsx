import { TriangleAlert } from 'lucide-react';
import { useEffect, useId, useState } from 'react';

import type { SprintCarryResult, SprintSummary, SprintTransferMode } from '@/api/provider';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Combobox } from '@/components/ui/combobox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { useToast } from '@/components/ui/toast';
import {
  useCloseSprint,
  useCloseSprintPreview,
  useSprints,
} from '@/features/boards/sprint-queries';
import { formatNumber } from '@/features/metrics/chart';

/**
 * Closing a sprint (story GIT-US-0089, task GIT-T-0165).
 *
 * The dialog is built around one rule: **nothing is committed before a person
 * has seen what would move where**. Opening it runs the close with
 * `dryRun: true`, which computes the entire report — the counts, the
 * destination each unfinished reference would take, and every refusal — while
 * writing nothing at all and publishing no event. Changing the destination
 * re-runs that preview, so the report on screen is always the plan the confirm
 * button would carry out.
 *
 * The refusals are the second rule. A reference into a project this machine has
 * not cloned cannot be written, and that is reported *here*, as an inline
 * banner above the confirm button, rather than as a toast after the fact: a
 * partial close a team only discovers afterwards is how work gets lost.
 */
export function CloseSprintDialog({
  sprint,
  open,
  onOpenChange,
}: {
  sprint: SprintSummary;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [mode, setMode] = useState<SprintTransferMode>('none');
  const [target, setTarget] = useState('');
  const [targetText, setTargetText] = useState('');
  const [acknowledged, setAcknowledged] = useState(false);
  const close = useCloseSprint();
  const { toast } = useToast();
  const modeName = useId();
  const acknowledgeId = useId();

  const transfer = { mode, ...(mode === 'next' && target !== '' ? { target } : {}) };
  const preview = useCloseSprintPreview(
    sprint.id,
    { transfer, ...(sprint.rev === undefined ? {} : { rev: sprint.rev }) },
    open,
  );

  // Only sprints of this board that are not over can receive work: moving items
  // into a sprint that has already ended would make its numbers lie, which the
  // core refuses outright with `sprint_target_completed`.
  const candidates = useSprints({ board: sprint.board });
  const targets = (candidates.data ?? []).filter(
    (row) => row.id !== sprint.id && row.status !== 'completed',
  );

  // A destination the user has not confirmed yet must never be treated as
  // confirmed after the dialog is reopened.
  useEffect(() => {
    if (open) return;
    setMode('none');
    setTarget('');
    setTargetText('');
    setAcknowledged(false);
  }, [open]);

  // The acknowledgement is about a specific set of refusals; a new preview is a
  // new set, so it has to be read again.
  useEffect(() => {
    setAcknowledged(false);
  }, [preview.data]);

  const report = preview.data?.report;
  const refused = (report?.carried ?? []).filter(
    (one): one is SprintCarryResult & { error: string } => one.error !== undefined,
  );
  const blocked = refused.length > 0 && !acknowledged;
  const ready = preview.isSuccess && !preview.isFetching && !blocked;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Close {sprint.title}</DialogTitle>
          <DialogDescription>
            Closing grades the sprint and freezes its numbers. Nothing below has happened yet: this
            is what closing it now would do.
          </DialogDescription>
        </DialogHeader>

        {preview.isPending ? (
          <p className="text-sm text-muted-foreground">Working out what would move…</p>
        ) : null}

        {preview.isError ? (
          <p role="alert" className="text-sm text-destructive">
            This close could not be previewed: {preview.error.message}
          </p>
        ) : null}

        {report ? (
          <div className="space-y-4">
            {/* `dl` carries no implicit role, so the group is named explicitly:
                the three counts are one labelled unit, not three loose numbers. */}
            <dl
              role="group"
              aria-label="What this close would grade"
              className="grid grid-cols-3 gap-2 text-center"
            >
              <Count label="Finished" value={report.completed.length} />
              <Count label="Unfinished" value={report.incomplete.length} />
              <Count label="Unresolved" value={report.unresolved.length} />
            </dl>
            <p className="text-xs text-muted-foreground">
              {formatNumber(report.completedPoints)} of{' '}
              {formatNumber(report.completedPoints + report.incompletePoints)} points finished.
              {report.unresolved.length > 0
                ? ' An unresolved reference belongs to a project nothing here can read; it is reported, never counted as done.'
                : ''}
            </p>

            <fieldset className="space-y-2">
              <legend className="section-label mb-1">Where the unfinished work goes</legend>
              <Destination
                name={modeName}
                value="none"
                current={mode}
                onSelect={setMode}
                title="Leave it where it is"
                hint="The sprint closes and no item is touched."
              />
              <Destination
                name={modeName}
                value="next"
                current={mode}
                onSelect={setMode}
                title="Carry it into another sprint"
                hint="One write to the sprint files; the items themselves are not changed."
              />
              <Destination
                name={modeName}
                value="backlog"
                current={mode}
                onSelect={setMode}
                title="Send it back to the backlog"
                hint="Writes each item in its own repository, so a project nobody cloned is refused."
              />
            </fieldset>

            {mode === 'next' ? (
              <div className="space-y-1">
                <Label htmlFor="close-sprint-target">Target sprint</Label>
                <Combobox
                  id="close-sprint-target"
                  label="Target sprint"
                  value={targetText}
                  onValueChange={setTargetText}
                  options={targets.filter((row) =>
                    `${row.id} ${row.title}`.toLowerCase().includes(targetText.toLowerCase()),
                  )}
                  getOptionKey={(row) => row.id}
                  isOptionSelected={(row) => row.id === target}
                  renderOption={(row) => (
                    <span className="flex items-center justify-between gap-2">
                      <span>{row.title}</span>
                      <span className="text-xs text-muted-foreground">{row.status}</span>
                    </span>
                  )}
                  onSelect={(row) => {
                    setTarget(row.id);
                    setTargetText(row.title);
                  }}
                  placeholder="The earliest planned sprint of this board"
                  emptyLabel="No sprint of this board can take the work; every other one is over."
                />
                <p className="text-xs text-muted-foreground">
                  Only sprints of {sprint.board} that are not over are offered. Leave it empty to
                  use the earliest planned one.
                </p>
              </div>
            ) : null}

            {refused.length > 0 ? (
              <div
                role="alert"
                className="space-y-2 rounded-lg border border-warning/40 bg-warning/15 p-3 text-sm text-warning"
              >
                <p className="flex items-center gap-2 font-medium">
                  <TriangleAlert aria-hidden="true" className="h-4 w-4 shrink-0" />
                  {refused.length} item(s) would stay exactly where they are
                </p>
                <ul aria-label="Refused items" className="space-y-1 text-xs">
                  {refused.map((one) => (
                    <li key={one.ref}>
                      <span className="font-mono">{one.ref}</span> — {one.error}
                    </li>
                  ))}
                </ul>
                <div className="flex items-center gap-2">
                  <Checkbox
                    id={acknowledgeId}
                    checked={acknowledged}
                    onChange={(event) => {
                      setAcknowledged(event.target.checked);
                    }}
                  />
                  <Label htmlFor={acknowledgeId} className="text-xs font-normal text-warning">
                    Close anyway; I have read what will not move
                  </Label>
                </div>
              </div>
            ) : null}
          </div>
        ) : null}

        <DialogFooter>
          <Button
            variant="ghost"
            onClick={() => {
              onOpenChange(false);
            }}
          >
            Cancel
          </Button>
          <Button
            disabled={!ready || close.isPending}
            onClick={() => {
              close.mutate(
                {
                  id: sprint.id,
                  transfer,
                  ...(sprint.rev === undefined ? {} : { rev: sprint.rev }),
                },
                {
                  onSuccess: (result) => {
                    const moved = (result.report?.carried ?? []).filter(
                      (one) => one.error === undefined && one.action !== 'leave',
                    );
                    const failed = (result.report?.carried ?? []).filter(
                      (one) => one.error !== undefined,
                    );
                    toast({
                      variant: failed.length > 0 ? 'destructive' : 'default',
                      title: `${sprint.title} is closed`,
                      description:
                        failed.length > 0
                          ? `${moved.length} item(s) moved; ${failed.length} could not be written and stayed where they were.`
                          : `${moved.length} item(s) moved. Its numbers are frozen in the sprint file.`,
                    });
                    onOpenChange(false);
                  },
                  onError: (error) => {
                    toast({
                      variant: 'destructive',
                      title: 'The sprint could not be closed',
                      description: error.message,
                    });
                  },
                },
              );
            }}
          >
            Close sprint
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** One of the three counts a close grades. */
function Count({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-md border border-border bg-surface-muted px-2 py-3">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-lg font-semibold tabular-nums text-foreground">{value}</dd>
    </div>
  );
}

/** One destination of the bulk choice, as a radio with its consequence spelled out. */
function Destination({
  name,
  value,
  current,
  onSelect,
  title,
  hint,
}: {
  name: string;
  value: SprintTransferMode;
  current: SprintTransferMode;
  onSelect: (mode: SprintTransferMode) => void;
  title: string;
  hint: string;
}) {
  const id = `${name}-${value}`;
  return (
    <div className="flex items-start gap-2 rounded-md border border-border p-2 text-sm hover:border-border-strong">
      <input
        id={id}
        type="radio"
        name={name}
        value={value}
        checked={current === value}
        className="mt-1 accent-accent"
        onChange={() => {
          onSelect(value);
        }}
      />
      <label htmlFor={id} className="cursor-pointer">
        <span className="block font-medium text-foreground">{title}</span>
        <span className="block text-xs text-muted-foreground">{hint}</span>
      </label>
    </div>
  );
}
