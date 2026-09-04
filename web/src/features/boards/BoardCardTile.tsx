import { useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { Link } from '@tanstack/react-router';
import { CloudOff, GripVertical } from 'lucide-react';

import type { BoardCard, ProjectSummary } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { LabelChip, PriorityBadge } from '@/features/backlog/Badges';
import { bareItemId } from '@/features/backlog/item-meta';
import { cn } from '@/lib/cn';

export type BoardCardTileProps = {
  card: BoardCard;
  /** The project the card belongs to, when the workspace has it open. */
  project: ProjectSummary | undefined;
  /** Fields the board asks for; empty shows everything the card carries. */
  show: string[];
  /** Rendered inside the card: the "Move to…" keyboard alternative. */
  actions?: React.ReactNode;
  /** Drag is off for a remote card and in a read-only workspace. */
  draggable: boolean;
};

/** Whether a card field is shown, given the board's `card.show` list. */
function shows(fields: string[], field: string): boolean {
  return fields.length === 0 || fields.includes(field);
}

/**
 * One card of a board.
 *
 * A card whose project nobody cloned is muted, badged "remote" and cannot be
 * dragged; its tooltip says how to make it editable (docs/04 §7). Everything
 * else is a normal card: id, title, assignees, labels, priority and estimate.
 */
export function BoardCardTile({ card, project, show, actions, draggable }: BoardCardTileProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: card.ref,
    disabled: !draggable,
    data: { type: 'card', ref: card.ref },
  });

  const style = {
    transform: CSS.Translate.toString(transform),
    transition,
  };

  return (
    <li
      ref={setNodeRef}
      style={style}
      data-ref={card.ref}
      data-remote={card.remote ? 'true' : undefined}
      className={cn(
        'rounded-md border border-border bg-card p-2 text-sm shadow-sm',
        card.remote && 'border-dashed bg-muted/40 text-muted-foreground',
        isDragging && 'opacity-50',
      )}
    >
      <div className="flex items-start gap-2">
        {draggable ? (
          <button
            type="button"
            aria-label={`Drag ${card.item}`}
            className="mt-0.5 cursor-grab rounded text-muted-foreground hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
            {...attributes}
            {...listeners}
          >
            <GripVertical aria-hidden="true" className="h-4 w-4" />
          </button>
        ) : (
          <span
            className="mt-0.5 text-muted-foreground"
            title={card.reason ?? 'This card cannot be moved from here.'}
          >
            <CloudOff aria-hidden="true" className="h-4 w-4" />
          </span>
        )}

        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-1">
            {card.remote || !project ? (
              <span className="font-mono text-xs text-muted-foreground">{card.item}</span>
            ) : (
              <Link
                to="/p/$project/items/$id"
                params={{ project: card.project, id: bareItemId(card.item) }}
                className="font-mono text-xs text-accent underline-offset-4 hover:underline"
              >
                {card.item}
              </Link>
            )}
            {shows(show, 'project') ? (
              <Badge variant="outline" size="sm" className="font-normal">
                {card.project}
              </Badge>
            ) : null}
            {card.remote ? (
              <Badge variant="outline" size="sm" className="font-normal">
                remote
              </Badge>
            ) : null}
            {!card.declared ? (
              <Badge variant="destructive" size="sm" className="font-normal">
                unknown project
              </Badge>
            ) : null}
          </div>

          <p className="font-medium leading-snug">
            {card.title ?? <span className="italic">Title unavailable until the repo is cloned</span>}
          </p>

          {card.remote ? (
            <p className="text-xs text-muted-foreground">{card.reason}</p>
          ) : (
            <div className="flex flex-wrap items-center gap-1">
              {shows(show, 'priority') || shows(show, 'key') ? (
                <PriorityBadge priority={card.priority} />
              ) : null}
              {shows(show, 'estimate') && card.estimate !== undefined ? (
                <Badge variant="outline" size="sm" className="font-normal">
                  {card.estimate} pts
                </Badge>
              ) : null}
              {shows(show, 'assignee')
                ? (card.assignees ?? []).map((handle) => (
                    <Badge key={handle} variant="default" size="sm" className="font-normal">
                      {handle}
                    </Badge>
                  ))
                : null}
              {shows(show, 'labels')
                ? (card.labels ?? []).map((label) => (
                    <LabelChip
                      key={label}
                      label={label}
                      color={project?.labels.find((l) => l.name === label)?.color}
                    />
                  ))
                : null}
              {shows(show, 'due') && card.due ? (
                <Badge variant="outline" size="sm" className="font-normal">
                  due {card.due}
                </Badge>
              ) : null}
            </div>
          )}

          {actions}
        </div>
      </div>
    </li>
  );
}
