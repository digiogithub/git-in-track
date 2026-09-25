import { Link, useParams } from '@tanstack/react-router';
import { ArrowLeft, BadgeCheck, ExternalLink, Pencil } from 'lucide-react';
import { useId, useMemo, useState } from 'react';

import type { CoverageRow, ProjectSummary, Requirement, RequirementPatch } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { useToast } from '@/components/ui/toast';
import { StatusBadge } from '@/features/backlog/Badges';
import { bareItemId } from '@/features/backlog/item-meta';
import { ItemBody } from '@/features/backlog/ItemBody';
import { useBacklogEvents, useProject } from '@/features/backlog/queries';
import { ConflictFields, type ConflictFieldRow } from '@/features/editor/ConflictFields';
import { allowedStatuses, readProjectSchema } from '@/features/editor/project-schema';
import { CoverageBadge } from '@/features/specs/CoverageBadge';
import {
  useRequirement,
  useRequirementCoverage,
  useRequirementTrace,
  useUpdateRequirement,
} from '@/features/specs/queries';
import { refFromRouteParams } from '@/features/specs/trace-groups';
import { TracePanel } from '@/features/specs/TracePanel';
import { requirementAnchor } from '@/markdown';

/** What an edit started from: the requirement rev it quotes and the values it changes. */
type Draft = { title: string; text: string; baseRev: string; baseTitle: string; baseText: string };

/** A save refused with `stale_revision`, kept until the user decides. */
type Conflict = { error: ProviderError; patch: RequirementPatch };

const fieldLabels: Record<string, string> = {
  text: 'Statement and scenarios',
  title: 'Title',
  status: 'Status',
  trace: 'Trace',
  verified: 'Verified',
  links: 'Links',
};

/**
 * The requirement detail (`/p/$project/specs/$spec/$req`, story GIT-US-0129,
 * ADR-037): one requirement block rendered as Markdown, its workflow status
 * control, the durable `verified` stamp, its computed coverage with the
 * suspect reasons, and the trace panel. Editing saves only this block through
 * `updateRequirement`, quoting the **requirement rev** (the write token, doc
 * 03 §21.5) — never the block rev a stamp records. A `stale_revision` shows
 * the per-field diff and lets the user reload theirs or save theirs-aware.
 * Browser-only mode reads and edits the block; trace and coverage read
 * `unavailable`.
 */
export function RequirementDetail() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const specId = params.spec ?? '';
  const ref = refFromRouteParams(specId, params.req ?? '');
  const provider = useProvider();
  const writable = provider.capabilities.write;

  useBacklogEvents(projectKey);

  const project = useProject(projectKey).data;
  const requirementQuery = useRequirement(projectKey, ref);
  const traceQuery = useRequirementTrace(projectKey, ref);
  const coverageQuery = useRequirementCoverage(projectKey, ref);

  const results = useMemo(
    () => new Map((coverageQuery.data?.tests ?? []).map((t) => [t.test, t.result])),
    [coverageQuery.data],
  );

  const requirement = requirementQuery.data?.requirement;

  return (
    <div className="min-w-0 space-y-4">
      <header className="space-y-2">
        <Link
          to="/p/$project/specs"
          params={{ project: projectKey }}
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft aria-hidden="true" className="h-3 w-3" />
          Specs
        </Link>
        {requirement ? (
          <RequirementHeading requirement={requirement} project={project} projectKey={projectKey} />
        ) : (
          <h1 className="page-title font-mono">{ref}</h1>
        )}
      </header>

      {requirementQuery.isPending ? (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading the requirement…</p>
      ) : null}

      {requirementQuery.error ? (
        <Card>
          <CardHeader>
            <CardTitle>
              {requirementQuery.error instanceof ProviderError &&
              requirementQuery.error.code === 'not_found'
                ? `${ref} was not found`
                : 'The requirement could not be read'}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            {requirementQuery.error.message}
          </CardContent>
        </Card>
      ) : null}

      {requirement ? (
        <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
          <div className="min-w-0 space-y-4">
            <BlockCard
              key={requirement.ref}
              requirement={requirement}
              projectKey={projectKey}
              writable={writable}
              onReload={() => void requirementQuery.refetch()}
            />
            <TracePanel
              projectKey={projectKey}
              trace={traceQuery.data}
              error={traceQuery.error}
              loading={traceQuery.isPending}
              results={results}
            />
          </div>
          <aside className="min-w-0 space-y-4" aria-label="Requirement state">
            <StatusCard
              requirement={requirement}
              project={project}
              projectKey={projectKey}
              writable={writable}
            />
            <CoverageCard
              state={
                coverageQuery.isError
                  ? { kind: 'unavailable', error: coverageQuery.error }
                  : coverageQuery.isPending
                    ? { kind: 'loading' }
                    : { kind: 'ready', row: coverageQuery.data ?? null }
              }
            />
            <VerifiedCard verified={requirement.verified} blockRev={requirement.blockRev} />
          </aside>
        </div>
      ) : null}
    </div>
  );
}

