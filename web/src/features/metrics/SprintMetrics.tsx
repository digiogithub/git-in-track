import { Link, useParams } from '@tanstack/react-router';
import { Info, TriangleAlert } from 'lucide-react';

import type { MetricsProvenance, MetricStat, SprintMetricsView } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useSprints } from '@/features/boards/sprint-queries';
import { BurndownChart } from '@/features/metrics/BurndownChart';
import { BAND_LABELS, bandsInUse, formatNumber, shortDate } from '@/features/metrics/chart';
import { CumulativeFlowChart } from '@/features/metrics/CumulativeFlowChart';
import { useSprintMetrics } from '@/features/metrics/metrics-queries';

/**
 * Sprint metrics (docs/05-web-app.md §16, docs/04-team-repository.md §12,
 * story GIT-US-0028).
 *
 * The page leads with where its numbers came from, because that is the part a
 * team has to know before it acts on a chart: the companion reconstructs the
 * series from the git history of the item files, and a browser-only session
 * says so and shows the approximation it can draw from the `updated` stamps.
 * Every chart is followed by the table of numbers behind it.
 */
export function SprintMetrics() {
  const { sprintId } = useParams({ from: '/metrics/$sprintId' });
  const metrics = useSprintMetrics(sprintId);

  if (metrics.isPending) {
    return <p className="text-sm text-muted-foreground">Reading the history…</p>;
  }
  if (metrics.isError || !metrics.data) {
    return (
      <p className="text-sm text-destructive">
        The metrics of {sprintId} could not be computed: {metrics.error?.message}
      </p>
    );
  }
  return <SprintMetricsBody view={metrics.data} />;
}

