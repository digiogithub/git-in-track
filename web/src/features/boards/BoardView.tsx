import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCorners,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from '@dnd-kit/core';
import { restrictToWindowEdges } from '@dnd-kit/modifiers';
import { sortableKeyboardCoordinates } from '@dnd-kit/sortable';
import { useParams } from '@tanstack/react-router';
import { useMemo, useState } from 'react';

import type { BoardCard, BoardView as BoardViewData, CardMove } from '@/api/provider';
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
import { Select } from '@/components/ui/select';
import { useToast } from '@/components/ui/toast';
import { useProjects } from '@/features/backlog/queries';
import { BoardColumnPanel } from '@/features/boards/BoardColumnPanel';
import { useBoard, useBoardEvents, useMoveCard } from '@/features/boards/queries';

/** A move waiting for the user to confirm that it may exceed a WIP limit. */
type PendingMove = { move: CardMove; column: string; limit: number };

/** Client-side narrowing on top of the filters the board file declares. */
type ColumnFilter = { project: string; label: string; assignee: string };

const emptyFilter: ColumnFilter = { project: '', label: '', assignee: '' };

function passes(card: BoardCard, filter: ColumnFilter): boolean {
  if (filter.project && card.project !== filter.project) return false;
  if (filter.label && !(card.labels ?? []).includes(filter.label)) return false;
  if (filter.assignee && !(card.assignees ?? []).includes(filter.assignee)) return false;
  return true;
}

/**
 * The Kanban board (docs/05-web-app.md §9, story GIT-US-0017).
 *
 * Columns and card order come from the board file in the team repository; card
 * content comes from the per-project indexes. Dropping a card in another column
 * writes the new status into the item's own repository and the new position
 * into the board's `order:` list, and nothing else.
 *
 * Every drag has a keyboard equivalent: dnd-kit's keyboard sensor moves a card
 * with the space bar and the arrow keys, and each card carries a "Move to…"
 * menu that does the same thing with one control. Both announce the result in
 * a live region.
 */
export function BoardView() {
  const { slug } = useParams({ from: '/boards/$slug' });
  return <BoardCanvas slug={slug} />;
}

/**
 * The board itself, addressed by slug. It is separate from the route component
 * so that a test renders a board without standing up the whole route tree.
 */
