/**
 * Pure helpers for the requirement trace panel (story GIT-US-0129).
 *
 * A trace edge ties a requirement to code or tests through in-code markers,
 * `trace:` entries of the `requirements:` map, or both (doc 03 §21.7). The
 * panel groups the edges of each role by that origin, because the two are
 * fixed in different places: a marker in the source file, a `trace:` entry in
 * the spec.
 */

import type { TraceEdge } from '@/api/provider';

export type TraceOrigin = 'marker' | 'trace' | 'both';

/** Display order: the edges backed by both first, then markers, then `trace:` entries. */
export const traceOrigins: readonly TraceOrigin[] = ['both', 'marker', 'trace'];

export type TraceGroup = { origin: TraceOrigin; edges: TraceEdge[] };

/** Which side an edge comes from; an edge with no source reads as `trace`. */
export function originOf(edge: TraceEdge): TraceOrigin {
  const marker = edge.sources.includes('marker');
  const trace = edge.sources.includes('trace');
  if (marker && trace) return 'both';
  return marker ? 'marker' : 'trace';
}

/** The trace ref of an edge, `<path>[#<symbol>]`: also the test id coverage reports. */
export function traceRefOf(edge: Pick<TraceEdge, 'path' | 'symbol'>): string {
  return edge.symbol ? `${edge.path}#${edge.symbol}` : edge.path;
}

/** The edges grouped by origin in `traceOrigins` order, each sorted by trace ref; empty groups are left out. */
export function groupByOrigin(edges: readonly TraceEdge[]): TraceGroup[] {
  return traceOrigins
    .map((origin) => ({
      origin,
      edges: edges
        .filter((edge) => originOf(edge) === origin)
        .sort((a, b) => traceRefOf(a).localeCompare(traceRefOf(b))),
    }))
    .filter((group) => group.edges.length > 0);
}

/** `L12, L40` for the marker lines of an edge; empty without any. */
export function formatLines(lines: readonly number[] | undefined): string {
  if (!lines?.length) return '';
  return [...lines]
    .sort((a, b) => a - b)
    .map((line) => `L${line}`)
    .join(', ');
}

/**
 * The route segments of a requirement detail: `/specs/<SPEC-ID>/R<n>`. The
 * `$req` segment is the requirement's local number, so the URL reads like the
 * ref it names.
 */
export function requirementRouteParams(ref: string): { spec: string; req: string } {
  const bare = ref.slice(ref.lastIndexOf('/') + 1);
  const dot = bare.lastIndexOf('.');
  if (dot < 0) return { spec: bare, req: '' };
  return { spec: bare.slice(0, dot), req: bare.slice(dot + 1) };
}

/** The ref a detail route names; a `$req` that is already a full ref is taken as is. */
export function refFromRouteParams(spec: string, req: string): string {
  return req.includes('.') ? req : `${spec}.${req}`;
}
