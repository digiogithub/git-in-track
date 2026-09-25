import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { ChevronDown, ChevronRight, Grid3x3, Plus } from 'lucide-react';
import { useCallback, useId, useMemo, useState } from 'react';

import type { ProjectSummary } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { StatusBadge } from '@/features/backlog/Badges';
import { bareItemId } from '@/features/backlog/item-meta';
import { NewItemLink } from '@/features/backlog/NewItemLink';
import { useBacklogEvents, useProject } from '@/features/backlog/queries';
import { AddRequirementDialog } from '@/features/specs/AddRequirementDialog';
import { CoverageBadge } from '@/features/specs/CoverageBadge';
import {
  countCoverage,
  groupRequirements,
  type CoverageView,
  type SpecGroup,
} from '@/features/specs/grouping';
import { useCoverage, useRequirements, useSpecs } from '@/features/specs/queries';
import {
  coverageStates,
  listParam,
  parseSpecSearch,
  type SpecSearch,
  type SpecSearchInput,
} from '@/features/specs/search';
import { cn } from '@/lib/cn';
import { requirementAnchor } from '@/markdown';

type SearchRecord = Record<string, unknown>;
type NavigateWithSearch = (options: {
  search: (prev: SearchRecord) => SearchRecord;
  replace?: boolean;
}) => void;