function RequirementHeading({
  requirement,
  project,
  projectKey,
}: {
  requirement: Requirement;
  project: ProjectSummary | undefined;
  projectKey: string;
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0 space-y-1">
        <p className="flex flex-wrap items-center gap-2 text-xs">
          <span className="font-mono text-muted-foreground">{requirement.ref}</span>
          <StatusBadge status={requirement.status} project={project} />
        </p>
        <h1 className="page-title break-words">{requirement.title}</h1>
      </div>
      <Link
        to="/p/$project/items/$id"
        params={{ project: projectKey, id: bareItemId(requirement.spec) }}
        hash={requirement.anchor || requirementAnchor(requirement.ref)}
        className="inline-flex h-9 items-center gap-1.5 rounded-md border border-input px-3 text-sm font-medium hover:bg-secondary"
      >
        <ExternalLink aria-hidden="true" className="h-4 w-4" />
        Open in spec
      </Link>
    </div>
  );
}

/** The block, read or edited; a save patches only what changed, under the rev the edit started from. */
function BlockCard({
  requirement,
  projectKey,
  writable,
  onReload,
}: {
  requirement: Requirement;
  projectKey: string;
  writable: boolean;
  onReload: () => void;
}) {
  const [draft, setDraft] = useState<Draft | null>(null);
  const [conflict, setConflict] = useState<Conflict | null>(null);
  const update = useUpdateRequirement(projectKey);
  const { toast } = useToast();
  const titleId = useId();
  const textId = useId();
  const text = requirement.text ?? '';

  const startEditing = () => {
    setConflict(null);
    setDraft({
      title: requirement.title,
      text,
      baseRev: requirement.rev,
      baseTitle: requirement.title,
      baseText: text,
    });
  };

  const save = (patch: RequirementPatch, rev: string) => {
    update.mutate(
      { ref: requirement.ref, patch, rev },
      {
        onSuccess: (result) => {
          setDraft(null);
          setConflict(null);
          toast({ title: `Saved ${result.requirement.ref}` });
        },
        onError: (error) => {
          if (error instanceof ProviderError && error.code === 'stale_revision') {
            setConflict({ error, patch });
            onReload();
            return;
          }
          toast({
            variant: 'destructive',
            title: 'The requirement could not be saved',
            description: error.message,
          });
        },
      },
    );
  };

  const submit = () => {
    if (!draft) return;
    const patch: RequirementPatch = {};
    const title = draft.title.trim();
    if (title !== draft.baseTitle) patch.title = title;
    if (draft.text !== draft.baseText) patch.text = draft.text;
    if (patch.title === undefined && patch.text === undefined) {
      setDraft(null);
      return;
    }
    save(patch, draft.baseRev);
  };

  return (
    <Card aria-labelledby="block-title">
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0 p-4 pb-2">
        <CardTitle id="block-title" className="text-base">
          Requirement
        </CardTitle>
        {writable && !draft ? (
          <Button variant="outline" size="sm" onClick={startEditing}>
            <Pencil aria-hidden="true" className="h-3.5 w-3.5" />
            Edit block
          </Button>
        ) : null}
      </CardHeader>
      <CardContent className="space-y-4 p-4 pt-0">
        {conflict ? (
          <ConflictPanel
            conflict={conflict}
            requirement={requirement}
            busy={update.isPending}
            onReload={() => {
              setConflict(null);
              setDraft(null);
              onReload();
            }}
            onRetry={() => {
              // Quote the rev on disk now: the core's own `currentRev`, else the
              // one the reload just read. Never `*`.
              const rev = conflict.error.currentRev ?? requirement.rev;
              setDraft((current) => (current ? { ...current, baseRev: rev } : current));
              save(conflict.patch, rev);
            }}
            onKeepEditing={() => setConflict(null)}
          />
        ) : null}

        {draft ? (
          <form
            aria-label="Edit requirement"
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              submit();
            }}
          >
            <div className="space-y-1.5">
              <Label htmlFor={titleId}>Title</Label>
              <Input
                id={titleId}
                value={draft.title}
                required
                maxLength={200}
                autoComplete="off"
                onChange={(event) => setDraft({ ...draft, title: event.target.value })}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={textId}>Statement and scenarios</Label>
              <Textarea
                id={textId}
                rows={12}
                className="font-mono text-sm"
                value={draft.text}
                onChange={(event) => setDraft({ ...draft, text: event.target.value })}
              />
              <p className="text-xs text-muted-foreground">
                Only this block is written; the heading keeps its ref and the rest of the spec is
                untouched.
              </p>
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <Button
                type="button"
                variant="ghost"
                disabled={update.isPending}
                onClick={() => {
                  setDraft(null);
                  setConflict(null);
                }}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={draft.title.trim() === '' || update.isPending || conflict !== null}
              >
                {update.isPending ? 'Saving…' : 'Save block'}
              </Button>
            </div>
          </form>
        ) : text.trim() === '' ? (
          <p className="text-sm text-muted-foreground">This requirement has no statement yet.</p>
        ) : (
          <ItemBody
            body={text}
            path={requirement.path}
            project={projectKey}
            cacheKey={`${requirement.ref}@${requirement.blockRev}`}
          />
        )}
      </CardContent>
    </Card>
  );
}

