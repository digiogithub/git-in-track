import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { ArrowLeft, CircleAlert, ExternalLink, GitCompareArrows, Sparkles } from 'lucide-react';
import { useId, useMemo, useState, type FormEvent } from 'react';

import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { bareItemId } from '@/features/backlog/item-meta';
import { useBacklogEvents, useProject } from '@/features/backlog/queries';
import { CoverageBadge } from '@/features/specs/CoverageBadge';
import {
  DEFAULT_BASE,
  formatRange,
  groupByTier,
  impactRange,
  isWorktree,
  parseImpactSearch,
  readReason,
  refSuggestions,
  tierLabels,
  tierStatusText,
  WORKTREE,
  type ImpactHit,
  type ImpactSearch,
  type TierGroup,
} from '@/features/specs/impact';
import { useImpact, useRefStatus } from '@/features/specs/queries';
import { requirementRouteParams } from '@/features/specs/trace-groups';
import { cn } from '@/lib/cn';

type SearchRecord = Record<string, unknown>;
type NavigateWithSearch = (options: { search: (prev: SearchRecord) => SearchRecord }) => void;

/**
 * The requirement impact view (`/p/$project/specs/impact`, story GIT-US-0131,
 * doc 03 §21.11): pick a base and a head — a branch, a recent commit or the
 * working tree — and see the requirements the diff affects, grouped by tier.
 * Tier 1 hits are reached by a trace, tier 2 through a call (Pando), tier 3
 * are semantic candidates with a score, drawn apart and never mixed with the
 * certain hits. `?base=&head=` in the URL makes a range shareable. Impact is
 * a companion answer: browser-only mode renders `unavailable` with a hint.
 */
export function ImpactView() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const raw = useSearch({ strict: false });
  const search = useMemo(() => parseImpactSearch(raw), [raw]);
  const range = impactRange(search);
  const navigate = useNavigate() as unknown as NavigateWithSearch;
  const provider = useProvider();

  useBacklogEvents(projectKey);

  const project = useProject(projectKey).data;
  const impactQuery = useImpact(projectKey, range);
  const unavailable =
    impactQuery.error instanceof ProviderError && impactQuery.error.code === 'unavailable'
      ? impactQuery.error
      : undefined;
  const failed = unavailable ? undefined : impactQuery.error;

  // The pickers only matter where impact answers at all: companion mode.
  const refStatus = useRefStatus(project?.vaultId, provider.capabilities.git && !unavailable);
  const suggestions = useMemo(() => refSuggestions(refStatus.data), [refStatus.data]);

  const groups = useMemo(
    () => (impactQuery.data ? groupByTier(impactQuery.data) : []),
    [impactQuery.data],
  );

  const apply = (next: ImpactSearch) => {
    navigate({
      search: (prev) => {
        const out: SearchRecord = { ...prev };
        delete out['base'];
        delete out['head'];
        if (next.base) out['base'] = next.base;
        if (next.head && !isWorktree(next.head)) out['head'] = next.head;
        return out;
      },
    });
  };

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
        <h1 className="page-title">Impact</h1>
        <p className="text-sm text-muted-foreground">
          Requirements of <strong>{projectKey}</strong> a diff affects, by how certainly it reaches
          them.
        </p>
      </header>

      {unavailable ? (
        <Card role="status">
          <CardHeader>
            <CardTitle>Impact unavailable</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm text-muted-foreground">
            <p>{unavailable.message}</p>
            <p>
              Impact needs the git history, code scanner and test results only the companion reads:
              run <code className="font-mono">gintrack serve</code> in the repository and open the
              app it serves.
            </p>
          </CardContent>
        </Card>
      ) : (
        <>
          <RangeForm
            key={`${range.base}..${range.head ?? ''}`}
            base={range.base}
            head={range.head}
            suggestions={suggestions}
            onApply={apply}
          />

          {failed ? (
            <Card>
              <CardHeader>
                <CardTitle>
                  The impact of {formatRange(range.base, range.head)} could not be read
                </CardTitle>
              </CardHeader>
              <CardContent className="text-sm text-muted-foreground">{failed.message}</CardContent>
            </Card>
          ) : null}

          {!failed && impactQuery.isPending ? (
            <p className="py-8 text-center text-sm text-muted-foreground">Resolving the impact…</p>
          ) : null}

          {impactQuery.data ? (
            <>
              <p className="text-sm text-muted-foreground" data-testid="impact-summary">
                <span className="font-mono text-foreground">
                  {formatRange(impactQuery.data.base, impactQuery.data.head)}
                </span>
                : {impactQuery.data.files} files, {impactQuery.data.symbols} symbols,{' '}
                {impactQuery.data.hits.length}{' '}
                {impactQuery.data.hits.length === 1 ? 'requirement' : 'requirements'}
              </p>
              {groups.map((group) => (
                <TierSection key={group.tier} group={group} projectKey={projectKey} />
              ))}
            </>
          ) : null}
        </>
      )}
    </div>
  );
}

