import { Link } from '@tanstack/react-router';
import type { CSSProperties, ReactNode } from 'react';

import type { ItemType, Priority, ProjectSummary } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import {
  bareItemId,
  priorityBadgeClass,
  statusBadgeClass,
  statusCategory,
  statusName,
  typeName,
} from '@/features/backlog/item-meta';
import { cn } from '@/lib/cn';

export function StatusBadge({
  status,
  project,
  className,
}: {
  status: string | undefined;
  project: ProjectSummary | undefined;
  className?: string;
}) {
  const category = statusCategory(project, status);
  return (
    <Badge
      variant="outline"
      className={cn(statusBadgeClass(category), className)}
      data-category={category}
      data-status={status ?? ''}
    >
      {statusName(project, status)}
    </Badge>
  );
}

export function PriorityBadge({
  priority,
  className,
}: {
  priority: Priority | undefined;
  className?: string;
}) {
  if (!priority) return <span className="text-muted-foreground">—</span>;
  return (
    <Badge variant="outline" className={cn(priorityBadgeClass(priority), className)}>
      {priority}
    </Badge>
  );
}

export function TypeBadge({ type, className }: { type: ItemType; className?: string }) {
  return (
    <Badge variant="outline" className={cn('font-normal', className)}>
      {typeName(type)}
    </Badge>
  );
}

/**
 * A label in the colour `project.yaml` gives it. The colour is repository data,
 * not a token, so it travels as `--label-colour` and `.label-chip` in
 * `index.css` decides how to use it: the high-contrast and colour-blind palettes
 * keep it on the border only and set the text in the foreground colour, so a
 * label never has to be told apart by an arbitrary hue (GIT-US-0206).
 */
export function LabelChip({ label, color }: { label: string; color?: string | undefined }) {
  return (
    <Badge
      variant="outline"
      size="sm"
      className="label-chip font-normal"
      style={color ? ({ '--label-colour': color } as CSSProperties) : undefined}
    >
      {label}
    </Badge>
  );
}

/** Navigable reference to another item; renders plain text for an empty id. */
export function ItemLink({
  project,
  id,
  children,
  className,
  rowLink = false,
}: {
  project: string;
  id: string | undefined;
  children?: ReactNode;
  className?: string;
  /** Marks the link the item table's j/k navigation walks. */
  rowLink?: boolean;
}) {
  if (!id) return <span className="text-muted-foreground">—</span>;
  return (
    <Link
      to="/p/$project/items/$id"
      params={{ project, id: bareItemId(id) }}
      data-row-link={rowLink ? 'true' : undefined}
      className={cn('font-mono text-accent underline-offset-4 hover:underline', className)}
    >
      {children ?? id}
    </Link>
  );
}