function ConflictPanel({
  conflict,
  requirement,
  busy,
  onReload,
  onRetry,
  onKeepEditing,
}: {
  conflict: Conflict;
  requirement: Requirement;
  busy: boolean;
  onReload: () => void;
  onRetry: () => void;
  onKeepEditing: () => void;
}) {
  const { error, patch } = conflict;
  // The core names the text without quoting it; once the reload has read the
  // rev it reported, the text on disk is known and both sides can be shown.
  const fresh = error.currentRev !== undefined && requirement.rev === error.currentRev;
  const rows: ConflictFieldRow[] = (error.conflicts ?? []).map((field) => {
    const row: ConflictFieldRow = { ...field, label: fieldLabels[field.field] ?? field.field };
    if (field.field === 'text') {
      if (row.current === undefined && fresh) row.current = requirement.text ?? '';
      if (row.proposed === undefined && patch.text !== undefined) row.proposed = patch.text;
    }
    return row;
  });
  return (
    <div
      role="alert"
      aria-labelledby="requirement-conflict-title"
      className="space-y-3 rounded-md border border-warning bg-warning/10 p-3"
    >
      <h2 id="requirement-conflict-title" className="text-sm font-semibold">
        {requirement.ref} changed on disk
      </h2>
      <p className="text-sm text-muted-foreground">
        Someone (or something) wrote this requirement after you started editing, so the revision
        check failed. Nothing has been saved yet.
      </p>
      {rows.length > 0 ? (
        <ConflictFields fields={rows} />
      ) : (
        <p className="text-sm text-muted-foreground">
          The runtime named no field in conflict: reload to see what is on disk now.
        </p>
      )}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="ghost" size="sm" disabled={busy} onClick={onKeepEditing}>
          Keep editing
        </Button>
        <Button variant="outline" size="sm" disabled={busy} onClick={onReload}>
          Reload theirs
        </Button>
        <Button variant="destructive" size="sm" disabled={busy} onClick={onRetry}>
          Save mine over theirs
        </Button>
      </div>
    </div>
  );
}

/** The workflow status, moved like an item's: through the declared transitions only. */
function StatusCard({
  requirement,
  project,
  projectKey,
  writable,
}: {
  requirement: Requirement;
  project: ProjectSummary | undefined;
  projectKey: string;
  writable: boolean;
}) {
  const update = useUpdateRequirement(projectKey);
  const { toast } = useToast();
  const selectId = useId();
  const schema = readProjectSchema(project);
  // A triage-category status is not a requirement status (E-REQ-STATUS).
  const options = allowedStatuses(schema, requirement.status).filter(
    (status) => status.category !== 'triage',
  );
  const listed = options.some((status) => status.id === requirement.status);

  return (
    <Card>
      <CardHeader className="p-4 pb-2">
        <CardTitle className="text-base">
          <label htmlFor={selectId}>Status</label>
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 p-4 pt-0">
        <Select
          id={selectId}
          value={requirement.status}
          disabled={!writable || update.isPending}
          onChange={(event) => {
            const status = event.target.value;
            if (!status || status === requirement.status) return;
            update.mutate(
              { ref: requirement.ref, patch: { status }, rev: requirement.rev },
              {
                onSuccess: () => {
                  toast({ title: `Moved ${requirement.ref} to ${status}` });
                },
                onError: (error) => {
                  if (error instanceof ProviderError && error.code === 'stale_revision') {
                    toast({
                      variant: 'destructive',
                      title: 'Changed on disk',
                      description: `${requirement.ref} was modified elsewhere. It has been reloaded — review it and try again.`,
                    });
                    return;
                  }
                  toast({
                    variant: 'destructive',
                    title: 'The status could not be changed',
                    description: error.message,
                  });
                },
              },
            );
          }}
        >
          {listed ? null : <option value={requirement.status}>{requirement.status}</option>}
          {options.map((status) => (
            <option key={status.id} value={status.id}>
              {status.name}
            </option>
          ))}
        </Select>
        {requirement.statusImplicit ? (
          <p className="text-xs text-muted-foreground">
            No entry in the spec yet: this is the workflow&rsquo;s initial status.
          </p>
        ) : null}
        {!writable ? <p className="text-xs text-muted-foreground">Read-only workspace</p> : null}
      </CardContent>
    </Card>
  );
}