/** The base and head pickers: free refs with suggestions, the working tree as the default head. */
function RangeForm({
  base,
  head,
  suggestions,
  onApply,
}: {
  base: string;
  head: string | undefined;
  suggestions: { base: string[]; head: string[] };
  onApply: (next: ImpactSearch) => void;
}) {
  const id = useId();
  const [baseValue, setBaseValue] = useState(base);
  const [headValue, setHeadValue] = useState(head ?? WORKTREE);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const nextBase = baseValue.trim() || DEFAULT_BASE;
    const nextHead = headValue.trim();
    onApply({ base: nextBase, ...(nextHead && !isWorktree(nextHead) ? { head: nextHead } : {}) });
  };

  return (
    <form
      onSubmit={submit}
      aria-label="Diff range"
      className="flex flex-wrap items-end gap-3 rounded-md border border-border bg-card p-3"
    >
      <div className="min-w-0 flex-1 basis-40 space-y-1">
        <Label htmlFor={`${id}-base`}>Base</Label>
        <Input
          id={`${id}-base`}
          list={`${id}-base-refs`}
          value={baseValue}
          onChange={(e) => setBaseValue(e.target.value)}
          placeholder={DEFAULT_BASE}
          autoComplete="off"
          spellCheck={false}
          className="font-mono"
        />
        <datalist id={`${id}-base-refs`}>
          {suggestions.base.map((ref) => (
            <option key={ref} value={ref} />
          ))}
        </datalist>
      </div>
      <div className="min-w-0 flex-1 basis-40 space-y-1">
        <Label htmlFor={`${id}-head`}>Head</Label>
        <Input
          id={`${id}-head`}
          list={`${id}-head-refs`}
          value={headValue}
          onChange={(e) => setHeadValue(e.target.value)}
          placeholder={WORKTREE}
          autoComplete="off"
          spellCheck={false}
          className="font-mono"
          aria-describedby={`${id}-head-hint`}
        />
        <datalist id={`${id}-head-refs`}>
          {suggestions.head.map((ref) => (
            <option key={ref} value={ref} label={ref === WORKTREE ? 'working tree' : undefined} />
          ))}
        </datalist>
      </div>
      <Button type="submit" className="gap-1.5">
        <GitCompareArrows aria-hidden="true" className="h-4 w-4" />
        Show impact
      </Button>
      <p id={`${id}-head-hint`} className="w-full text-xs text-muted-foreground">
        A branch, tag or commit (<code className="font-mono">HEAD~1</code>). Head{' '}
        <code className="font-mono">{WORKTREE}</code> is the working tree, uncommitted changes
        included; another head is exact only when it is <code className="font-mono">HEAD</code> of a
        clean tree.
      </p>
    </form>
  );
}

