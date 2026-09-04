import { Link, useParams } from '@tanstack/react-router';
import { useState, type ReactNode } from 'react';

import type { Item, ProjectSummary } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Select } from '@/components/ui/select';
import { ToastProvider, useToast } from '@/components/ui/toast';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import {
  ItemLink,
  LabelChip,
  PriorityBadge,
  StatusBadge,
  TypeBadge,
} from '@/features/backlog/Badges';
import { FeatureLink } from '@/features/backlog/FeatureLink';
import {
  acceptanceProgress,
  formatDate,
  linkKindInverse,
  linkKindName,
  typeName,
  type ItemRelation,
  type LinkKind,
} from '@/features/backlog/item-meta';
import { ItemBody } from '@/features/backlog/ItemBody';
import {
  useAddComment,
  useBacklogEvents,
  useChildren,
  useComments,
  useItem,
  useMoveItem,
  useProject,
} from '@/features/backlog/queries';

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="space-y-0.5">
      <dt className="text-xs uppercase tracking-wide text-muted-foreground">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

function Dash() {
  return <span className="text-muted-foreground">—</span>;
}

/** Typed relations grouped by kind, each annotated with its inverse. */
function LinksPanel({ project, links }: { project: string; links: ItemRelation[] }) {
  const grouped = new Map<LinkKind, ItemRelation[]>();
  for (const link of links) {
    grouped.set(link.kind, [...(grouped.get(link.kind) ?? []), link]);
  }

  if (grouped.size === 0) return <Dash />;

  return (
    <ul className="space-y-2">
      {[...grouped.entries()].map(([kind, entries]) => (
        <li key={kind}>
          <p className="text-xs text-muted-foreground">
            {linkKindName(kind)}{' '}
            <span className="italic">(target sees: {linkKindInverse(kind)})</span>
          </p>
          <ul className="flex flex-wrap gap-2 pt-1">
            {entries.map((link) => (
              <li key={`${kind}-${link.target}`}>
                {link.note ? (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>
                        <ItemLink project={project} id={link.target} />
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>{link.note}</TooltipContent>
                  </Tooltip>
                ) : (
                  <ItemLink project={project} id={link.target} />
                )}
              </li>
            ))}
          </ul>
        </li>
      ))}
    </ul>
  );
}