type CoverageState =
  | { kind: 'loading' }
  | { kind: 'unavailable'; error: Error }
  | { kind: 'ready'; row: CoverageRow | null };

/** The computed coverage state and its reason codes (doc 03 §21.6); never stored. */
function CoverageCard({ state }: { state: CoverageState }) {
  return (
    <Card>
      <CardHeader className="p-4 pb-2">
        <CardTitle className="text-base">Coverage</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 p-4 pt-0 text-sm">
        {state.kind === 'loading' ? <CoverageBadge state={undefined} /> : null}
        {state.kind === 'unavailable' ? (
          <>
            <CoverageBadge state="unavailable" />
            <p role="status" className="text-xs text-muted-foreground">
              {state.error instanceof ProviderError && state.error.code === 'unavailable'
                ? state.error.message
                : `Coverage could not be read: ${state.error.message}`}
            </p>
          </>
        ) : null}
        {state.kind === 'ready' ? (
          <>
            <CoverageBadge state={state.row?.status ?? 'untested'} />
            {state.row?.reasons?.length ? (
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">
                  {state.row.status === 'suspect' ? 'Suspect because' : 'Reasons'}
                </p>
                <ul aria-label="Coverage reasons" className="flex flex-wrap gap-1">
                  {state.row.reasons.map((reason) => (
                    <li key={reason}>
                      <code className="rounded bg-secondary px-1.5 py-0.5 font-mono text-2xs">
                        {reason}
                      </code>
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </>
        ) : null}
      </CardContent>
    </Card>
  );
}

/** The durable `verified` stamp (doc 03 §21.6, R-REQ-11a), compared with the current block rev. */
function VerifiedCard({
  verified,
  blockRev,
}: {
  verified: Requirement['verified'];
  blockRev: string;
}) {
  const stamped = verified && (verified.rev || verified.commit || verified.at || verified.by);
  return (
    <Card>
      <CardHeader className="p-4 pb-2">
        <CardTitle className="flex items-center gap-1.5 text-base">
          <BadgeCheck aria-hidden="true" className="h-4 w-4" />
          Verified
        </CardTitle>
      </CardHeader>
      <CardContent className="p-4 pt-0 text-sm">
        {!stamped ? (
          <p className="text-muted-foreground">Never verified: no stamp in the spec.</p>
        ) : (
          <dl
            aria-label="Verification stamp"
            className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1"
          >
            <dt className="text-muted-foreground">rev</dt>
            <dd className="min-w-0">
              <code className="break-all font-mono text-xs">{verified.rev ?? '—'}</code>{' '}
              {verified.rev ? (
                verified.rev === blockRev ? (
                  <span className="text-xs text-success">current text</span>
                ) : (
                  <span className="text-xs text-warning">older text</span>
                )
              ) : null}
            </dd>
            <dt className="text-muted-foreground">commit</dt>
            <dd className="min-w-0">
              <code className="font-mono text-xs" title={verified.commit}>
                {verified.commit ? verified.commit.slice(0, 12) : '—'}
              </code>
            </dd>
            <dt className="text-muted-foreground">at</dt>
            <dd className="min-w-0 text-xs">
              {verified.at ? <time dateTime={verified.at}>{verified.at}</time> : '—'}
            </dd>
            <dt className="text-muted-foreground">by</dt>
            <dd className="min-w-0 text-xs">{verified.by ?? '—'}</dd>
          </dl>
        )}
      </CardContent>
    </Card>
  );
}