function TierSection({ group, projectKey }: { group: TierGroup; projectKey: string }) {
  const label = tierLabels[group.tier];
  const statusText = tierStatusText(group.status);
  const problem = group.status?.status === 'unavailable' || group.status?.status === 'error';
  const headingId = useId();
  const candidates = group.tier === 3;

  return (
    <section aria-labelledby={headingId} data-tier={group.tier} className="min-w-0 space-y-2">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
        <h2 id={headingId} className="text-base font-semibold">
          Tier {group.tier} · {label.title}
        </h2>
        <span className="text-xs text-muted-foreground">
          {group.hits.length} {group.hits.length === 1 ? 'hit' : 'hits'} — {label.hint}
        </span>
      </div>
      {statusText ? (
        <p
          role="status"
          data-tier-status={group.status?.status ?? 'missing'}
          className={cn(
            'flex items-start gap-1.5 text-xs',
            problem ? 'text-warning' : 'text-muted-foreground',
          )}
        >
          {problem ? (
            <CircleAlert aria-hidden="true" className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          ) : null}
          <span className="min-w-0 break-words">{statusText}</span>
        </p>
      ) : null}
      {group.hits.length > 0 ? (
        <ul className="space-y-2" aria-label={`Tier ${group.tier} hits`}>
          {group.hits.map((hit) => (
            <HitRow key={hit.ref} hit={hit} projectKey={projectKey} candidate={candidates} />
          ))}
        </ul>
      ) : group.status?.status === 'ok' ? (
        <p className="empty-state text-sm">No requirement reached at this tier.</p>
      ) : null}
    </section>
  );
}

function HitRow({
  hit,
  projectKey,
  candidate,
}: {
  hit: ImpactHit;
  projectKey: string;
  candidate: boolean;
}) {
  const route = requirementRouteParams(hit.ref);
  return (
    <li
      data-ref={hit.ref}
      data-candidate={candidate ? 'true' : undefined}
      className={cn(
        'min-w-0 space-y-1.5 rounded-md border p-3',
        candidate ? 'border-dashed border-border-strong bg-transparent' : 'border-border bg-card',
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <Link
          to="/p/$project/specs/$spec/$req"
          params={{ project: projectKey, spec: route.spec, req: route.req }}
          className={cn(
            'break-all font-mono text-sm font-medium hover:underline',
            candidate ? 'text-muted-foreground' : 'text-foreground',
          )}
        >
          {hit.ref}
        </Link>
        {candidate ? (
          <Badge variant="outline" size="sm" data-candidate-badge="">
            <Sparkles aria-hidden="true" />
            candidate
            {hit.score !== undefined ? ` · score ${hit.score.toFixed(3)}` : ''}
          </Badge>
        ) : null}
        {hit.status ? <CoverageBadge state={hit.status} /> : null}
        {hit.suspect && hit.status !== 'suspect' ? (
          <Badge
            variant="warning"
            size="sm"
            data-suspect=""
            title="Changed and not re-verified at head."
          >
            <CircleAlert aria-hidden="true" />
            suspect
          </Badge>
        ) : null}
      </div>
      <p className={cn('min-w-0 break-words text-sm', candidate && 'text-muted-foreground')}>
        {hit.title}
      </p>
      {hit.reasons.length > 0 ? (
        <ul className="flex flex-wrap gap-1.5" aria-label="Reasons">
          {hit.reasons.map((reason) => {
            const read = readReason(reason);
            return (
              <li
                key={reason}
                title={read.title}
                className="inline-flex min-w-0 max-w-full items-baseline gap-1 rounded bg-secondary px-1.5 py-0.5 text-2xs"
              >
                <span className="shrink-0 font-medium">{read.kind}</span>
                {read.text ? (
                  <span className="min-w-0 break-all font-mono text-muted-foreground">
                    {read.text}
                  </span>
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : null}
      {hit.pending?.length ? (
        <p className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
          <span>Pending delta in</span>
          {hit.pending.map((id) => (
            <Link
              key={id}
              to="/p/$project/items/$id"
              params={{ project: projectKey, id: bareItemId(id) }}
              className="inline-flex items-center gap-0.5 font-mono text-foreground hover:underline"
            >
              {bareItemId(id)}
              <ExternalLink aria-hidden="true" className="h-3 w-3" />
            </Link>
          ))}
        </p>
      ) : null}
    </li>
  );
}
