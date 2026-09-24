import { Link } from '@tanstack/react-router';
import { FileCode2, FlaskConical, Link2, TriangleAlert } from 'lucide-react';
import type { ReactNode } from 'react';

import type { CoverageRow, TraceEdge, TracedRequirement } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { bareItemId } from '@/features/backlog/item-meta';
import {
  formatLines,
  groupByOrigin,
  traceRefOf,
  type TraceOrigin,
} from '@/features/specs/trace-groups';

type TestResult = NonNullable<CoverageRow['tests']>[number]['result'];

const originLabel: Record<TraceOrigin, string> = {
  both: 'Marker and trace:',
  marker: 'Marker',
  trace: 'trace: entry',
};

const originHint: Record<TraceOrigin, string> = {
  both: 'Declared by an in-code marker and by a trace: entry in the spec.',
  marker: 'Declared by an in-code marker (Implements: / Verifies:).',
  trace: 'Declared by a trace: entry in the spec’s requirements map.',
};

const resultVariant: Record<TestResult, BadgeProps['variant']> = {
  pass: 'success',
  fail: 'destructive',
  skip: 'default',
  missing: 'outline',
};

/**
 * The trace panel of a requirement (story GIT-US-0129): the code and tests it
 * is tied to, grouped by origin (marker, `trace:` entry or both), each as
 * `path#symbol` with the marker lines; the tests with their last local result;
 * and the stories and tasks that `implements` or `modifies` it — the computed
 * inverses of those one-sided links (R-LINK-8). In browser-only mode the
 * provider answers `unavailable` and the panel says so.
 */
export function TracePanel({
  projectKey,
  trace,
  error,
  loading,
  results,
}: {
  projectKey: string;
  trace: TracedRequirement | undefined;
  error: Error | null;
  loading: boolean;
  /** Last local result per test id, from the coverage row; empty when coverage is unavailable. */
  results: ReadonlyMap<string, TestResult>;
}) {
  return (
    <Card aria-labelledby="trace-panel-title">
      <CardHeader className="p-4 pb-2">
        <CardTitle id="trace-panel-title" className="text-base">
          Trace
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4 p-4 pt-0 text-sm">
        {error ? (
          <TraceUnavailable error={error} />
        ) : loading || !trace ? (
          <p className="text-muted-foreground">Loading the trace…</p>
        ) : (
          <>
            <EdgeSection
              title="Code"
              icon={<FileCode2 aria-hidden="true" className="h-4 w-4" />}
              edges={trace.code}
              empty="No code is traced to this requirement."
            />
            <EdgeSection
              title="Tests"
              icon={<FlaskConical aria-hidden="true" className="h-4 w-4" />}
              edges={trace.tests}
              empty="No test verifies this requirement."
              results={results}
            />
            <WorkSection projectKey={projectKey} work={trace.work} />
            {trace.broken?.length ? (
              <section aria-label="Broken trace entries" className="space-y-1.5">
                <h3 className="flex items-center gap-1.5 text-sm font-medium text-warning">
                  <TriangleAlert aria-hidden="true" className="h-4 w-4" />
                  Broken trace: entries
                </h3>
                <ul className="space-y-1">
                  {trace.broken.map((entry) => (
                    <li key={`${entry.field}:${entry.entry}`} className="text-xs">
                      <code className="font-mono">{entry.entry}</code>{' '}
                      <span className="text-muted-foreground">
                        ({entry.field}) — {entry.message}
                      </span>
                    </li>
                  ))}
                </ul>
              </section>
            ) : null}
          </>
        )}
      </CardContent>
    </Card>
  );
}

function TraceUnavailable({ error }: { error: Error }) {
  const unavailable = error instanceof ProviderError && error.code === 'unavailable';
  return (
    <p
      role="status"
      data-trace="unavailable"
      className="rounded-md border border-dashed border-border-strong px-3 py-2 text-muted-foreground"
    >
      <strong className="font-medium text-foreground">
        {unavailable ? 'Trace unavailable.' : 'The trace could not be read.'}
      </strong>{' '}
      {error.message}
    </p>
  );
}

function EdgeSection({
  title,
  icon,
  edges,
  empty,
  results,
}: {
  title: string;
  icon: ReactNode;
  edges: readonly TraceEdge[];
  empty: string;
  results?: ReadonlyMap<string, TestResult>;
}) {
  const groups = groupByOrigin(edges);
  return (
    <section aria-label={title} className="space-y-2">
      <h3 className="flex items-center gap-1.5 text-sm font-medium">
        {icon}
        {title}
        <span className="text-xs font-normal text-muted-foreground">{edges.length}</span>
      </h3>
      {groups.length === 0 ? (
        <p className="text-xs text-muted-foreground">{empty}</p>
      ) : (
        groups.map((group) => (
          <div key={group.origin} className="space-y-1" data-origin={group.origin}>
            <p
              className="text-2xs font-medium uppercase tracking-wide text-muted-foreground"
              title={originHint[group.origin]}
            >
              {originLabel[group.origin]}
            </p>
            <ul aria-label={`${title} by ${originLabel[group.origin]}`} className="space-y-1">
              {group.edges.map((edge) => {
                const ref = traceRefOf(edge);
                const lines = formatLines(edge.lines);
                const result = results?.get(ref);
                return (
                  <li
                    key={ref}
                    data-trace-ref={ref}
                    className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5"
                  >
                    <code className="min-w-0 break-all font-mono text-xs">
                      {edge.path}
                      {edge.symbol ? (
                        <span className="text-accent">#{edge.symbol}</span>
                      ) : (
                        <span className="text-muted-foreground"> (file)</span>
                      )}
                    </code>
                    {lines ? (
                      <span className="text-2xs tabular-nums text-muted-foreground">{lines}</span>
                    ) : null}
                    {results ? (
                      <Badge
                        size="sm"
                        variant={resultVariant[result ?? 'missing']}
                        data-result={result ?? 'missing'}
                        title="Last local result"
                      >
                        {result === undefined || result === 'missing' ? 'no result' : result}
                      </Badge>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </div>
        ))
      )}
    </section>
  );
}

function WorkSection({
  projectKey,
  work,
}: {
  projectKey: string;
  work: TracedRequirement['work'];
}) {
  const sorted = [...work].sort((a, b) => a.kind.localeCompare(b.kind) || a.id.localeCompare(b.id));
  return (
    <section aria-label="Work" className="space-y-2">
      <h3 className="flex items-center gap-1.5 text-sm font-medium">
        <Link2 aria-hidden="true" className="h-4 w-4" />
        Stories and tasks
        <span className="text-xs font-normal text-muted-foreground">{work.length}</span>
      </h3>
      {sorted.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          No story or task implements or modifies this requirement.
        </p>
      ) : (
        <ul className="space-y-1">
          {sorted.map((entry) => (
            <li
              key={`${entry.kind}:${entry.id}`}
              data-work={entry.id}
              className="flex flex-wrap items-center gap-2"
            >
              <Badge size="sm" variant={entry.kind === 'implements' ? 'info' : 'accent'}>
                {entry.kind === 'implements' ? 'implemented by' : 'modified by'}
              </Badge>
              <Link
                to="/p/$project/items/$id"
                params={{
                  project: entry.id.includes('/')
                    ? entry.id.split('/')[0] || projectKey
                    : projectKey,
                  id: bareItemId(entry.id),
                }}
                className="font-mono text-xs text-accent underline-offset-4 hover:underline"
              >
                {entry.id}
              </Link>
              {entry.wholeSpec ? (
                <span className="text-2xs text-muted-foreground">(links the whole spec)</span>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
