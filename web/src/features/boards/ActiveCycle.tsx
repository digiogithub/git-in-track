import { CalendarClock, CircleDashed, Snowflake } from 'lucide-react';

import type { SprintMetricsView, SprintSummary } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Progress } from '@/components/ui/progress';
import { BurndownChart } from '@/features/metrics/BurndownChart';
import { formatNumber } from '@/features/metrics/chart';
import { useSprintMetrics } from '@/features/metrics/metrics-queries';

/**
 * The running cycle, summarised (story GIT-US-0089, task GIT-T-0161).
 *
 * It sits at the top of the scrum board so that "where does the sprint stand"
 * is answered without leaving the board. Two rules shape what it may say:
 *
 *  1. **The status is derived, never stored** (ADR-034). `SprintSummary.status`
 *     comes from the core, computed from the dates against the host's day, so a
 *     sprint with no dates is a draft and this component never recomputes any
 *     of it. A draft has no progress to show — it has a planning state.
 *  2. **A chart is never shown without its provenance** (ADR-017). The note is
 *     printed above the burndown, always, because a series a team cannot place
 *     is a series it cannot act on. A closed sprint reads its stored snapshot
 *     and says so: after the close the items have left the scope, so the frozen
 *     block is the only truthful answer (R-MET-12).
 */
export function ActiveCycle({ sprint }: { sprint: SprintSummary }) {
  // A draft has no days, so there is no series to reconstruct and no request
  // worth making for one.
  const draft = sprint.status === 'draft';
  const metrics = useSprintMetrics(draft ? undefined : sprint.id);

  return (
    <section aria-label="Active cycle" data-testid="active-cycle" className="space-y-3">
      <CycleDates sprint={sprint} />

      {draft ? (
        <DraftPlanningState sprint={sprint} />
      ) : (
        <CycleProgress sprint={sprint} />
      )}

      {draft ? null : <CycleBurndown sprintId={sprint.id} metrics={metrics} />}
    </section>
  );
}

/** The date range and what the derived status makes of it. */
function CycleDates({ sprint }: { sprint: SprintSummary }) {
  return (
    <p className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
      <Badge variant={STATUS_TONE[sprint.status]} size="sm" className="font-normal">
        {STATUS_LABEL[sprint.status]}
      </Badge>
      {sprint.start && sprint.end ? (
        <span>
          {sprint.start} → {sprint.end}
        </span>
      ) : null}
      <span>{timing(sprint)}</span>
    </p>
  );
}

/**
 * A draft says what it is missing instead of showing a bar at zero. A progress
 * bar over a sprint nobody has scheduled would read as "no work done" rather
 * than "no sprint yet", which is a different and much worse claim.
 */
function DraftPlanningState({ sprint }: { sprint: SprintSummary }) {
  return (
    <div className="empty-state flex items-start gap-3 text-left">
      <CircleDashed aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0 text-subtle-foreground" />
      <div className="space-y-1">
        <p className="text-sm font-medium text-foreground">Still a draft</p>
        <p className="text-xs text-muted-foreground">
          {sprint.metrics.items === 0
            ? 'Nothing is in its scope yet.'
            : `${String(sprint.metrics.items)} item(s) planned, ${formatNumber(sprint.metrics.points)} points.`}{' '}
          Adding a start and an end date is what schedules it.
        </p>
      </div>
    </div>
  );
}

/** Done against committed points, plus the scope that arrived after the start. */
function CycleProgress({ sprint }: { sprint: SprintSummary }) {
  const committed = sprint.metrics.committedPoints;
  // Before a sprint starts nothing is committed yet, so the whole scope is the
  // only honest denominator.
  const target = committed > 0 ? committed : sprint.metrics.points;

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2 text-xs">
        <span className="text-muted-foreground">
          {committed > 0 ? 'Done against committed' : 'Done against scope'}
        </span>
        <span className="tabular-nums font-medium text-foreground">
          {formatNumber(sprint.metrics.donePoints)} of {formatNumber(target)} points
        </span>
      </div>
      <Progress
        value={sprint.metrics.donePoints}
        max={target}
        label={`${sprint.title}: ${formatNumber(sprint.metrics.donePoints)} of ${formatNumber(target)} points done`}
      />
      <dl className="flex flex-wrap gap-4 text-xs">
        <Figure label="Committed" value={`${formatNumber(committed)} points`} />
        <Figure
          label="Completed"
          value={`${formatNumber(sprint.metrics.donePoints)} of ${formatNumber(sprint.metrics.points)} points`}
        />
        <Figure
          label="Items"
          value={`${String(sprint.metrics.done)} of ${String(sprint.metrics.items)} done`}
        />
        <Figure
          label="Added mid-sprint"
          value={sprint.metrics.added === 0 ? 'none' : String(sprint.metrics.added)}
        />
        {sprint.metrics.unresolved > 0 ? (
          <Figure label="Unresolved" value={String(sprint.metrics.unresolved)} />
        ) : null}
      </dl>
    </div>
  );
}

/**
 * The compact burndown, with its provenance above it.
 *
 * The chart is the existing `BurndownChart` — no charting dependency is added
 * for a second, smaller copy of the same picture.
 */
function CycleBurndown({
  sprintId,
  metrics,
}: {
  sprintId: string;
  metrics: ReturnType<typeof useSprintMetrics>;
}) {
  if (metrics.isPending) {
    return <p className="text-xs text-muted-foreground">Reading the history…</p>;
  }
  if (metrics.isError || !metrics.data) {
    return (
      <p className="text-xs text-destructive">
        The burndown of {sprintId} could not be read: {metrics.error?.message}
      </p>
    );
  }
  return (
    <div className="space-y-2">
      <CycleProvenance view={metrics.data} />
      <BurndownChart burndown={metrics.data.burndown} />
    </div>
  );
}

/**
 * One line saying where the curve came from. It is above the chart and it is
 * never conditional: ADR-017 makes the provenance part of the answer, not
 * decoration on it.
 */
export function CycleProvenance({ view }: { view: SprintMetricsView }) {
  const frozen = view.provenance.source === 'snapshot';
  const Icon = frozen ? Snowflake : CalendarClock;
  return (
    <p
      role="note"
      className="flex items-start gap-2 rounded-md border border-border bg-surface-muted px-3 py-2 text-xs text-muted-foreground"
    >
      <Icon aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0" />
      <span>
        {frozen ? <span className="font-medium text-foreground">Frozen at the close. </span> : null}
        {view.provenance.note}
      </span>
    </p>
  );
}

/** One figure of the cycle header. */
function Figure({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{value}</dd>
    </div>
  );
}

/** The human name of each derived status. */
const STATUS_LABEL: Record<SprintSummary['status'], string> = {
  current: 'Current',
  upcoming: 'Upcoming',
  draft: 'Draft',
  completed: 'Completed',
};

/**
 * The tone each derived status is chipped in. `current` is the only one that
 * earns attention; the rest are context, and `completed` is deliberately
 * neutral rather than a success — a sprint being over says nothing about how
 * it went.
 */
const STATUS_TONE: Record<SprintSummary['status'], 'info' | 'outline' | 'default'> = {
  current: 'info',
  upcoming: 'outline',
  draft: 'outline',
  completed: 'default',
};

/** What the dates mean today, in one phrase. */
function timing(sprint: SprintSummary): string {
  switch (sprint.status) {
    case 'draft':
      return 'No dates yet';
    case 'upcoming':
      return `Starts on ${sprint.start ?? ''}`;
    case 'completed':
      return 'Sprint over';
    default:
      return `${String(sprint.remainingDays)} of ${String(sprint.totalDays)} days left`;
  }
}
