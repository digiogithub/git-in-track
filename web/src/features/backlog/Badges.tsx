import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';

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

export function LabelChip({ label, color }: { label: string; color?: string | undefined }) {
  return (
    <Badge
      variant="outline"
      size="sm"
      className="font-normal"
      style={color ? { borderColor: color, color } : undefined}
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
