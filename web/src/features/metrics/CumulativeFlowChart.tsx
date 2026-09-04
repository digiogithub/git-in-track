import { useState } from 'react';

import type { CumulativeFlow } from '@/api/provider';
import { Legend } from '@/features/metrics/BurndownChart';
import {
  BAND_FILLS,
  BAND_LABELS,
  CHART,
  niceMax,
  plotHeight,
  shortDate,
  ticksOf,
  xAt,
  yAt,
} from '@/features/metrics/chart';

/**
 * Cumulative flow: how many items stood in each status band at the end of each
 * day (docs/04 §12).
 *
 * The bands stack bottom first with the finished work at the bottom, so the top
 * edge of the shape is the scope and scope growth is visible as the whole
 * shape rising. `Unknown` is hatched as well as neutral, because "we cannot
 * tell" must never look like a status.
 */
export function CumulativeFlowChart({ flow }: { flow: CumulativeFlow }) {
  const [active, setActive] = useState<number | null>(null);

  const days = flow.days.filter((day) => day.observed);
  const max = niceMax(Math.max(1, ...days.map((day) => day.total)));
  const present = flow.bands.filter((band) => days.some((day) => (day.counts[band] ?? 0) > 0));
  const hovered = active === null ? null : days[active];

  if (days.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No day of this sprint has been measured yet, so there is nothing to plot.
      </p>
    );
  }

  // Each band is an area between the running total below it and above it. The
  // 2px surface gap between bands is what separates them; no strokes are drawn
  // around the fills.
  const bases = days.map(() => 0);
  const areas = flow.bands.map((band) => {
    const upper = days.map((day, i) => {
      const value = bases[i] ?? 0;
      const next = value + (day.counts[band] ?? 0);
      bases[i] = next;
      return next;
    });
    const lower = days.map((_, i) => (upper[i] ?? 0) - (days[i]?.counts[band] ?? 0));
    const top = upper.map((value, i) => `${i === 0 ? 'M' : 'L'}${xAt(i, days.length)} ${yAt(value, max)}`);
    const bottom = lower
      .map((value, i) => ({ value, i }))
      .reverse()
      .map(({ value, i }) => `L${xAt(i, days.length)} ${yAt(value, max)}`);
    return { band, path: [...top, ...bottom, 'Z'].join(' ') };
  });

  return (
    <figure className="m-0 space-y-3">
      <Legend
        entries={present.map((band) => ({
          label: BAND_LABELS[band],
          color: BAND_FILLS[band],
          hatched: band === 'unknown',
        }))}
      />
      <div className="relative overflow-x-auto">
        <svg
          viewBox={`0 0 ${CHART.width} ${CHART.height}`}
          className="h-auto w-full min-w-[520px]"
          role="img"
          aria-label="Cumulative flow: item counts by status over the sprint. The table below carries every value."
          onMouseLeave={() => setActive(null)}
        >
          <defs>
            <pattern id="cfd-unknown" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
              <rect width="6" height="6" style={{ fill: 'hsl(var(--chart-unknown))' }} />
              <line x1="0" y1="0" x2="0" y2="6" style={{ stroke: 'hsl(var(--card))' }} strokeWidth={2} />
            </pattern>
          </defs>

          {ticksOf(max).map((tick) => (
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
                {Math.round(tick)}
              </text>
            </g>
          ))}

          {areas.map(({ band, path }) => (
            <path
              key={band}
              d={path}
              style={{
                fill: band === 'unknown' ? 'url(#cfd-unknown)' : BAND_FILLS[band],
                stroke: 'hsl(var(--card))',
              }}
              strokeWidth={2}
            />
          ))}

          {days.map((day, i) => (
            <g key={day.date}>
              <rect
                x={xAt(i, days.length) - 12}
                y={CHART.top}
                width={24}
                height={plotHeight}
                fill="transparent"
                onMouseEnter={() => setActive(i)}
              />
              {i % Math.ceil(days.length / 7) === 0 && (
                <text
                  x={xAt(i, days.length)}
                  y={CHART.height - 10}
                  textAnchor="middle"
                  className="fill-muted-foreground text-[11px]"
                >
                  {shortDate(day.date)}
                </text>
              )}
            </g>
          ))}
          {active !== null && (
            <line
              x1={xAt(active, days.length)}
              x2={xAt(active, days.length)}
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
            <ul className="text-muted-foreground">
              {present.map((band) => (
                <li key={band}>
                  {BAND_LABELS[band]}: {hovered.counts[band] ?? 0}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </figure>
  );
}
