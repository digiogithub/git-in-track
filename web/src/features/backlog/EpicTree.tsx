import { useParams } from '@tanstack/react-router';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useMemo, useState } from 'react';

import type { Item, ProjectSummary } from '@/api/provider';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { ItemLink, PriorityBadge, StatusBadge } from '@/features/backlog/Badges';
import { rollup, type CategoryRollup } from '@/features/backlog/item-meta';
import { useBacklogEvents, useItems, useProject } from '@/features/backlog/queries';

const TREE_PAGE_SIZE = 200;

function RollupBar({ summary, label }: { summary: CategoryRollup; label: string }) {
  return (
    <div className="w-56 shrink-0 space-y-1">
      <Progress
        value={summary.done}
        max={Math.max(summary.total - summary.cancelled, 0)}
        label={`${label}: ${summary.done} of ${summary.total} done`}
        indicatorClassName="bg-[hsl(var(--status-done))]"
      />
      <p className="text-xs text-muted-foreground">
        {summary.done}/{summary.total} done
        {summary.inProgress > 0 ? ` · ${summary.inProgress} in progress` : ''}
        {summary.points > 0 ? ` · ${summary.donePoints}/${summary.points} pts` : ''}
      </p>
    </div>
  );
}

function TreeNode({
  item,
  childrenOf,
  project,
  projectKey,
  depth,
}: {
  item: Item;
  childrenOf: Map<string, Item[]>;
  project: ProjectSummary | undefined;
  projectKey: string;
  depth: number;
}) {
  const [open, setOpen] = useState(depth === 0);
  const children = childrenOf.get(item.id) ?? [];
  const summary = rollup(children, project);

  return (
    <li>
      <div
        className="flex flex-wrap items-center gap-2 border-b border-border py-2"
        style={{ paddingLeft: `${depth * 1.25}rem` }}
      >
        {children.length > 0 ? (
          <button
            type="button"
            aria-expanded={open}
            aria-label={`${open ? 'Collapse' : 'Expand'} ${item.id}`}
            onClick={() => {
              setOpen((value) => !value);
            }}
            className="rounded p-0.5 text-muted-foreground hover:bg-secondary hover:text-foreground"
          >
            {open ? (
              <ChevronDown aria-hidden="true" className="h-4 w-4" />
            ) : (
              <ChevronRight aria-hidden="true" className="h-4 w-4" />
            )}
          </button>
        ) : (
          <span className="inline-block w-5" />
        )}
        <ItemLink project={projectKey} id={item.id} />
        <span className="flex-1 text-sm font-medium">{item.title}</span>
        <StatusBadge status={item.status} project={project} />
        <PriorityBadge priority={item.priority} />
        {children.length > 0 ? <RollupBar summary={summary} label={item.title} /> : null}
      </div>
      {open && children.length > 0 ? (
        <ul>
          {children.map((child) => (
            <TreeNode
              key={child.id}
              item={child}
              childrenOf={childrenOf}
              project={project}
              projectKey={projectKey}
              depth={depth + 1}
            />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

/** Epic → story → task tree with roll-ups (`/p/$project/epics`). */
export function EpicTree() {
  const params = useParams({ strict: false });
  const projectKey = params.project ?? '';

  useBacklogEvents(projectKey);

  const projectQuery = useProject(projectKey);
  const project = projectQuery.data;
  const itemsQuery = useItems(
    useMemo(
      () => ({
        project: projectKey,
        limit: TREE_PAGE_SIZE,
        sort: 'id' as const,
        order: 'asc' as const,
      }),
      [projectKey],
    ),
  );

  const items = useMemo(
    () => itemsQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [itemsQuery.data],
  );

  const { epics, childrenOf } = useMemo(() => {
    const byParent = new Map<string, Item[]>();
    for (const item of items) {
      if (!item.parent) continue;
      byParent.set(item.parent, [...(byParent.get(item.parent) ?? []), item]);
    }
    return { epics: items.filter((item) => item.type === 'epic'), childrenOf: byParent };
  }, [items]);

  const overall = rollup(
    items.filter((item) => item.type !== 'milestone' && item.type !== 'epic'),
    project,
  );

  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Epics</h1>
        <p className="text-sm text-muted-foreground">
          Every epic of <strong>{projectKey}</strong> with its stories and tasks.
        </p>
      </header>

      {itemsQuery.isPending ? (
        <p className="py-8 text-center text-sm text-muted-foreground">Loading the tree…</p>
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

      {itemsQuery.isSuccess && epics.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>No epics yet</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            An epic groups stories. Create one and set <code className="font-mono">parent</code> on
            the stories that belong to it.
          </CardContent>
        </Card>
      ) : null}

      {epics.length > 0 ? (
        <>
          <div className="flex flex-wrap items-center gap-3 rounded-md border border-border p-3">
            <p className="text-sm font-medium">Project progress</p>
            <RollupBar summary={overall} label="Project" />
          </div>
          <ul>
            {epics.map((epic) => (
              <TreeNode
                key={epic.id}
                item={epic}
                childrenOf={childrenOf}
                project={project}
                projectKey={projectKey}
                depth={0}
              />
            ))}
          </ul>
        </>
      ) : null}

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
