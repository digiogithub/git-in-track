import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { CloudCheck, CloudOff, CloudUpload, MessageSquarePlus, TriangleAlert } from 'lucide-react';
import { useState, type ReactNode } from 'react';

import type { Comment, Item, ProjectSummary, YouTrackPushComments } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { fieldClasses } from '@/components/ui/field';
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
import { hasYoutrackRef, useCommentSyncState, youtrackRef } from '@/features/backlog/comment-sync';
import { DeleteItemDialog } from '@/features/backlog/DeleteItemDialog';
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
import { NewItemLink } from '@/features/backlog/NewItemLink';
import {
  useAddComment,
  useBacklogEvents,
  useChildren,
  useComments,
  useCommentPushEvents,
  useDeleteItem,
  useItem,
  useMoveItem,
  useProject,
  usePushCommentToYoutrack,
  useToggleTask,
  useUnlinkExternal,
} from '@/features/backlog/queries';
import { useFeedbackDraft, useFeedbackPushPreference } from '@/features/feedback/feedback-store';
import { FeedbackPanel } from '@/features/feedback/FeedbackPanel';
import { FeedbackSelection } from '@/features/feedback/FeedbackSelection';
import { formatFeedbackComment } from '@/features/feedback/format';
import { youtrackMessage } from '@/features/settings/youtrack-messages';
import { isYouTrackLinked, useYouTrackSettings } from '@/features/settings/youtrack-queries';

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

/**
 * The YouTrack state of one comment, and — in `manual` mode — the action that
 * changes it.
 *
 * The badge is always rendered once the surface is gated in, including for a
 * comment nobody has sent: "not sent" is a state a reviewer has to be able to
 * see, and a row that showed nothing would read as "already handled". In `auto`
 * mode the action disappears and the badge stays, because every comment is
 * pushed anyway and a button that changed nothing would be a lie.
 */
