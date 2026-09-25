import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { ArrowLeft, CircleCheck, CircleMinus, CircleX, ExternalLink } from 'lucide-react';
import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type UIEvent,
} from 'react';

import { ProviderError } from '@/api/provider';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { bareItemId } from '@/features/backlog/item-meta';
import { useBacklogEvents } from '@/features/backlog/queries';
import { CoverageBadge } from '@/features/specs/CoverageBadge';
import {
  buildMatrix,
  countStatuses,
  filterRows,
  joinRows,
  parseMatrixSearch,
  summarizeSpecs,
  visibleRange,
  type Matrix,
  type MatrixRow,
  type MatrixSearch,
  type MatrixSearchInput,
  type SpecSummary,
  type StatusCounts,
  type TestResult,
} from '@/features/specs/matrix';
import { useCoverage, useRequirements, useSpecs } from '@/features/specs/queries';
import { coverageStates, listParam } from '@/features/specs/search';
import { requirementRouteParams } from '@/features/specs/trace-groups';
import { cn } from '@/lib/cn';
import { requirementAnchor } from '@/markdown';

/** Every body row has this height, so the window is plain arithmetic. */
export const MATRIX_ROW_HEIGHT = 64;
/** Rows rendered beyond each edge of the viewport. */
export const MATRIX_OVERSCAN = 8;
/** The viewport assumed before the container has been measured (and in tests). */
const FALLBACK_VIEWPORT = 480;
/** Two `h-8` header rows. */
const HEADER_HEIGHT = 64;

type SearchRecord = Record<string, unknown>;
type NavigateWithSearch = (options: {
  search: (prev: SearchRecord) => SearchRecord;
  replace?: boolean;
}) => void;

/** Merges a patch into the URL filter; `undefined` clears a param. */
function useSetMatrixSearch(): (patch: MatrixSearchInput) => void {
  const navigate = useNavigate() as unknown as NavigateWithSearch;
  return useCallback(
    (patch: MatrixSearchInput) => {
      navigate({
        search: (prev) => {
          const next: SearchRecord = {};
          for (const [key, value] of Object.entries({ ...prev, ...patch })) {
            if (value !== undefined && value !== '') next[key] = value;
          }
          return next;
        },
        replace: true,
      });
    },
    [navigate],
  );
}

function toggle(list: readonly string[] | undefined, value: string): string[] {
  const current = list ?? [];
  return current.includes(value) ? current.filter((v) => v !== value) : [...current, value];
}

const chipClass =
  'inline-flex h-7 items-center gap-1 rounded-full border px-2.5 text-xs font-medium transition-colors duration-fast';
const chipOff = 'border-input text-muted-foreground hover:bg-secondary hover:text-foreground';
const chipOn = 'border-transparent bg-primary text-primary-foreground';

/**
 * The requirement coverage matrix (`/p/$project/specs/coverage`, story
 * GIT-US-0130): requirements as rows, linked tests as columns grouped by file,
 * each cell the test's last local result and each row the computed coverage
 * state of doc 03 §21.6. Coverage is a companion answer; in browser-only mode
 * the page renders `unavailable` with a hint to run `gintrack serve`.
 */
