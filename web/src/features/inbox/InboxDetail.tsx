import { Link } from '@tanstack/react-router';

import type { Item, ProjectSummary } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { ItemLink, LabelChip, PriorityBadge, TypeBadge } from '@/features/backlog/Badges';
import { formatDate } from '@/features/backlog/item-meta';
import { ItemBody } from '@/features/backlog/ItemBody';
import { useComments } from '@/features/backlog/queries';
import { effectiveInboxStatus } from '@/features/inbox/inbox-state';

export type InboxDetailProps = {
  item: Item;
  projectKey: string;
  project: ProjectSummary | undefined;
  today: string;
};

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-0.5">
      <dt className="section-label">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

/**
 * The submission under triage: what it says, where it came from, and the thread
 * it already carries.
 *
 * The thread is read-only here, and `CommentsPanel` is deliberately *not*
 * exported from `features/backlog/ItemDetail.tsx` so that it stays that way.
 * A triage pane answers one question — does this belong in the backlog — and
 * accepting opens the item itself, which is where the conversation about it
 * belongs. The pass is also driven from the keyboard, and `useKeyboardTriage`
 * correctly ignores `j`, `k`, `a`, `r` and `s` while the focus is in a field:
 * a composer inside the pane would therefore not steal those keys, it would
 * quietly turn them off for as long as someone is typing, in the one place
 * where moving through the queue is the whole job.
 */
export function InboxDetail({ item, projectKey, project, today }: InboxDetailProps) {
  const comments = useComments(projectKey, item.id);
  const state = effectiveInboxStatus(item.inbox, today);
  const labels = item.labels ?? [];

  return (
    <div className="space-y-5">
      <header className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-xs text-muted-foreground">{item.id}</span>
          <TypeBadge type={item.type} />
          <PriorityBadge priority={item.priority} />
          {state === 'snoozed' && item.inbox?.snoozedUntil ? (
            <Badge variant="warning">Back on {formatDate(item.inbox.snoozedUntil)}</Badge>
          ) : null}
          {state === 'duplicate' && item.inbox?.duplicateOf ? (
            <Badge variant="outline">
              Duplicate of <ItemLink project={projectKey} id={item.inbox.duplicateOf} />
            </Badge>
          ) : null}
        </div>
        <h2 data-inbox-title className="page-title">
          {item.title}
        </h2>
        {labels.length > 0 ? (
          <ul className="flex flex-wrap gap-1.5">
            {labels.map((label) => (
              <li key={label}>
                <LabelChip
                  label={label}
                  color={project?.labels.find((entry) => entry.name === label)?.color}
                />
              </li>
            ))}
          </ul>
        ) : null}
      </header>

      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Meta label="Arrived via">{item.inbox?.source ?? 'unknown'}</Meta>
        <Meta label="Received">{formatDate(item.inbox?.received ?? item.created)}</Meta>
        <Meta label="Submitted by">{item.author ?? '—'}</Meta>
        <Meta label="Open item">
          <Link
            to="/p/$project/items/$id"
            params={{ project: projectKey, id: item.id }}
            className="text-accent underline-offset-4 hover:underline"
          >
            Full view
          </Link>
        </Meta>
      </dl>

      <section aria-label="Submission body" className="space-y-2">
        {/* Bodies are written by whoever submitted them, so they keep going
            through the shared Markdown pipeline and its sanitizer. */}
        <ItemBody
          body={item.body}
          path={item.path}
          project={projectKey}
          cacheKey={`${item.path}@${item.rev}`}
        />
      </section>

      <Card>
        <CardHeader>
          <CardTitle>Comments</CardTitle>
        </CardHeader>
        <CardContent>
          {comments.isPending ? <p className="text-sm text-muted-foreground">Loading…</p> : null}
          {comments.isSuccess && comments.data.length === 0 ? (
            <p className="empty-state">No comments yet.</p>
          ) : null}
          <ul aria-label="Comment thread" className="space-y-3">
            {(comments.data ?? []).map((comment) => (
              <li key={comment.path} className="rounded-md border border-border p-3">
                <p className="text-xs text-muted-foreground">
                  <strong className="text-foreground">
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
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}