/** The page body, split out so the tests can render it without a router. */
export function SprintMetricsBody({ view }: { view: SprintMetricsView }) {
  const { sprint, burndown, flow, stats, provenance } = view;
  const bands = bandsInUse(flow);
  const observed = burndown.points.filter((point) => point.observed);
  const latest = observed.at(-1);

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">{sprint.title} — metrics</h1>
        <p className="text-sm text-muted-foreground">
          {sprint.start} to {sprint.end} · {sprint.metrics.items} items · board {sprint.board}
        </p>
      </header>

      <ProvenanceBanner provenance={provenance} />

      <section aria-label="Headline numbers" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile
          label="Committed"
          value={`${formatNumber(burndown.committedPoints)} pts`}
          hint={`${sprint.metrics.added} pulled in mid-sprint`}
        />
        <StatTile
          label="Remaining"
          value={latest ? `${formatNumber(latest.remaining)} pts` : '—'}
          hint={latest ? `on ${shortDate(latest.date)}` : 'not measured yet'}
        />
        <StatTile
          label="Throughput"
          value={String(stats.throughput)}
          hint={`${formatNumber(stats.throughputPerWeek)} items per week`}
        />
        <StatTile
          label="Cycle time"
          value={stats.cycleTime.count > 0 ? `${formatNumber(stats.cycleTime.median)} d` : '—'}
          hint={
            stats.cycleTime.count > 0
              ? `median of ${stats.cycleTime.count}; 85th percentile ${formatNumber(stats.cycleTime.p85)} d`
              : 'no item has a measurable start and finish'
          }
        />
      </section>

      <Card>
        <CardHeader>
          <CardTitle>Burndown</CardTitle>
          <CardDescription>
            Points still open at the end of each day, against the straight line from the commitment
            to zero. Only days that have happened are plotted.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <BurndownChart burndown={burndown} />
          <details>
            <summary className="cursor-pointer text-sm text-muted-foreground">
              Burndown as a table
            </summary>
            <Table>
              <TableCaption>Every day of {burndown.sprint}, with the numbers plotted above.</TableCaption>
              <TableHeader>
                <TableRow>
                  <TableHead>Day</TableHead>
                  <TableHead>Date</TableHead>
                  <TableHead>Ideal</TableHead>
                  <TableHead>Remaining</TableHead>
                  <TableHead>Scope</TableHead>
                  <TableHead>Done</TableHead>
                  <TableHead>Unknown</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {burndown.points.map((point) => (
                  <TableRow key={point.date}>
                    <TableCell>{point.day}</TableCell>
                    <TableCell>{point.date}</TableCell>
                    <TableCell className="tabular-nums">{formatNumber(point.ideal)}</TableCell>
                    <TableCell className="tabular-nums">
                      {point.observed ? formatNumber(point.remaining) : 'not measured'}
                    </TableCell>
                    <TableCell className="tabular-nums">
                      {point.observed ? formatNumber(point.scope) : '—'}
                    </TableCell>
                    <TableCell className="tabular-nums">
                      {point.observed ? formatNumber(point.done) : '—'}
                    </TableCell>
                    <TableCell className="tabular-nums">{point.observed ? point.unknown : '—'}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </details>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Cumulative flow</CardTitle>
          <CardDescription>
            How many items stood in each status at the end of each day. Finished work stacks at the
            bottom, so the top edge of the shape is the scope.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <CumulativeFlowChart flow={flow} />
          <details>
            <summary className="cursor-pointer text-sm text-muted-foreground">
              Cumulative flow as a table
            </summary>
            <Table>
              <TableCaption>Item counts by status for every measured day.</TableCaption>
              <TableHeader>
                <TableRow>
                  <TableHead>Date</TableHead>
                  {bands.map((band) => (
                    <TableHead key={band}>{BAND_LABELS[band]}</TableHead>
                  ))}
                  <TableHead>Total</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {flow.days
                  .filter((day) => day.observed)
                  .map((day) => (
                    <TableRow key={day.date}>
                      <TableCell>{day.date}</TableCell>
                      {bands.map((band) => (
                        <TableCell key={band} className="tabular-nums">
                          {day.counts[band] ?? 0}
                        </TableCell>
                      ))}
                      <TableCell className="tabular-nums">{day.total}</TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          </details>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Flow</CardTitle>
          <CardDescription>
            Cycle time is the wait from an item first moving into progress to it first reaching a
            terminal status. Lead time starts at the item's creation instead, so it needs a complete
            history and is empty without one. Throughput counts the items that finished inside this
            sprint.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Measure</TableHead>
                <TableHead>Samples</TableHead>
                <TableHead>Median</TableHead>
                <TableHead>Mean</TableHead>
                <TableHead>85th pct</TableHead>
                <TableHead>Range</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <StatRow label="Cycle time (days)" stat={stats.cycleTime} />
              <StatRow label="Lead time (days)" stat={stats.leadTime} />
            </TableBody>
          </Table>
          {stats.excluded > 0 && (
            <p className="text-sm text-muted-foreground">
              {stats.excluded} finished item(s) were left out: their history does not reach back to
              the transition being measured.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

/** One headline number with the sentence that qualifies it. */
function StatTile({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <p className="text-sm text-muted-foreground">{label}</p>
      <p className="mt-1 text-2xl font-semibold text-card-foreground">{value}</p>
      <p className="mt-1 text-xs text-muted-foreground">{hint}</p>
    </div>
  );
}

/** One row of the flow table; a measure with no sample says so. */
function StatRow({ label, stat }: { label: string; stat: MetricStat }) {
  if (stat.count === 0) {
    return (
      <TableRow>
        <TableCell>{label}</TableCell>
        <TableCell colSpan={5} className="text-muted-foreground">
          No item in this sprint has a measurable one.
        </TableCell>
      </TableRow>
    );
  }
  return (
    <TableRow>
      <TableCell>{label}</TableCell>
      <TableCell className="tabular-nums">{stat.count}</TableCell>
      <TableCell className="tabular-nums">{formatNumber(stat.median)}</TableCell>
      <TableCell className="tabular-nums">{formatNumber(stat.mean)}</TableCell>
      <TableCell className="tabular-nums">{formatNumber(stat.p85)}</TableCell>
      <TableCell className="tabular-nums">
        {formatNumber(stat.min)}–{formatNumber(stat.max)}
      </TableCell>
    </TableRow>
  );
}

/**
 * Where the numbers came from. It is the first thing on the page and it is
 * never hidden: a chart whose provenance is unstated is a chart nobody can act
 * on (docs/04 §12).
 */
export function ProvenanceBanner({ provenance }: { provenance: MetricsProvenance }) {
  const approximate = provenance.approximate;
  const Icon = approximate ? TriangleAlert : Info;
  const partial = provenance.covered < provenance.items;
  return (
    <div
      role="note"
      className={
        approximate
          ? 'flex gap-3 rounded-lg border border-border bg-muted p-4 text-sm'
          : 'flex gap-3 rounded-lg border border-border bg-card p-4 text-sm'
      }
    >
      <Icon aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="space-y-1">
        <p className="flex flex-wrap items-center gap-2 font-medium text-foreground">
          {approximate ? 'These numbers are an approximation' : 'Reconstructed from git history'}
          <Badge variant="outline">{provenance.source}</Badge>
        </p>
        <p className="text-muted-foreground">{provenance.note}</p>
        {partial && (
          <p className="text-muted-foreground">
            {provenance.covered} of {provenance.items} references have a readable history; the rest
            are reported as unknown on every day rather than counted as work.
          </p>
        )}
      </div>
    </div>
  );
}

/**
 * The metrics index: which sprint to look at. It exists so that the charts are
 * reachable from the navigation and not only from a board.
 */
export function MetricsIndex() {
  const sprints = useSprints();
  const rows = [...(sprints.data ?? [])].reverse();

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Metrics</h1>
        <p className="text-sm text-muted-foreground">
          Burndown and cumulative flow, computed from what is already in git. Pick a sprint.
        </p>
      </header>
      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No sprint is open in this workspace, so there is nothing to measure yet.
        </p>
      ) : (
        <ul className="space-y-2">
          {rows.map((sprint) => (
            <li key={sprint.id}>
              <Link
                to="/metrics/$sprintId"
                params={{ sprintId: sprint.id }}
                className="flex items-center justify-between rounded-lg border border-border bg-card p-4 hover:border-ring"
              >
                <span>
                  <span className="font-medium">{sprint.title}</span>
                  <span className="block text-xs text-muted-foreground">
                    {sprint.start} to {sprint.end} · {sprint.state}
                  </span>
                </span>
                <Badge variant="outline">{formatNumber(sprint.metrics.points)} pts</Badge>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