export function CoverageMatrixPage() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const raw = useSearch({ strict: false });
  const search = useMemo(() => parseMatrixSearch(raw), [raw]);
  const setSearch = useSetMatrixSearch();

  useBacklogEvents(projectKey);

  const specsQuery = useSpecs(projectKey);
  const requirementsQuery = useRequirements(projectKey);
  const coverageQuery = useCoverage(projectKey);

  const allRows = useMemo(
    () => joinRows(requirementsQuery.data?.requirements ?? [], coverageQuery.data?.coverage ?? []),
    [requirementsQuery.data, coverageQuery.data],
  );
  const matrix = useMemo(() => buildMatrix(allRows, search), [allRows, search]);
  const statusCounts = useMemo(
    () => countStatuses(filterRows(allRows, { ...(search.spec ? { spec: search.spec } : {}) })),
    [allRows, search.spec],
  );
  const summaries = useMemo(
    () => summarizeSpecs(specsQuery.data ?? [], allRows),
    [specsQuery.data, allRows],
  );

  const unavailable =
    coverageQuery.error instanceof ProviderError && coverageQuery.error.code === 'unavailable'
      ? coverageQuery.error
      : undefined;
  const failed = unavailable ? undefined : (coverageQuery.error ?? requirementsQuery.error);
  const loading = coverageQuery.isPending || requirementsQuery.isPending;
  const filtered = Boolean(search.spec?.length) || Boolean(search.status?.length);

  return (
    <div className="min-w-0 space-y-4">
      <header className="space-y-1">
        <Link
          to="/p/$project/specs"
          params={{ project: projectKey }}
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft aria-hidden="true" className="h-3 w-3" />
          Specs
        </Link>
        <h1 className="page-title">Coverage matrix</h1>
        <p className="text-sm text-muted-foreground">
          Requirements of <strong>{projectKey}</strong> against their linked tests, with the last
          local result of each.
        </p>
      </header>

      {unavailable ? (
        <Card role="status">
          <CardHeader>
            <CardTitle>Coverage unavailable</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm text-muted-foreground">
            <p>{unavailable.message}</p>
            <p>
              The matrix needs the test results and git history only the companion reads: run{' '}
              <code className="font-mono">gintrack serve</code> in the repository and open the app
              it serves.
            </p>
          </CardContent>
        </Card>
      ) : null}

      {failed ? (
        <Card>
          <CardHeader>
            <CardTitle>Coverage could not be read</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">{failed.message}</CardContent>
        </Card>
      ) : null}

      {!unavailable && !failed && loading ? (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading coverage…</p>
      ) : null}

      {!unavailable && !failed && !loading ? (
        <>
          <MatrixFilters
            search={search}
            counts={statusCounts}
            specs={summaries}
            onChange={setSearch}
          />
          <SpecSummaryTable summaries={summaries} />
          {matrix.rows.length === 0 ? (
            <Card>
              <CardHeader>
                <CardTitle>{filtered ? 'No requirement matches' : 'No requirements yet'}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                {filtered ? (
                  <>
                    <p>No requirement is in the specs and states picked above.</p>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setSearch({ spec: undefined, status: undefined })}
                    >
                      Clear filters
                    </Button>
                  </>
                ) : (
                  <p>Add requirements to a spec on the Specs page to see their coverage here.</p>
                )}
              </CardContent>
            </Card>
          ) : (
            <MatrixGrid matrix={matrix} projectKey={projectKey} />
          )}
        </>
      ) : null}
    </div>
  );
}