function StatusPicker({
  item,
  project,
  projectKey,
}: {
  item: Item;
  project: ProjectSummary | undefined;
  projectKey: string;
}) {
  const provider = useProvider();
  const move = useMoveItem(projectKey);
  const { toast } = useToast();
  const canWrite = provider.capabilities.write;

  return (
    <div className="flex items-center gap-2">
      <label
        htmlFor="item-status"
        className="text-xs uppercase tracking-wide text-muted-foreground"
      >
        Status
      </label>
      <Select
        id="item-status"
        className="w-44"
        value={item.status ?? ''}
        disabled={!canWrite || move.isPending}
        onChange={(event) => {
          const status = event.target.value;
          if (!status || status === item.status) return;
          move.mutate(
            { id: item.id, status, rev: item.rev },
            {
              onSuccess: () => {
                toast({ title: `Moved to ${status}` });
              },
              onError: (error) => {
                if (error instanceof ProviderError && error.code === 'stale_revision') {
                  toast({
                    variant: 'destructive',
                    title: 'Changed on disk',
                    description: `${item.id} was modified elsewhere. The item has been reloaded — review it and try again.`,
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
        {item.status === undefined ? <option value="">No status</option> : null}
        {(project?.statuses ?? []).map((status) => (
          <option key={status.id} value={status.id}>
            {status.name}
          </option>
        ))}
      </Select>
      {!canWrite ? (
        <span className="text-xs text-muted-foreground">Read-only workspace</span>
      ) : null}
    </div>
  );
}

function CommentsPanel({ item, projectKey }: { item: Item; projectKey: string }) {
  const provider = useProvider();
  const comments = useComments(projectKey, item.id);
  const addComment = useAddComment(projectKey);
  const { toast } = useToast();
  const [draft, setDraft] = useState('');
  const canWrite = provider.capabilities.write;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Comments</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {comments.isPending ? <p className="text-sm text-muted-foreground">Loading…</p> : null}
        {comments.isSuccess && comments.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">No comments yet.</p>
        ) : null}
        <ul aria-label="Comment thread" className="space-y-3">
          {(comments.data ?? []).map((comment) => (
            <li key={comment.path} className="rounded-md border border-border p-3">
              <p className="text-xs text-muted-foreground">
                <strong className="text-foreground">{comment.author}</strong>{' '}
                {formatDate(comment.created)}
              </p>
              <p className="mt-1 whitespace-pre-wrap text-sm">{comment.body}</p>
            </li>
          ))}
        </ul>

        <div className="space-y-2">
          <label
            htmlFor="comment-draft"
            className="text-xs uppercase tracking-wide text-muted-foreground"
          >
            Add a comment
          </label>
          <textarea
            id="comment-draft"
            rows={3}
            value={draft}
            disabled={!canWrite}
            placeholder={canWrite ? 'Write a comment…' : 'This workspace is read-only'}
            onChange={(event) => {
              setDraft(event.target.value);
            }}
            className="w-full rounded-md border border-input bg-background p-2 text-sm shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
          />
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              disabled={!canWrite || draft.trim().length === 0 || addComment.isPending}
              onClick={() => {
                addComment.mutate(
                  { id: item.id, body: draft.trim() },
                  {
                    onSuccess: () => {
                      setDraft('');
                    },
                    onError: (error) => {
                      toast({
                        variant: 'destructive',
                        title: 'The comment was not saved',
                        description: error.message,
                      });
                    },
                  },
                );
              }}
            >
              Post comment
            </Button>
            {!canWrite ? (
              <p className="text-xs text-muted-foreground">
                Open the folder with write access, or run the companion, to comment.
              </p>
            ) : null}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

function ItemDetailView() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';
  const id = params.id ?? '';

  useBacklogEvents(projectKey);

  const projectQuery = useProject(projectKey);
  const project = projectQuery.data;
  const itemQuery = useItem(projectKey, id);
  const item = itemQuery.data;
  const childrenQuery = useChildren(projectKey, id);
  const parentQuery = useItem(projectKey, item?.parent ?? '');
  const grandParentQuery = useItem(projectKey, parentQuery.data?.parent ?? '');

  if (itemQuery.isPending) {
    return <p className="py-8 text-center text-sm text-muted-foreground">Loading {id}…</p>;
  }

  if (itemQuery.isError || !item) {
    const notFound =
      itemQuery.error instanceof ProviderError && itemQuery.error.code === 'not_found';
    return (
      <Card>
        <CardHeader>
          <CardTitle>{notFound ? `${id} does not exist` : `${id} could not be read`}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm text-muted-foreground">
          <p>
            {notFound
              ? 'The id may have been renumbered, or the file lives in another project.'
              : (itemQuery.error?.message ?? 'Unknown error')}
          </p>
          <Link
            to="/p/$project/items"
            params={{ project: projectKey }}
            className="text-accent underline-offset-4 hover:underline"
          >
            Back to the item list
          </Link>
        </CardContent>
      </Card>
    );
  }

  const acceptance = acceptanceProgress(item.body);
  const children = childrenQuery.data ?? [];
  const custom = Object.entries(item.custom ?? {});

  return (
    <div className="space-y-6">
      <nav aria-label="Breadcrumb">
        <ol className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
          <li>
            <Link
              to="/p/$project/items"
              params={{ project: projectKey }}
              className="hover:underline"
            >
              {projectKey}
            </Link>
          </li>
          {grandParentQuery.data ? (
            <li className="flex items-center gap-1">
              <span aria-hidden="true">/</span>
              <ItemLink project={projectKey} id={grandParentQuery.data.id} className="font-sans">
                {grandParentQuery.data.title}
              </ItemLink>
            </li>
          ) : null}
          {parentQuery.data ? (
            <li className="flex items-center gap-1">
              <span aria-hidden="true">/</span>
              <ItemLink project={projectKey} id={parentQuery.data.id} className="font-sans">
                {parentQuery.data.title}
              </ItemLink>
            </li>
          ) : null}
          <li className="flex items-center gap-1">
            <span aria-hidden="true">/</span>
            <span className="text-foreground">{item.id}</span>
          </li>
        </ol>
      </nav>

      <header className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm text-muted-foreground">{item.id}</span>
          <TypeBadge type={item.type} />
          <StatusBadge status={item.status} project={project} />
          <PriorityBadge priority={item.priority} />
        </div>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">{item.title}</h1>
          <div className="flex items-center gap-2">
            <StatusPicker item={item} project={project} projectKey={projectKey} />
            <FeatureLink
              to="/p/$project/items/$id/edit"
              params={{ project: projectKey, id: item.id }}
              className="inline-flex h-9 items-center rounded-md border border-input px-4 text-sm font-medium hover:bg-secondary"
            >
              Edit
            </FeatureLink>
          </div>
        </div>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Front matter</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <Field label={typeName(item.type) === 'Task' ? 'Story' : 'Parent'}>
              {item.parent ? <ItemLink project={projectKey} id={item.parent} /> : <Dash />}
            </Field>
            <Field label="Milestone">
              {item.milestone ? <ItemLink project={projectKey} id={item.milestone} /> : <Dash />}
            </Field>
            <Field label="Assignees">
              {(item.assignees ?? []).length > 0 ? (item.assignees ?? []).join(', ') : <Dash />}
            </Field>
            <Field label="Labels">
              {(item.labels ?? []).length > 0 ? (
                <span className="flex flex-wrap gap-1">
                  {(item.labels ?? []).map((label) => (
                    <LabelChip
                      key={label}
                      label={label}
                      color={project?.labels.find((entry) => entry.name === label)?.color}
                    />
                  ))}
                </span>
              ) : (
                <Dash />
              )}
            </Field>
            <Field label="Estimate">{item.estimate ?? <Dash />}</Field>
            <Field label="Effort / spent">
              {item.effort === undefined && item.spent === undefined ? (
                <Dash />
              ) : (
                `${item.effort ?? 0}h planned / ${item.spent ?? 0}h spent`
              )}
            </Field>
            <Field label="Created">{formatDate(item.created)}</Field>
            <Field label="Updated">{formatDate(item.updated)}</Field>
            <Field label="Due">{item.due ? formatDate(item.due) : <Dash />}</Field>
            {item.started ? <Field label="Started">{formatDate(item.started)}</Field> : null}
            {item.closed ? <Field label="Closed">{formatDate(item.closed)}</Field> : null}
            <Field label="Path">
              <code className="break-all font-mono text-xs">{item.path}</code>
            </Field>
            <div className="sm:col-span-2 lg:col-span-3">
              <dt className="text-xs uppercase tracking-wide text-muted-foreground">Links</dt>
              <dd className="pt-1 text-sm">
                <LinksPanel project={projectKey} links={item.links ?? []} />
              </dd>
            </div>
            {custom.length > 0 ? (
              <div className="sm:col-span-2 lg:col-span-3">
                <dt className="text-xs uppercase tracking-wide text-muted-foreground">
                  Custom fields
                </dt>
                <dd className="flex flex-wrap gap-2 pt-1 text-sm">
                  {custom.map(([key, value]) => (
                    <Badge key={key} variant="outline">
                      {key}: {String(value)}
                    </Badge>
                  ))}
                </dd>
              </div>
            ) : null}
          </dl>
        </CardContent>
      </Card>

      {acceptance.total > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>Acceptance criteria</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <p className="text-sm">
              {acceptance.checked} of {acceptance.total} checked
            </p>
            <Progress
              value={acceptance.checked}
              max={acceptance.total}
              label={`Acceptance criteria: ${acceptance.checked} of ${acceptance.total} checked`}
            />
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Description</CardTitle>
        </CardHeader>
        <CardContent>
          <ItemBody
            body={item.body}
            path={item.path}
            project={projectKey}
            cacheKey={`${item.path}@${item.rev}`}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>
            {item.type === 'epic' ? 'Stories' : item.type === 'story' ? 'Tasks' : 'Children'}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {childrenQuery.isPending ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : children.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing is parented to this item yet.</p>
          ) : (
            <ul aria-label="Child items" className="divide-y divide-border">
              {children.map((child) => (
                <li key={child.id} className="flex flex-wrap items-center gap-2 py-2">
                  <ItemLink project={projectKey} id={child.id} />
                  <TypeBadge type={child.type} />
                  <span className="flex-1 text-sm">{child.title}</span>
                  <StatusBadge status={child.status} project={project} />
                  <PriorityBadge priority={child.priority} />
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <CommentsPanel item={item} projectKey={projectKey} />
    </div>
  );
}

/** Item read view (`/p/$project/items/$id`). */
export function ItemDetail() {
  return (
    <TooltipProvider delayDuration={200}>
      <ToastProvider>
        <ItemDetailView />
      </ToastProvider>
    </TooltipProvider>
  );
}
