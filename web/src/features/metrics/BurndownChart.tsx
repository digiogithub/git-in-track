import { useState } from 'react';

import type { Burndown } from '@/api/provider';
import {
  CHART,
  formatNumber,
  linePath,
  niceMax,
  plotHeight,
  shortDate,
  ticksOf,
  xAt,
  yAt,
} from '@/features/metrics/chart';

/**
 * Sprint burndown: the points still open at the end of each day, against the
 * straight line from the commitment to zero (docs/04 §12).
 *
 * Only observed days are plotted. A future day has no measurement, and a day
 * the history cannot speak for is marked rather than drawn through: the whole
 * point of this chart is that the numbers are real.
 */
export function BurndownChart({ burndown }: { burndown: Burndown }) {
  const [active, setActive] = useState<number | null>(null);

  const points = burndown.points;
  const observed = points.filter((point) => point.observed);
  const scopeMoved = observed.some((point) => point.scope !== observed[0]?.scope);
  const max = niceMax(
    Math.max(burndown.committedPoints, ...observed.map((point) => Math.max(point.scope, point.remaining)), 1),
  );
  const ticks = ticksOf(max);

  const project = (value: number, index: number) => ({
    x: xAt(index, points.length),
    y: yAt(value, max),
  });
  const idealLine = linePath(points.map((point, i) => project(point.ideal, i)));
  const remainingLine = linePath(
    points.map((point, i) => (point.observed ? project(point.remaining, i) : null)).filter(isPoint),
  );
  const scopeLine = linePath(
    points.map((point, i) => (point.observed ? project(point.scope, i) : null)).filter(isPoint),
  );
  const last = observed.at(-1);
  const lastIndex = last ? points.indexOf(last) : -1;
  const hovered = active === null ? null : points[active];

  if (points.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        This sprint has no dates, so it has no days to plot.
      </p>
    );
  }

  return (
    <figure className="m-0 space-y-3">
      <Legend
        entries={[
          { label: 'Remaining', color: 'hsl(var(--chart-todo))' },
          ...(scopeMoved ? [{ label: 'Scope', color: 'hsl(var(--chart-progress))' }] : []),
          { label: 'Ideal', color: 'hsl(var(--chart-ideal))', dashed: true },
        ]}
      />
      <div className="relative overflow-x-auto">
        <svg
          viewBox={`0 0 ${CHART.width} ${CHART.height}`}
          className="h-auto w-full min-w-[520px]"
          role="img"
          aria-label={`Burndown of ${burndown.sprint}. The table below carries every value.`}
          onMouseLeave={() => setActive(null)}
        >
          {ticks.map((tick) => (
            <g key={tick}>
              <line
                x1={CHART.left}
                x2={CHART.width - CHART.right}
                y1={yAt(tick, max)}
                y2={yAt(tick, max)}
                style={{ stroke: 'hsl(var(--chart-grid))' }}
                strokeWidth={1}
              />
              <text
                x={CHART.left - 8}
                y={yAt(tick, max) + 4}
                textAnchor="end"
                className="fill-muted-foreground text-[11px] tabular-nums"
              >
                {formatNumber(tick)}
              </text>
            </g>
          ))}

          <path
            d={idealLine}
            fill="none"
            style={{ stroke: 'hsl(var(--chart-ideal))' }}
            strokeWidth={2}
            strokeDasharray="6 4"
            strokeLinecap="round"
          />
          {scopeMoved && (
            <path
              d={scopeLine}
              fill="none"
              style={{ stroke: 'hsl(var(--chart-progress))' }}
              strokeWidth={2}
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          )}
          <path
            d={remainingLine}
            fill="none"
            style={{ stroke: 'hsl(var(--chart-todo))' }}
            strokeWidth={2}
            strokeLinejoin="round"
            strokeLinecap="round"
          />

          {/* A day the history cannot fully account for is marked, never smoothed over. */}
          {points.map((point, i) =>
            point.observed && point.unknown > 0 ? (
              <circle
                key={`unknown-${point.date}`}
                cx={xAt(i, points.length)}
                cy={yAt(point.remaining, max)}
                r={5}
                style={{ fill: 'hsl(var(--chart-unknown))', stroke: 'hsl(var(--card))' }}
                strokeWidth={2}
              />
            ) : null,
          )}

          {last && lastIndex >= 0 && (
            <>
              <circle
                cx={xAt(lastIndex, points.length)}
                cy={yAt(last.remaining, max)}
                r={4}
                style={{ fill: 'hsl(var(--chart-todo))', stroke: 'hsl(var(--card))' }}
                strokeWidth={2}
              />
              <text
                x={xAt(lastIndex, points.length) - 8}
                y={yAt(last.remaining, max) - 10}
                textAnchor="end"
                className="fill-foreground text-[11px] font-medium tabular-nums"
              >
                {formatNumber(last.remaining)}
              </text>
            </>
          )}

          {points.map((point, i) => (
            <g key={point.date}>
              <rect
                x={xAt(i, points.length) - 12}
                y={CHART.top}
                width={24}
                height={plotHeight}
                fill="transparent"
                onMouseEnter={() => setActive(i)}
              />
              {i % Math.ceil(points.length / 7) === 0 && (
                <text
                  x={xAt(i, points.length)}
                  y={CHART.height - 10}
                  textAnchor="middle"
                  className="fill-muted-foreground text-[11px]"
                >
                  {shortDate(point.date)}
                </text>
              )}
            </g>
          ))}
          {active !== null && (
            <line
              x1={xAt(active, points.length)}
              x2={xAt(active, points.length)}
              y1={CHART.top}
              y2={CHART.top + plotHeight}
              style={{ stroke: 'hsl(var(--chart-grid))' }}
              strokeWidth={1}
            />
          )}
        </svg>
        {hovered && (
          <div
            role="status"
            className="pointer-events-none absolute left-2 top-2 rounded-md border border-border bg-popover px-3 py-2 text-xs shadow-sm"
          >
            <p className="font-medium text-popover-foreground">{shortDate(hovered.date)}</p>
            {hovered.observed ? (
              <p className="text-muted-foreground">
                {formatNumber(hovered.remaining)} of {formatNumber(hovered.scope)} points remaining
                {hovered.unknown > 0 ? `, ${hovered.unknown} unknown` : ''}
              </p>
            ) : (
              <p className="text-muted-foreground">Not measured yet</p>
            )}
          </div>
        )}
      </div>
    </figure>
  );
}

/** A point that survived the observed filter. */
function isPoint(value: { x: number; y: number } | null): value is { x: number; y: number } {
  return value !== null;
}

/** The legend both charts share: a swatch beside text in an ink token. */
export function Legend({
  entries,
}: {
  entries: Array<{ label: string; color: string; dashed?: boolean; hatched?: boolean }>;
}) {
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {entries.map((entry) => (
        <li key={entry.label} className="flex items-center gap-1.5">
          <span
            aria-hidden="true"
            className="inline-block h-2.5 w-4 rounded-sm"
            style={{
              background: entry.hatched
                ? `repeating-linear-gradient(45deg, ${entry.color} 0 2px, transparent 2px 4px)`
                : entry.dashed
                  ? `repeating-linear-gradient(90deg, ${entry.color} 0 4px, transparent 4px 7px)`
                  : entry.color,
            }}
          />
          {entry.label}
        </li>
      ))}
    </ul>
  );
}