function CommentYouTrackState({
  comment,
  itemId,
  projectKey,
  mode,
}: {
  comment: Comment;
  itemId: string;
  projectKey: string;
  mode: YouTrackPushComments;
}) {
  const sync = useCommentSyncState(comment);
  const push = usePushCommentToYoutrack(projectKey);
  const { toast } = useToast();

  const send = () => {
    push.mutate(
      { id: itemId, commentPath: comment.path },
      {
        onSuccess: (result) => {
          const already = result.skipped.length > 0 && result.pushed.length === 0;
          toast({
            title: already ? 'That comment is already on the issue' : 'Queued for YouTrack',
            description: already
              ? 'It carries a YouTrack reference already, so nothing was sent twice.'
              : 'The push runs in the background; the comment will say when it has arrived.',
          });
        },
        onError: (error) => {
          toast({
            variant: 'destructive',
            title: 'The comment was not queued',
            description: youtrackMessage(error),
          });
        },
      },
    );
  };

  const queueing = push.isPending;
  const showAction = mode !== 'auto' && (sync.state === 'unsent' || sync.state === 'failed');

  return (
    <div className="mt-2 flex flex-wrap items-center gap-2">
      {sync.state === 'unsent' ? (
        <Badge variant="outline">
          <CloudOff aria-hidden="true" />
          Not sent to YouTrack
        </Badge>
      ) : null}
      {sync.state === 'pending' || (queueing && sync.state !== 'sent') ? (
        <Badge variant="info">
          <CloudUpload aria-hidden="true" />
          Sending to YouTrack…
        </Badge>
      ) : null}
      {sync.state === 'sent' ? (
        <Badge variant="success">
          <CloudCheck aria-hidden="true" />
          {sync.url === undefined ? (
            <>Sent as {sync.id}</>
          ) : (
            <>
              Sent as{' '}
              <a
                href={sync.url}
                target="_blank"
                rel="noreferrer"
                className="underline underline-offset-2"
              >
                {sync.id}
              </a>
            </>
          )}
        </Badge>
      ) : null}
      {sync.state === 'failed' ? (
        <Badge variant="destructive">
          <TriangleAlert aria-hidden="true" />
          Not sent: {sync.error}
        </Badge>
      ) : null}
      {showAction ? (
        <Button size="sm" variant="ghost" disabled={queueing} onClick={send}>
          {sync.state === 'failed' ? 'Retry' : 'Send to YouTrack'}
        </Button>
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

  // Three separate facts, and all three have to hold: this runtime can reach
  // YouTrack, this project is connected to a YouTrack project, and this item
  // actually mirrors an issue. Without the third there is no issue to post to.
  const youtrack = useYouTrackSettings(projectKey);
  const pushable =
    provider.capabilities.youtrack &&
    isYouTrackLinked(youtrack.data) &&
    hasYoutrackRef(item.external);
  const pushMode: YouTrackPushComments = youtrack.data?.pushComments === 'auto' ? 'auto' : 'manual';
  useCommentPushEvents(projectKey, item.id);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Comments</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {comments.isPending ? <p className="text-sm text-muted-foreground">Loading…</p> : null}
        {comments.isError ? (
          <p role="alert" className="text-sm text-destructive">
            The comments could not be read: {comments.error.message}
          </p>
        ) : null}
        {comments.isSuccess && comments.data.length === 0 ? (
          <p className="empty-state">No comments yet.</p>
        ) : null}
        <ul aria-label="Comment thread" className="space-y-3">
          {(comments.data ?? []).map((comment) => (
            <li key={comment.path} className="rounded-md border border-border p-3">
              <p className="text-xs text-muted-foreground">
                <strong
                  className="text-foreground"
                  {...(comment.authorEmail ? { title: comment.authorEmail } : {})}
                >
                  {comment.authorName || comment.author}
                </strong>{' '}
                {formatDate(comment.created)}
              </p>
              <div className="mt-1 text-sm">
                <ItemBody
                  body={comment.body}
                  path={comment.path}
                  project={projectKey}
                  cacheKey={`${comment.path}@${comment.rev}`}
                />
              </div>
              {pushable ? (
                <CommentYouTrackState
                  comment={comment}
                  itemId={item.id}
                  projectKey={projectKey}
                  mode={pushMode}
                />
              ) : null}
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
            className={`${fieldClasses} p-2`}
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
  const navigate = useNavigate();
  const provider = useProvider();
  const projectKey = params.project ?? '';
  const id = params.id ?? '';
  const { toast } = useToast();
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  useBacklogEvents(projectKey);

  const projectQuery = useProject(projectKey);
  const project = projectQuery.data;
  const itemQuery = useItem(projectKey, id);
  const item = itemQuery.data;
  const childrenQuery = useChildren(projectKey, id);
  const parentQuery = useItem(projectKey, item?.parent ?? '');
  const grandParentQuery = useItem(projectKey, parentQuery.data?.parent ?? '');
  const toggleTask = useToggleTask(projectKey);
  const deleteItem = useDeleteItem(projectKey);
  const unlinkExternal = useUnlinkExternal(projectKey);
  const [confirmingUnlink, setConfirmingUnlink] = useState(false);
  const feedback = useFeedbackDraft({ kind: 'item', project: projectKey, ref: id });
  const saveFeedback = useAddComment(projectKey);
  const [feedbackError, setFeedbackError] = useState<string | null>(null);
  const feedbackPush = useFeedbackPushPreference(projectKey);
  const pushFeedback = usePushCommentToYoutrack(projectKey);
  const youtrack = useYouTrackSettings(projectKey);

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
  // What this item can parent: a story under an epic, a task under a story.
  const childType =
    item.type === 'epic' ? 'story' : item.type === 'story' ? ('task' as const) : undefined;
  const custom = Object.entries(item.custom ?? {});
  // The issue this item mirrors, when it mirrors one. It is what makes a
  // comment pushable and what a re-import updates instead of duplicating, so
  // the detail view says it out loud rather than leaving it in the file.
  const tracker = youtrackRef(item);
  const feedbackCount = feedback.draft.notes.length;
  const showFeedback = feedback.draft.active || feedbackCount > 0;
  // The same three facts the per-comment action is gated on: this note becomes
  // an ordinary comment, so it can only travel where a comment can.
  const canPushFeedback =
    provider.capabilities.youtrack &&
    isYouTrackLinked(youtrack.data) &&
    hasYoutrackRef(item.external);

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
          <h1 className="page-title">{item.title}</h1>
          <div className="flex items-center gap-2">
            <StatusPicker item={item} project={project} projectKey={projectKey} />
            <Button
              variant={feedback.draft.active ? 'secondary' : 'outline'}
              aria-pressed={feedback.draft.active}
              title="Select text in the description to attach feedback notes"
              onClick={() => feedback.setActive(!feedback.draft.active)}
            >
              <MessageSquarePlus className="size-4" aria-hidden="true" />
              Feedback{feedbackCount > 0 ? ` (${feedbackCount})` : ''}
            </Button>
            <FeatureLink
              to="/p/$project/items/$id/edit"
              params={{ project: projectKey, id: item.id }}
              className="inline-flex h-9 items-center rounded-md border border-input px-4 text-sm font-medium hover:bg-secondary"
            >
              Edit
            </FeatureLink>
            <Button
              variant="outline"
              disabled={!provider.capabilities.write || item.deleted === true}
              onClick={() => {
                setDeleteError(null);
                setConfirmingDelete(true);
              }}
            >
              Delete
            </Button>
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
            {tracker ? (
              <Field label="YouTrack">
                <span className="flex flex-wrap items-center gap-2">
                  {tracker.url === undefined || tracker.url === '' ? (
                    <code className="font-mono text-xs">{tracker.id}</code>
                  ) : (
                    <a
                      href={tracker.url}
                      target="_blank"
                      rel="noreferrer"
                      className="font-mono text-xs hover:underline"
                    >
                      {tracker.id}
                    </a>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={!provider.capabilities.write || unlinkExternal.isPending}
                    title="Forgets the issue this item mirrors; the issue itself is left alone"
                    onClick={() => {
                      setConfirmingUnlink(true);
                    }}
                  >
                    Unlink
                  </Button>
                </span>
              </Field>
            ) : null}
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
          <FeedbackSelection
            source={item.body}
            active={feedback.draft.active}
            notes={feedback.draft.notes}
            contentKey={`${item.path}@${item.rev}`}
            onEnable={() => feedback.setActive(true)}
            onAddNote={feedback.addNote}
          >
            <ItemBody
              body={item.body}
              path={item.path}
              project={projectKey}
              cacheKey={`${item.path}@${item.rev}`}
              sourceLines
              taskBusy={toggleTask.isPending}
              {...(provider.capabilities.write
                ? {
                    onToggleTask: (line: number, checked: boolean) => {
                      toggleTask.mutate(
                        { id: item.id, line, checked, rev: item.rev },
                        {
                          onError: (error) => {
                            const stale =
                              error instanceof ProviderError && error.code === 'stale_revision';
                            toast({
                              variant: 'destructive',
                              title: stale ? 'Changed on disk' : 'The checkbox was not saved',
                              description: stale
                                ? `${item.id} was modified elsewhere. It has been reloaded — try again.`
                                : error.message,
                            });
                          },
                        },
                      );
                    },
                  }
                : {})}
            />
          </FeedbackSelection>
        </CardContent>
      </Card>

      {showFeedback ? (
        <FeedbackPanel
          feedback={feedback}
          destination="comment"
          canWrite={provider.capabilities.write}
          saving={saveFeedback.isPending}
          error={feedbackError}
          {...(canPushFeedback
            ? {
                push: {
                  enabled: feedbackPush.enabled,
                  onChange: feedbackPush.setEnabled,
                },
              }
            : {})}
          onSave={() => {
            setFeedbackError(null);
            saveFeedback.mutate(
              { id: item.id, body: formatFeedbackComment(feedback.draft.notes) },
              {
                onSuccess: (comment) => {
                  feedback.clear();
                  toast({ title: 'Feedback saved as a comment' });
                  // The note is a comment like any other, so sending it on is
                  // the same push — asked for once, here, rather than by
                  // hunting the new comment down in the thread afterwards.
                  if (!canPushFeedback || !feedbackPush.enabled) return;
                  pushFeedback.mutate(
                    { id: item.id, commentPath: comment.path },
                    {
                      onError: (error) => {
                        toast({
                          variant: 'destructive',
                          title: 'The feedback was saved, but not queued for YouTrack',
                          description: youtrackMessage(error),
                        });
                      },
                    },
                  );
                },
                onError: (error) => {
                  setFeedbackError(`The feedback was not saved: ${error.message}`);
                },
              },
            );
          }}
        />
      ) : null}

      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
          <CardTitle>
            {item.type === 'epic' ? 'Stories' : item.type === 'story' ? 'Tasks' : 'Children'}
          </CardTitle>
          {childType ? (
            <NewItemLink
              project={projectKey}
              type={childType}
              parent={item.id}
              label={childType === 'story' ? 'New story' : 'New task'}
            />
          ) : null}
        </CardHeader>
        <CardContent>
          {childrenQuery.isPending ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : children.length === 0 ? (
            <p className="empty-state">Nothing is parented to this item yet.</p>
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

      <Dialog open={confirmingUnlink} onOpenChange={setConfirmingUnlink}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Unlink {item.id} from YouTrack?</DialogTitle>
            <DialogDescription>
              The reference to {tracker?.id ?? 'the issue'} is removed from this item. Nothing is
              sent to YouTrack: the issue stays exactly where it is, and this project stays
              connected.
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            Afterwards, comments can no longer be sent from this item, and importing that issue
            again creates a new item instead of updating this one.
          </p>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                setConfirmingUnlink(false);
              }}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={unlinkExternal.isPending}
              onClick={() => {
                unlinkExternal.mutate(
                  { id: item.id, rev: item.rev, system: 'youtrack' },
                  {
                    onSuccess: () => {
                      setConfirmingUnlink(false);
                      toast({
                        title: `${item.id} was unlinked`,
                        description:
                          'Its YouTrack reference was removed. The issue was not touched.',
                      });
                    },
                    onError: (error) => {
                      toast({
                        variant: 'destructive',
                        title: 'The item was not unlinked',
                        description:
                          error instanceof ProviderError && error.code === 'stale_revision'
                            ? `${item.id} changed on disk since this page was loaded. Reload it and try again.`
                            : youtrackMessage(error),
                      });
                    },
                  },
                );
              }}
            >
              Unlink the item
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {confirmingDelete ? (
        <DeleteItemDialog
          item={item}
          projectKey={projectKey}
          busy={deleteItem.isPending}
          error={deleteError}
          onCancel={() => {
            setConfirmingDelete(false);
          }}
          onConfirm={() => {
            setDeleteError(null);
            deleteItem.mutate(
              { id: item.id, rev: item.rev },
              {
                onSuccess: () => {
                  setConfirmingDelete(false);
                  toast({ title: `${item.id} deleted` });
                  void navigate({ to: '/p/$project/items', params: { project: projectKey } });
                },
                onError: (error) => {
                  setDeleteError(
                    error instanceof ProviderError && error.code === 'stale_revision'
                      ? `${item.id} changed on disk since this page was loaded. Reload it and try again.`
                      : error.message,
                  );
                },
              },
            );
          }}
        />
      ) : null}
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
