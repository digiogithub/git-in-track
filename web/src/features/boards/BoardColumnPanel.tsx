import { useDroppable } from '@dnd-kit/core';
import { SortableContext, verticalListSortingStrategy } from '@dnd-kit/sortable';

import type { BoardCard, BoardColumnView, ProjectSummary } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { Select } from '@/components/ui/select';
import { BoardCardTile } from '@/features/boards/BoardCardTile';
import { cn } from '@/lib/cn';

export type BoardColumnPanelProps = {
  column: BoardColumnView;
  /** The cards left after the filter bar; may be shorter than `column.cards`. */
  cards: BoardCard[];
  columns: BoardColumnView[];
  projects: Map<string, ProjectSummary>;
  show: string[];
  canWrite: boolean;
  /** The keyboard alternative to dragging: move a card to another column. */
  onMoveTo: (ref: string, toColumn: string) => void;
};

/**
 * One column: a header carrying the live WIP count, and the cards below it.
 *
 * A column over its limit colours its header and badges the count. The limit is
 * advisory (docs/04 R-COL-5): it never hides a card, and the confirmation that
 * lets a drop exceed it lives in the board, not here.
 */
export function BoardColumnPanel({
  column,
  cards,
  columns,
  projects,
  show,
  canWrite,
  onMoveTo,
}: BoardColumnPanelProps) {
  const { setNodeRef, isOver } = useDroppable({ id: column.id, data: { type: 'column' } });
  const limited = column.wip !== undefined && column.wip > 0;
  const wouldExceed = limited && column.cards.length >= (column.wip ?? 0);

  return (
    <section
      aria-labelledby={`column-${column.id}`}
      data-column={column.id}
      data-exceeded={column.exceeded ? 'true' : undefined}
      className="flex w-72 shrink-0 flex-col gap-2"
    >
      <header
        className={cn(
          'flex items-center justify-between rounded-md border border-border px-2 py-1',
          column.exceeded && 'border-destructive bg-destructive/10',
        )}
        style={column.color && !column.exceeded ? { borderColor: column.color } : undefined}
      >
        <h3 id={`column-${column.id}`} className="text-sm font-semibold">
          {column.name}
        </h3>
        <Badge
          variant={column.exceeded ? 'destructive' : 'outline'}
          size="sm"
          className="font-normal"
          title={
            limited
              ? `${column.cards.length} of a WIP limit of ${column.wip}`
              : `${column.cards.length} cards, no WIP limit`
          }
        >
          {column.cards.length}
          {limited ? ` / ${column.wip}` : ''}
        </Badge>
      </header>

      {column.exceeded ? (
        <p role="status" className="text-xs text-destructive">
          {column.name} is over its WIP limit of {column.wip}. Finish something before starting more.
        </p>
      ) : null}

      <ul
        ref={setNodeRef}
        data-drop-target={column.id}
        aria-label={`${column.name} cards`}
        className={cn(
          'flex min-h-24 flex-col gap-2 rounded-md border border-dashed border-transparent p-1',
          isOver && !wouldExceed && 'border-accent bg-accent/5',
          isOver && wouldExceed && 'border-destructive bg-destructive/5',
        )}
      >
        <SortableContext
          items={cards.map((card) => card.ref)}
          strategy={verticalListSortingStrategy}
        >
          {cards.map((card) => (
            <BoardCardTile
              key={card.ref}
              card={card}
              project={projects.get(card.project)}
              show={show}
              draggable={canWrite && !card.remote}
              actions={
                canWrite && !card.remote ? (
                  <label className="mt-1 flex items-center gap-1 text-xs text-muted-foreground">
                    <span className="sr-only">Move {card.item} to another column</span>
                    <Select
                      aria-label={`Move ${card.item} to`}
                      value=""
                      className="h-7 text-xs"
                      onChange={(event) => {
                        if (event.target.value) onMoveTo(card.ref, event.target.value);
                      }}
                    >
                      <option value="">Move to…</option>
                      {columns
                        .filter((candidate) => candidate.id !== column.id)
                        .map((candidate) => (
                          <option key={candidate.id} value={candidate.id}>
                            {candidate.name}
                          </option>
                        ))}
                    </Select>
                  </label>
                ) : null
              }
            />
          ))}
        </SortableContext>

        {cards.length === 0 ? (
          <li className="rounded-md border border-dashed border-border p-3 text-center text-xs text-muted-foreground">
            No cards
          </li>
        ) : null}
      </ul>
    </section>
  );
}
