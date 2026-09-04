/**
 * The small amount of geometry the two sprint charts share.
 *
 * There is no charting library here on purpose: both charts are a handful of
 * polylines over a linear scale, which is less code than the adapter a library
 * would need, and it keeps the marks under the design system's own tokens
 * rather than a second palette (docs/05-web-app.md §16).
 */

import type { CumulativeFlow, FlowBand } from '@/api/provider';

/** The plot box of a chart, in the SVG's own user units. */
export const CHART = {
  width: 720,
  height: 260,
  left: 44,
  right: 16,
  top: 16,
  bottom: 32,
} as const;

/** The horizontal extent of the plot area. */
export const plotWidth = CHART.width - CHART.left - CHART.right;
/** The vertical extent of the plot area. */
export const plotHeight = CHART.height - CHART.top - CHART.bottom;

/** Maps a 0-based index over `count` slots onto an x coordinate. */
export function xAt(index: number, count: number): number {
  if (count <= 1) return CHART.left + plotWidth / 2;
  return CHART.left + (index / (count - 1)) * plotWidth;
}

/** Maps a value onto a y coordinate, with 0 at the baseline. */
export function yAt(value: number, max: number): number {
  if (max <= 0) return CHART.top + plotHeight;
  const clamped = Math.max(0, Math.min(value, max));
  return CHART.top + plotHeight - (clamped / max) * plotHeight;
}

/** Rounds an axis maximum up to a clean number, so the ticks read well. */
export function niceMax(value: number): number {
  if (value <= 0) return 1;
  const magnitude = 10 ** Math.floor(Math.log10(value));
  for (const step of [1, 2, 2.5, 5, 10]) {
    const candidate = step * magnitude;
    if (candidate >= value) return candidate;
  }
  return 10 * magnitude;
}

/** Four evenly spaced tick values from 0 to max. */
export function ticksOf(max: number): number[] {
  return [0, 0.25, 0.5, 0.75, 1].map((fraction) => Math.round(max * fraction * 100) / 100);
}

/** Builds an SVG path out of already-projected points. */
export function linePath(points: Array<{ x: number; y: number }>): string {
  return points.map((point, i) => `${i === 0 ? 'M' : 'L'}${point.x} ${point.y}`).join(' ');
}

/** How a date is written on an axis and in a table: `24 Aug`. */
export function shortDate(iso: string): string {
  const parsed = new Date(`${iso}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) return iso;
  return parsed.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', timeZone: 'UTC' });
}

/** Points and durations are shown to at most one decimal. */
export function formatNumber(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

/** The human name of a cumulative-flow band. */
export const BAND_LABELS: Record<FlowBand, string> = {
  done: 'Done',
  cancelled: 'Cancelled',
  in_progress: 'In progress',
  todo: 'To do',
  unknown: 'Unknown',
};

/**
 * The token each band is drawn in. Cancelled and unknown are neutral by design:
 * they are context rather than series, and `unknown` additionally carries the
 * hatch pattern so it never depends on colour alone.
 */
export const BAND_FILLS: Record<FlowBand, string> = {
  done: 'hsl(var(--chart-done))',
  cancelled: 'hsl(var(--chart-cancelled))',
  in_progress: 'hsl(var(--chart-progress))',
  todo: 'hsl(var(--chart-todo))',
  unknown: 'hsl(var(--chart-unknown))',
};

/** The bands a chart actually drew, for the data table's columns. */
export function bandsInUse(flow: CumulativeFlow): FlowBand[] {
  return flow.bands.filter((band) => flow.days.some((day) => (day.counts[band] ?? 0) > 0));
}
