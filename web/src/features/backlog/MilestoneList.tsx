import { Link, useParams } from '@tanstack/react-router';
import { useMemo } from 'react';

import type { Item } from '@/api/provider';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { ItemLink, StatusBadge } from '@/features/backlog/Badges';
import { formatDate, rollup } from '@/features/backlog/item-meta';
import { NewItemLink } from '@/features/backlog/NewItemLink';
import { useBacklogEvents, useItems, useProject } from '@/features/backlog/queries';

const PAGE_SIZE = 200;

function isOverdue(due: string | undefined, done: boolean): boolean {
  if (!due || done) return false;
  const date = new Date(due);
  return !Number.isNaN(date.getTime()) && date.getTime() < Date.now();
}

/** Milestones with due dates and progress (`/p/$project/milestones`). */
export function MilestoneList() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';

  useBacklogEvents(projectKey);

  const projectQuery = useProject(projectKey);
  const project = projectQuery.data;
  const itemsQuery = useItems(
    useMemo(
      () => ({ project: projectKey, limit: PAGE_SIZE, sort: 'id' as const, order: 'asc' as const }),
      [projectKey],
    ),
  );

  const items = useMemo(
    () => itemsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [itemsQuery.data],
  );

  const milestones = useMemo(() => {
    const list = items.filter((item) => item.type === 'milestone');
    return [...list].sort((a, b) => (a.due ?? '9999').localeCompare(b.due ?? '9999'));
  }, [items]);

  const membersOf = useMemo(() => {
    const map = new Map<string, Item[]>();
    for (const item of items) {
      if (!item.milestone) continue;
      map.set(item.milestone, [...(map.get(item.milestone) ?? []), item]);
    }
    return map;
  }, [items]);

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">Milestones</h1>
          <p className="text-sm text-muted-foreground">
            Delivery checkpoints of <strong>{projectKey}</strong>, earliest due date first.
          </p>
        </div>
        <NewItemLink project={projectKey} type="milestone" label="New milestone" variant="bar" />
      </header>

      {itemsQuery.isPending ? (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading milestones…</p>
      ) : null}

      {itemsQuery.isError ? (
        <Card>
          <CardHeader>
            <CardTitle>The index could not be read</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            {itemsQuery.error.message}
          </CardContent>
        </Card>
      ) : null}

      {itemsQuery.isSuccess && milestones.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>No milestones yet</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            A milestone is an item of type <code className="font-mono">milestone</code> with a{' '}
            <code className="font-mono">due</code> date. Items point at it through their{' '}
            <code className="font-mono">milestone</code> field.
          </CardContent>
        </Card>
      ) : null}

      <ul className="space-y-3">
        {milestones.map((milestone) => {
          const members = membersOf.get(milestone.id) ?? [];
          const summary = rollup(members, project);
          const overdue = isOverdue(milestone.due, summary.total > 0 && summary.percent === 100);
          return (
            <li key={milestone.id}>
              <Card>
                <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2 space-y-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <ItemLink project={projectKey} id={milestone.id} />
                    <CardTitle>{milestone.title}</CardTitle>
                    <StatusBadge status={milestone.status} project={project} />
                  </div>
                  <p
                    className={
                      overdue
                        ? 'text-sm font-medium text-destructive'
                        : 'text-sm text-muted-foreground'
                    }
                  >
                    {milestone.due ? `Due ${formatDate(milestone.due)}` : 'No due date'}
                    {overdue ? ' · overdue' : ''}
                  </p>
                </CardHeader>
                <CardContent className="space-y-2">
                  <Progress
                    value={summary.done}
                    max={Math.max(summary.total - summary.cancelled, 0)}
                    label={`${milestone.title}: ${summary.done} of ${summary.total} items done`}
                    indicatorClassName="bg-[hsl(var(--status-done))]"
                  />
                  <p className="text-sm text-muted-foreground">
                    {summary.done}/{summary.total} done · {summary.inProgress} in progress ·{' '}
                    {summary.todo} to do
                    {summary.points > 0 ? ` · ${summary.donePoints}/${summary.points} points` : ''}
                  </p>
                  <div className="flex flex-wrap items-center gap-3">
                    <Link
                      to="/p/$project/items"
                      params={{ project: projectKey }}
                      search={{ milestone: milestone.id }}
                      className="text-sm text-accent underline-offset-4 hover:underline"
                    >
                      See its items
                    </Link>
                    <NewItemLink
                      project={projectKey}
                      type="story"
                      milestone={milestone.id}
                      label="New story"
                    />
                  </div>
                </CardContent>
              </Card>
            </li>
          );
        })}
      </ul>

      {itemsQuery.hasNextPage ? (
        <Button
          variant="outline"
          size="sm"
          disabled={itemsQuery.isFetchingNextPage}
          onClick={() => {
            void itemsQuery.fetchNextPage();
          }}
        >
          {itemsQuery.isFetchingNextPage ? 'Loading…' : 'Load more items'}
        </Button>
      ) : null}
    </div>
  );
}