export function BoardCanvas({ slug }: { slug: string }) {
  const provider = useProvider();
  const board = useBoard(slug);
  const projectList = useProjects();
  const move = useMoveCard(slug);
  const { toast } = useToast();

  const [filter, setFilter] = useState<ColumnFilter>(emptyFilter);
  const [announcement, setAnnouncement] = useState('');
  const [pending, setPending] = useState<PendingMove | null>(null);
  const [dragging, setDragging] = useState<string | null>(null);

  useBoardEvents(slug);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const projects = useMemo(
    () => new Map((projectList.data ?? []).map((project) => [project.key, project])),
    [projectList.data],
  );

  const view = board.data;
  const facets = useMemo(() => collectFacets(view), [view]);

  if (board.isPending) {
    return <p className="text-sm text-muted-foreground">Loading the board…</p>;
  }
  if (board.isError || !view) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Board unavailable</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          {board.error instanceof Error ? board.error.message : `No board called ${slug}.`}
        </CardContent>
      </Card>
    );
  }

  const canWrite = provider.capabilities.write;

  /** Runs a move, turning a WIP refusal into a confirmation instead of a toast. */
  const submit = (request: CardMove, confirmed = false) => {
    const card = cardOf(view, request.ref);
    const target = view.columns.find((column) => column.id === request.toColumn);
    move.mutate(
      { ...request, ...(confirmed ? { force: true } : {}) },
      {
        onSuccess: (result) => {
          const column = result.board.columns.find((c) => c.id === result.move.toColumn);
          const position = (column?.cards.findIndex((c) => c.ref === request.ref) ?? 0) + 1;
          setAnnouncement(
            `Moved ${card?.item ?? request.ref} to ${column?.name ?? request.toColumn}, position ${position} of ${column?.cards.length ?? position}.`,
          );
        },
        onError: (error) => {
          if (error instanceof ProviderError && error.code === 'wip_limit_exceeded' && !confirmed) {
            setPending({
              move: request,
              column: target?.name ?? request.toColumn,
              limit: target?.wip ?? 0,
            });
            setAnnouncement(
              `${target?.name ?? request.toColumn} is at its WIP limit; the move needs confirmation.`,
            );
            return;
          }
          setAnnouncement(`The move was refused: ${error.message}`);
          toast({
            variant: 'destructive',
            title: `Could not move ${card?.item ?? request.ref}`,
            description: error.message,
          });
        },
      },
    );
  };

  const requestMove = (ref: string, toColumn: string, position: number) => {
    const card = cardOf(view, ref);
    if (!card) return;
    if (card.remote) {
      toast({
        variant: 'destructive',
        title: `${card.item} is read-only here`,
        description: card.reason ?? `Clone ${card.project} to move this card.`,
      });
      return;
    }
    submit({
      board: view.id,
      ref,
      toColumn,
      position,
      rev: view.rev,
      ...(card.rev === undefined ? {} : { itemRev: card.rev }),
    });
  };

  const onDragStart = (event: DragStartEvent) => setDragging(String(event.active.id));

  const onDragEnd = (event: DragEndEvent) => {
    setDragging(null);
    const { active, over } = event;
    if (!over) return;
    const ref = String(active.id);
    const target = targetOf(view, String(over.id));
    if (!target) return;
    requestMove(ref, target.column, target.position);
  };

  const unmapped = view.unmapped;

  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">{view.title}</h1>
        {view.description ? (
          <p className="text-sm text-muted-foreground">{view.description}</p>
        ) : null}
        <p className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
          <span>Projects:</span>
          {view.projects.map((key) => (
            <Badge key={key} variant="outline" size="sm" className="font-normal">
              {key}
            </Badge>
          ))}
          {(view.filters.types ?? []).length > 0 ? (
            <span>· types: {(view.filters.types ?? []).join(', ')}</span>
          ) : null}
          {(view.filters.labelsNone ?? []).length > 0 ? (
            <span>· without: {(view.filters.labelsNone ?? []).join(', ')}</span>
          ) : null}
        </p>
      </header>

      {!canWrite ? (
        <p className="text-sm text-muted-foreground">
          This workspace is read-only, so cards cannot be moved from here.
        </p>
      ) : null}

      {view.diagnostics.length > 0 ? (
        <ul role="alert" className="space-y-1 text-sm text-destructive">
          {view.diagnostics.map((diagnostic) => (
            <li key={`${diagnostic.code}-${diagnostic.message}`}>
              <code>{diagnostic.code}</code> {diagnostic.message}
            </li>
          ))}
        </ul>
      ) : null}

      <form
        aria-label="Board filters"
        className="flex flex-wrap items-end gap-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <label className="text-xs">
          <span className="mb-1 block text-muted-foreground">Project</span>
          <Select
            aria-label="Filter by project"
            value={filter.project}
            onChange={(event) => setFilter({ ...filter, project: event.target.value })}
            className="h-8 w-40 text-xs"
          >
            <option value="">All projects</option>
            {view.projects.map((key) => (
              <option key={key} value={key}>
                {key}
              </option>
            ))}
          </Select>
        </label>
        <label className="text-xs">
          <span className="mb-1 block text-muted-foreground">Label</span>
          <Select
            aria-label="Filter by label"
            value={filter.label}
            onChange={(event) => setFilter({ ...filter, label: event.target.value })}
            className="h-8 w-40 text-xs"
          >
            <option value="">All labels</option>
            {facets.labels.map((label) => (
              <option key={label} value={label}>
                {label}
              </option>
            ))}
          </Select>
        </label>
        <label className="text-xs">
          <span className="mb-1 block text-muted-foreground">Assignee</span>
          <Select
            aria-label="Filter by assignee"
            value={filter.assignee}
            onChange={(event) => setFilter({ ...filter, assignee: event.target.value })}
            className="h-8 w-40 text-xs"
          >
            <option value="">Everyone</option>
            {facets.assignees.map((handle) => (
              <option key={handle} value={handle}>
                {handle}
              </option>
            ))}
          </Select>
        </label>
        <Button type="button" variant="ghost" size="sm" onClick={() => setFilter(emptyFilter)}>
          Clear
        </Button>
      </form>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        modifiers={[restrictToWindowEdges]}
        onDragStart={onDragStart}
        onDragEnd={onDragEnd}
        onDragCancel={() => setDragging(null)}
        accessibility={{
          announcements: {
            onDragStart: ({ active }) => `Picked up ${active.id}.`,
            onDragOver: ({ active, over }) =>
              over ? `${active.id} is over ${over.id}.` : `${active.id} is over no column.`,
            onDragEnd: ({ active, over }) =>
              over ? `Dropped ${active.id} on ${over.id}.` : `${active.id} was returned.`,
            onDragCancel: ({ active }) => `Cancelled moving ${active.id}.`,
          },
        }}
      >
        <div className="flex gap-4 overflow-x-auto pb-2" data-dragging={dragging ?? undefined}>
          {view.columns.map((column) => (
            <BoardColumnPanel
              key={column.id}
              column={column}
              cards={column.cards.filter((card) => passes(card, filter))}
              columns={view.columns}
              projects={projects}
              show={view.card.show ?? []}
              canWrite={canWrite}
              onMoveTo={(ref, toColumn) => requestMove(ref, toColumn, 0)}
            />
          ))}
        </div>
      </DndContext>

      {unmapped.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">
              {unmapped.length} item{unmapped.length === 1 ? '' : 's'} hidden (unmapped status)
            </CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-1 text-sm">
              {unmapped.map((card) => (
                <li key={card.ref} className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-xs">{card.item}</span>
                  <span>{card.title}</span>
                  <Badge variant="outline" size="sm" className="font-normal">
                    {card.status}
                  </Badge>
                  <span className="text-xs text-muted-foreground">{card.reason}</span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      ) : null}

      <p aria-live="polite" role="status" aria-label="Board updates" className="sr-only">
        {announcement}
      </p>

      <Dialog open={pending !== null} onOpenChange={(open) => !open && setPending(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>WIP limit reached</DialogTitle>
            <DialogDescription>
              {pending
                ? `${pending.column} is at its WIP limit of ${pending.limit}. Limits are advisory: you can move the card anyway, but the column will be marked over its limit for everyone.`
                : ''}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setPending(null)}>
              Cancel
            </Button>
            <Button
              onClick={() => {
                if (pending) submit(pending.move, true);
                setPending(null);
              }}
            >
              Move anyway
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** The card a ref names, wherever it currently sits. */
function cardOf(view: BoardViewData, ref: string): BoardCard | undefined {
  return view.columns.flatMap((column) => column.cards).find((card) => card.ref === ref);
}

/**
 * Where a drop lands. dnd-kit reports either a column (an empty area) or the
 * card the pointer is over, which is what gives the position inside a column.
 */
function targetOf(
  view: BoardViewData,
  overId: string,
): { column: string; position: number } | undefined {
  const column = view.columns.find((c) => c.id === overId);
  if (column) return { column: column.id, position: column.cards.length };
  for (const candidate of view.columns) {
    const index = candidate.cards.findIndex((card) => card.ref === overId);
    if (index >= 0) return { column: candidate.id, position: index };
  }
  return undefined;
}

/** The labels and assignees the board currently shows, for the filter bar. */
function collectFacets(view: BoardViewData | undefined): {
  labels: string[];
  assignees: string[];
} {
  const labels = new Set<string>();
  const assignees = new Set<string>();
  for (const column of view?.columns ?? []) {
    for (const card of column.cards) {
      for (const label of card.labels ?? []) labels.add(label);
      for (const handle of card.assignees ?? []) assignees.add(handle);
    }
  }
  return {
    labels: [...labels].sort((a, b) => a.localeCompare(b)),
    assignees: [...assignees].sort((a, b) => a.localeCompare(b)),
  };
}