function MatrixFilters({
  search,
  counts,
  specs,
  onChange,
}: {
  search: MatrixSearch;
  counts: StatusCounts;
  specs: SpecSummary[];
  onChange: (patch: MatrixSearchInput) => void;
}) {
  return (
    <div className="space-y-2">
      <div
        role="group"
        aria-label="Filter by status"
        className="flex flex-wrap items-center gap-1.5"
      >
        <span className="mr-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Status
        </span>
        {coverageStates.map((state) => {
          const on = search.status?.includes(state) ?? false;
          return (
            <button
              key={state}
              type="button"
              aria-pressed={on}
              aria-label={`${state} (${counts[state]})`}
              className={cn(chipClass, on ? chipOn : chipOff)}
              onClick={() => onChange({ status: listParam(toggle(search.status, state)) })}
            >
              {state}
              <span className="tabular-nums opacity-80">{counts[state]}</span>
            </button>
          );
        })}
      </div>
      {specs.length > 0 ? (
        <div
          role="group"
          aria-label="Filter by spec"
          className="flex flex-wrap items-center gap-1.5"
        >
          <span className="mr-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Spec
          </span>
          {specs.map((spec) => {
            const on = search.spec?.includes(spec.id) ?? false;
            return (
              <button
                key={spec.id}
                type="button"
                aria-pressed={on}
                title={spec.title}
                className={cn(chipClass, 'font-mono', on ? chipOn : chipOff)}
                onClick={() => onChange({ spec: listParam(toggle(search.spec, spec.id)) })}
              >
                {spec.id}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

/** Counts per spec and state, over every requirement (the filters do not apply). */
function SpecSummaryTable({ summaries }: { summaries: SpecSummary[] }) {
  if (summaries.length === 0) return null;
  return (
    <div className="w-0 min-w-full overflow-x-auto rounded-md border border-border">
      <table className="w-full text-sm">
        <caption className="sr-only">Coverage by spec</caption>
        <thead className="bg-muted/50 text-xs text-muted-foreground">
          <tr>
            <th scope="col" className="h-8 px-3 text-left font-medium">
              Spec
            </th>
            <th scope="col" className="h-8 px-3 text-right font-medium">
              Requirements
            </th>
            {coverageStates.map((state) => (
              <th key={state} scope="col" className="h-8 px-3 text-right font-medium">
                {state}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {summaries.map((summary) => (
            <tr key={summary.id} data-spec={summary.id}>
              <th scope="row" className="px-3 py-1.5 text-left font-normal">
                <span className="font-mono text-xs">{summary.id}</span>{' '}
                <span className="text-muted-foreground">{summary.title}</span>
              </th>
              <td className="px-3 py-1.5 text-right tabular-nums">{summary.total}</td>
              {coverageStates.map((state) => (
                <td key={state} className="px-3 py-1.5 text-right tabular-nums">
                  {summary.counts[state]}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Icon, word and tone per result: the word is always there, colour only reinforces it. */
const resultLooks: Record<
  Exclude<TestResult, 'missing'>,
  { icon: ReactNode; className: string }
> = {
  pass: {
    icon: <CircleCheck aria-hidden="true" className="h-3.5 w-3.5" />,
    className: 'text-success',
  },
  fail: {
    icon: <CircleX aria-hidden="true" className="h-3.5 w-3.5" />,
    className: 'text-destructive',
  },
  skip: {
    icon: <CircleMinus aria-hidden="true" className="h-3.5 w-3.5" />,
    className: 'text-muted-foreground',
  },
};

function ResultCell({ result }: { result: TestResult | undefined }) {
  if (result === undefined) return null;
  if (result === 'missing') {
    return (
      <span data-result="missing" title="No result yet" className="text-muted-foreground">
        <span aria-hidden="true">—</span>
        <span className="sr-only">no result</span>
      </span>
    );
  }
  const look = resultLooks[result];
  return (
    <span
      data-result={result}
      className={cn('inline-flex items-center gap-1 text-xs font-medium', look.className)}
    >
      {look.icon}
      {result}
    </span>
  );
}

/** Tracks the scroll position and height of the matrix viewport. */
function useViewport() {
  const ref = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [height, setHeight] = useState(FALLBACK_VIEWPORT);

  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    const measure = () => setHeight(element.clientHeight || FALLBACK_VIEWPORT);
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const onScroll = useCallback((event: UIEvent<HTMLDivElement>) => {
    setScrollTop(event.currentTarget.scrollTop);
  }, []);

  return { ref, scrollTop, height, onScroll };
}

/**
 * The matrix itself: a native table inside its own scroll container, so a
 * wide matrix scrolls horizontally there and never the page. The header rows
 * and the requirement column are sticky; body rows are windowed — only the
 * rows in view (plus an overscan) are in the DOM, with spacer rows keeping the
 * scroll height, and `aria-rowcount`/`aria-rowindex` telling assistive
 * technology the true size.
 */
function MatrixGrid({ matrix, projectKey }: { matrix: Matrix; projectKey: string }) {
  const { ref, scrollTop, height, onScroll } = useViewport();
  const { rows, groups, columns } = matrix;
  // The two sticky header rows sit above the first body row.
  const bodyTop = Math.max(0, scrollTop - HEADER_HEIGHT);
  const { start, end } = visibleRange(
    rows.length,
    bodyTop,
    height,
    MATRIX_ROW_HEIGHT,
    MATRIX_OVERSCAN,
  );
  const before = start * MATRIX_ROW_HEIGHT;
  const after = (rows.length - end) * MATRIX_ROW_HEIGHT;
  const span = columns.length + 1;

  const stickyHead = 'sticky top-0 z-20 bg-card';
  const corner = 'sticky left-0 z-30 bg-card';

  return (
    <div
      ref={ref}
      onScroll={onScroll}
      // A scrollable region must be reachable by keyboard (WCAG 2.1.1).
      // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
      tabIndex={0}
      role="region"
      aria-label="Coverage matrix, scrollable"
      data-testid="coverage-matrix-viewport"
      className="max-h-[70vh] w-0 min-w-full overflow-auto rounded-md border border-border focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <table
        className="border-separate border-spacing-0 text-sm"
        aria-label="Requirement coverage"
        aria-rowcount={rows.length + 2}
        aria-colcount={span}
      >
        <thead className={stickyHead}>
          <tr aria-rowindex={1} className="h-8">
            <th
              scope="col"
              rowSpan={2}
              className={cn(
                corner,
                'w-44 min-w-44 border-b border-r border-border px-3 text-left align-bottom text-xs font-medium text-muted-foreground sm:w-72 sm:min-w-72',
              )}
            >
              Requirement
            </th>
            {groups.map((group) => (
              <th
                key={group.file}
                scope="colgroup"
                colSpan={group.columns.length}
                title={group.file}
                data-file={group.file}
                className="h-8 max-w-0 truncate border-b border-r border-border bg-card px-2 text-left font-mono text-2xs font-medium text-muted-foreground"
              >
                {group.file}
              </th>
            ))}
            {columns.length === 0 ? (
              <th
                scope="col"
                rowSpan={2}
                className="border-b border-border bg-card px-3 text-left text-xs font-normal text-muted-foreground"
              >
                No linked tests
              </th>
            ) : null}
          </tr>
          <tr aria-rowindex={2} className="h-8">
            {columns.map((column) => (
              <th
                key={column.test}
                scope="col"
                title={column.test}
                data-test={column.test}
                className="h-8 w-28 min-w-28 max-w-28 truncate border-b border-r border-border bg-card px-2 text-left font-mono text-2xs font-medium"
              >
                {column.symbol || '(file)'}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {before > 0 ? (
            <tr aria-hidden="true" style={{ height: before }}>
              <td colSpan={span} />
            </tr>
          ) : null}
          {rows.slice(start, end).map((row, offset) => (
            <MatrixBodyRow
              key={row.ref}
              row={row}
              index={start + offset}
              columns={columns}
              projectKey={projectKey}
              emptyColumns={columns.length === 0}
            />
          ))}
          {after > 0 ? (
            <tr aria-hidden="true" style={{ height: after }}>
              <td colSpan={span} />
            </tr>
          ) : null}
        </tbody>
      </table>
    </div>
  );
}

function MatrixBodyRow({
  row,
  index,
  columns,
  projectKey,
  emptyColumns,
}: {
  row: MatrixRow;
  index: number;
  columns: Matrix['columns'];
  projectKey: string;
  emptyColumns: boolean;
}) {
  const reasons = row.reasons.join(' · ');
  return (
    <tr
      aria-rowindex={index + 3}
      data-ref={row.ref}
      data-status={row.status}
      style={{ height: MATRIX_ROW_HEIGHT }}
    >
      <th
        scope="row"
        className="sticky left-0 z-10 w-44 min-w-44 max-w-44 border-b border-r border-border bg-card px-3 py-1 text-left align-middle font-normal sm:w-72 sm:min-w-72 sm:max-w-72"
      >
        <div className="flex min-w-0 flex-col gap-0.5 overflow-hidden">
          <span className="flex min-w-0 items-center gap-1.5">
            <Link
              to="/p/$project/specs/$spec/$req"
              params={{ project: projectKey, ...requirementRouteParams(row.ref) }}
              className="truncate font-mono text-xs leading-4 text-accent underline-offset-4 hover:underline"
            >
              {row.ref}
            </Link>
            <Link
              to="/p/$project/items/$id"
              params={{ project: projectKey, id: bareItemId(row.spec) }}
              hash={row.anchor || requirementAnchor(row.ref)}
              aria-label={`Open ${row.ref} in spec`}
              title="Open in spec"
              className="shrink-0 text-muted-foreground hover:text-accent"
            >
              <ExternalLink aria-hidden="true" className="h-3 w-3" />
            </Link>
          </span>
          <span className="truncate text-sm leading-5" title={row.title}>
            {row.title}
          </span>
          <span className="flex min-w-0 items-center gap-1.5">
            <CoverageBadge state={row.status} />
            {reasons ? (
              <span
                className="truncate text-2xs text-muted-foreground"
                title={`Reasons: ${row.reasons.join(', ')}`}
                data-reasons
              >
                {reasons}
              </span>
            ) : null}
          </span>
        </div>
      </th>
      {columns.map((column) => (
        <td
          key={column.test}
          className="border-b border-r border-border px-2 text-left align-middle"
        >
          <ResultCell result={row.results.get(column.test)} />
        </td>
      ))}
      {emptyColumns ? <td className="border-b border-border" /> : null}
    </tr>
  );
}
