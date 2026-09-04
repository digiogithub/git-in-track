/**
 * Presentation helpers shared by the backlog screens. All workflow knowledge
 * comes from `ProjectSummary` (i.e. from `project.yaml`); nothing here hardcodes
 * a status id.
 */

import type { Item, ItemType, Priority, ProjectSummary } from '@/api/provider';

/** A typed relation as stored in `links[]` (docs/03-data-model.md §12). */
export type ItemRelation = NonNullable<Item['links']>[number];
export type LinkKind = ItemRelation['kind'];

export type StatusCategory = 'todo' | 'in_progress' | 'done' | 'cancelled' | 'unknown';

/** The coarse workflow bucket of a status id, used for colour and roll-ups. */
export function statusCategory(
  project: ProjectSummary | undefined,
  status: string | undefined,
): StatusCategory {
  if (!status) return 'unknown';
  const found = project?.statuses.find((s) => s.id === status);
  switch (found?.category) {
    case 'todo':
    case 'in_progress':
    case 'done':
    case 'cancelled':
      return found.category;
    default:
      return 'unknown';
  }
}

/** Human name of a status id, falling back to the raw id. */
export function statusName(
  project: ProjectSummary | undefined,
  status: string | undefined,
): string {
  if (!status) return 'No status';
  return project?.statuses.find((s) => s.id === status)?.name ?? status;
}

/** Tailwind classes for a status badge, keyed by category token. */
export function statusBadgeClass(category: StatusCategory): string {
  switch (category) {
    case 'todo':
      return 'border-transparent bg-[hsl(var(--status-todo)/0.15)] text-[hsl(var(--status-todo))]';
    case 'in_progress':
      return 'border-transparent bg-[hsl(var(--status-in-progress)/0.18)] text-[hsl(var(--status-in-progress))]';
    case 'done':
      return 'border-transparent bg-[hsl(var(--status-done)/0.15)] text-[hsl(var(--status-done))]';
    case 'cancelled':
      return 'border-transparent bg-[hsl(var(--status-cancelled)/0.15)] text-[hsl(var(--status-cancelled))]';
    default:
      return 'border-transparent bg-secondary text-secondary-foreground';
  }
}

export function priorityBadgeClass(priority: Priority | undefined): string {
  switch (priority) {
    case 'critical':
      return 'border-transparent bg-[hsl(var(--priority-critical)/0.15)] text-[hsl(var(--priority-critical))]';
    case 'high':
      return 'border-transparent bg-[hsl(var(--priority-high)/0.15)] text-[hsl(var(--priority-high))]';
    case 'medium':
      return 'border-transparent bg-[hsl(var(--priority-medium)/0.15)] text-[hsl(var(--priority-medium))]';
    case 'low':
      return 'border-transparent bg-[hsl(var(--priority-low)/0.15)] text-[hsl(var(--priority-low))]';
    default:
      return 'border-transparent bg-secondary text-secondary-foreground';
  }
}

const typeNames: Record<ItemType, string> = {
  epic: 'Epic',
  story: 'Story',
  task: 'Task',
  milestone: 'Milestone',
  comment: 'Comment',
};

export function typeName(type: ItemType): string {
  return typeNames[type];
}

const linkKindNames: Record<LinkKind, string> = {
  blocks: 'Blocks',
  blocked_by: 'Blocked by',
  relates_to: 'Relates to',
  duplicates: 'Duplicates',
};

/** Inverse of a typed relation (docs/03-data-model.md §12.1). */
const linkKindInverses: Record<LinkKind, string> = {
  blocks: 'Blocked by',
  blocked_by: 'Blocks',
  relates_to: 'Relates to',
  duplicates: 'Duplicated by',
};

export function linkKindName(kind: LinkKind): string {
  return linkKindNames[kind] ?? kind;
}

/** "…, and the target sees this as <inverse>" — shown as the relation hint. */
export function linkKindInverse(kind: LinkKind): string {
  return linkKindInverses[kind] ?? kind;
}

export type AcceptanceProgress = { checked: number; total: number; percent: number };

const TASK_LIST_ITEM = /^[ \t]*[-*+] \[([ xX])\]/gm;

/**
 * Counts the acceptance-criteria checkboxes of a body. When the conventional
 * `## Acceptance Criteria` section exists only that section is counted;
 * otherwise every task-list item in the body is.
 */
export function acceptanceProgress(body: string | undefined): AcceptanceProgress {
  const empty: AcceptanceProgress = { checked: 0, total: 0, percent: 0 };
  if (!body) return empty;

  const section = extractAcceptanceSection(body);
  const scope = section ?? body;

  let checked = 0;
  let total = 0;
  TASK_LIST_ITEM.lastIndex = 0;
  for (const match of scope.matchAll(TASK_LIST_ITEM)) {
    total += 1;
    if (match[1] !== ' ') checked += 1;
  }
  if (total === 0) return empty;
  return { checked, total, percent: Math.round((checked / total) * 100) };
}

function extractAcceptanceSection(body: string): string | null {
  const heading = /^##+\s*acceptance criteria\s*$/im.exec(body);
  if (!heading) return null;
  const start = heading.index + heading[0].length;
  const rest = body.slice(start);
  const next = /^##+\s+\S/m.exec(rest);
  return next ? rest.slice(0, next.index) : rest;
}

/** Roll-up of a set of items by workflow category. */
export type CategoryRollup = {
  total: number;
  done: number;
  inProgress: number;
  todo: number;
  cancelled: number;
  points: number;
  donePoints: number;
  percent: number;
};

export function rollup(items: Item[], project: ProjectSummary | undefined): CategoryRollup {
  const result: CategoryRollup = {
    total: 0,
    done: 0,
    inProgress: 0,
    todo: 0,
    cancelled: 0,
    points: 0,
    donePoints: 0,
    percent: 0,
  };

  for (const item of items) {
    result.total += 1;
    result.points += item.estimate ?? 0;
    switch (statusCategory(project, item.status)) {
      case 'done':
        result.done += 1;
        result.donePoints += item.estimate ?? 0;
        break;
      case 'in_progress':
        result.inProgress += 1;
        break;
      case 'cancelled':
        result.cancelled += 1;
        break;
      default:
        result.todo += 1;
    }
  }

  const counted = result.total - result.cancelled;
  result.percent = counted > 0 ? Math.round((result.done / counted) * 100) : 0;
  return result;
}

/** Short, locale-independent date rendering (`2026-09-01`). */
export function formatDate(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toISOString().slice(0, 10);
}

/** Item ids look like `ACME-US-0042`; cross-project refs like `WEB/WEB-US-0031`. */
export function projectKeyOf(id: string, fallback: string): string {
  const qualified = id.includes('/') ? id.split('/')[0] : undefined;
  if (qualified) return qualified;
  const prefix = id.split('-')[0];
  return prefix && prefix.length > 0 ? prefix : fallback;
}

/** Strips the optional `<PROJECT>/` qualifier from a link target. */
export function bareItemId(id: string): string {
  const parts = id.split('/');
  return parts[parts.length - 1] ?? id;
}