/** Merges a patch into the URL filter; `undefined` clears a param. */
function useSetSpecSearch(): (patch: SpecSearchInput) => void {
  const navigate = useNavigate() as unknown as NavigateWithSearch;
  return useCallback(
    (patch: SpecSearchInput) => {
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
  'inline-flex h-7 items-center gap-1 rounded-full border px-2.5 text-xs font-medium transition-colors duration-fast disabled:cursor-not-allowed disabled:opacity-50';
const chipOff = 'border-input text-muted-foreground hover:bg-secondary hover:text-foreground';
const chipOn = 'border-transparent bg-primary text-primary-foreground';

/**
 * The specs page (`/p/$project/specs`, story GIT-US-0128): every spec is a
 * collapsible group and every requirement a row of its own, with its ref,
 * title, workflow status and computed coverage (ADR-037 decision 1).
 * Coverage is a companion answer; in browser-only mode every badge reads
 * `unavailable` and the rest of the page works unchanged. The header links
 * the coverage matrix (`specs/coverage`, GIT-US-0130).
 */
export function SpecsPage() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const raw = useSearch({ strict: false });
  const search = useMemo(() => parseSpecSearch(raw), [raw]);
  const setSearch = useSetSpecSearch();
  const provider = useProvider();
  const writable = provider.capabilities.write;

  useBacklogEvents(projectKey);

  const project = useProject(projectKey).data;
  const specsQuery = useSpecs(projectKey);
  const requirementsQuery = useRequirements(projectKey);
  const coverageQuery = useCoverage(projectKey);

  const coverage = useMemo<CoverageView>(() => {
    if (coverageQuery.isSuccess) {
      return {
        kind: 'ready',
        byRef: new Map(coverageQuery.data.coverage.map((row) => [row.ref, row])),
      };
    }
    if (coverageQuery.isError) {
      const error = coverageQuery.error;
      return {
        kind: 'unavailable',
        reason:
          error instanceof ProviderError && error.code === 'unavailable'
            ? error.message
            : `Coverage could not be read: ${error.message}`,
      };
    }
    return { kind: 'loading' };
  }, [coverageQuery.isSuccess, coverageQuery.isError, coverageQuery.data, coverageQuery.error]);

  const requirements = useMemo(
    () => requirementsQuery.data?.requirements ?? [],
    [requirementsQuery.data],
  );
  const groups = useMemo(
    () => groupRequirements(specsQuery.data ?? [], requirements, coverage, search),
    [specsQuery.data, requirements, coverage, search],
  );
  const counts = useMemo(() => countCoverage(requirements, coverage), [requirements, coverage]);

  const statuses = (project?.statuses ?? []).filter((status) => status.category !== 'triage');
  const filtered = Boolean(search.status?.length) || Boolean(search.coverage?.length);
  const loading = specsQuery.isPending || requirementsQuery.isPending;
  const failed = specsQuery.error ?? requirementsQuery.error;

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="page-title">Specs</h1>
          <p className="text-sm text-muted-foreground">
            Capabilities of <strong>{projectKey}</strong>, one row per requirement.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Link
            to="/p/$project/specs/coverage"
            params={{ project: projectKey }}
            className="inline-flex h-9 items-center gap-1.5 rounded-md border border-input px-3 text-sm font-medium hover:bg-secondary"
          >
            <Grid3x3 aria-hidden="true" className="h-4 w-4" />
            Coverage matrix
          </Link>
          {writable ? (
            <NewItemLink project={projectKey} type="spec" label="New spec" variant="bar" />
          ) : null}
        </div>
      </header>

      <SpecFilters
        project={project}
        statuses={statuses.map((s) => s.id)}
        search={search}
        counts={counts}
        coverage={coverage}
        onChange={setSearch}
      />

      {coverage.kind === 'unavailable' ? (
        <p
          role="status"
          className="rounded-md border border-dashed border-border-strong px-3 py-2 text-sm text-muted-foreground"
        >
          <strong className="font-medium text-foreground">Coverage unavailable.</strong>{' '}
          {coverage.reason}
        </p>
      ) : null}

      {loading ? (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading specs…</p>
      ) : null}

      {failed ? (
        <Card>
          <CardHeader>
            <CardTitle>The specs could not be read</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">{failed.message}</CardContent>
        </Card>
      ) : null}

      {!loading && !failed && groups.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>{filtered ? 'No requirement matches' : 'No specs yet'}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm text-muted-foreground">
            {filtered ? (
              <>
                <p>No requirement has the status and coverage picked above.</p>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setSearch({ status: undefined, coverage: undefined })}
                >
                  Clear filters
                </Button>
              </>
            ) : (
              <p>
                A spec is an item of type <code className="font-mono">spec</code> describing one
                capability; its requirements are <code className="font-mono">### REF — title</code>{' '}
                blocks under <code className="font-mono">## Requirements</code>.
              </p>
            )}
          </CardContent>
        </Card>
      ) : null}

      {!loading && !failed ? (
        <ul className="space-y-3" aria-label="Specs">
          {groups.map((group) => (
            <li key={group.id}>
              <SpecGroupCard
                group={group}
                project={project}
                projectKey={projectKey}
                writable={writable}
                filtered={filtered}
              />
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function SpecFilters({
  project,
  statuses,
  search,
  counts,
  coverage,
  onChange,
}: {
  project: ProjectSummary | undefined;
  statuses: string[];
  search: SpecSearch;
  counts: Record<(typeof coverageStates)[number], number>;
  coverage: CoverageView;
  onChange: (patch: SpecSearchInput) => void;
}) {
  const noCoverage = coverage.kind !== 'ready';
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
        {statuses.map((status) => {
          const on = search.status?.includes(status) ?? false;
          return (
            <button
              key={status}
              type="button"
              aria-pressed={on}
              className={cn(chipClass, on ? chipOn : chipOff)}
              onClick={() => onChange({ status: listParam(toggle(search.status, status)) })}
            >
              {project?.statuses.find((s) => s.id === status)?.name ?? status}
            </button>
          );
        })}
      </div>
      <div
        role="group"
        aria-label="Filter by coverage"
        className="flex flex-wrap items-center gap-1.5"
      >
        <span className="mr-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Coverage
        </span>
        {coverageStates.map((state) => {
          const on = search.coverage?.includes(state) ?? false;
          return (
            <button
              key={state}
              type="button"
              aria-pressed={on}
              disabled={noCoverage}
              title={noCoverage ? 'Coverage is unavailable here' : undefined}
              className={cn(chipClass, on ? chipOn : chipOff)}
              onClick={() => onChange({ coverage: listParam(toggle(search.coverage, state)) })}
            >
              {state}
              {noCoverage ? null : <span className="tabular-nums opacity-80">{counts[state]}</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}

function SpecGroupCard({
  group,
  project,
  projectKey,
  writable,
  filtered,
}: {
  group: SpecGroup;
  project: ProjectSummary | undefined;
  projectKey: string;
  writable: boolean;
  filtered: boolean;
}) {
  const [open, setOpen] = useState(true);
  const [adding, setAdding] = useState(false);
  const bodyId = useId();
  const title = group.spec?.title ?? group.id;
  const specId = bareItemId(group.id);

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0 p-3 sm:p-4">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <button
            type="button"
            aria-expanded={open}
            aria-controls={bodyId}
            className="inline-flex min-w-0 items-center gap-1.5 rounded-md text-left hover:text-accent"
            onClick={() => setOpen((value) => !value)}
          >
            {open ? (
              <ChevronDown aria-hidden="true" className="h-4 w-4 shrink-0" />
            ) : (
              <ChevronRight aria-hidden="true" className="h-4 w-4 shrink-0" />
            )}
            <CardTitle className="truncate text-base">{title}</CardTitle>
          </button>
          <Link
            to="/p/$project/items/$id"
            params={{ project: projectKey, id: specId }}
            className="font-mono text-xs text-accent underline-offset-4 hover:underline"
          >
            {group.id}
          </Link>
          {group.spec ? <StatusBadge status={group.spec.status} project={project} /> : null}
          <span className="text-xs text-muted-foreground">
            {filtered ? `${group.rows.length} of ${group.total}` : group.total}{' '}
            {group.total === 1 ? 'requirement' : 'requirements'}
          </span>
        </div>
        {writable && group.spec ? (
          <>
            <button
              type="button"
              aria-label={`Add requirement to ${group.id}`}
              className="inline-flex h-7 items-center gap-1 rounded-md border border-input px-2 text-xs font-medium text-muted-foreground hover:bg-secondary hover:text-foreground"
              onClick={() => setAdding(true)}
            >
              <Plus aria-hidden="true" className="h-3 w-3" />
              Add requirement
            </button>
            <AddRequirementDialog
              project={projectKey}
              spec={group.id}
              specTitle={title}
              open={adding}
              onOpenChange={setAdding}
            />
          </>
        ) : null}
      </CardHeader>
      {open ? (
        <CardContent id={bodyId} className="p-0">
          {group.rows.length === 0 ? (
            <p className="border-t border-border px-4 py-3 text-sm text-muted-foreground">
              No requirements yet.
            </p>
          ) : (
            <ul
              aria-label={`Requirements of ${group.id}`}
              className="divide-y divide-border border-t border-border"
            >
              {group.rows.map(({ requirement, coverage }) => (
                <li
                  key={requirement.ref}
                  data-ref={requirement.ref}
                  className="grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1 px-3 py-2 sm:grid-cols-[minmax(9rem,auto)_1fr_auto_auto] sm:px-4"
                >
                  <Link
                    to="/p/$project/items/$id"
                    params={{ project: projectKey, id: specId }}
                    hash={requirement.anchor || requirementAnchor(requirement.ref)}
                    className="font-mono text-xs text-accent underline-offset-4 hover:underline"
                  >
                    {requirement.ref}
                  </Link>
                  <span className="min-w-0 break-words text-sm">{requirement.title}</span>
                  <span className="col-start-2 flex flex-wrap items-center gap-1.5 sm:col-start-auto sm:contents">
                    <StatusBadge status={requirement.status} project={project} />
                    <CoverageBadge state={coverage} />
                  </span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      ) : null}
    </Card>
  );
}
