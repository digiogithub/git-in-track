/**
 * Grouping a sprint listing by the status the core derived (ADR-034).
 *
 * It is its own module so the rule can be tested without rendering a screen,
 * and so the list component stays a component: the status itself is never
 * computed here. `SprintSummary.status` arrives already decided by the core —
 * from the dates against the host's day — because a front end that recomputed
 * it would eventually disagree with the CLI about what "current" means.
 */

import type { SprintStatus, SprintSummary } from '@/api/provider';

/**
 * The order a sprint index reads in: what is running now, then what is coming,
 * then what has not been scheduled, then the archive. It mirrors
 * `core.SprintStatusOrder`, so both hosts agree on more than the words.
 */
export const SPRINT_STATUS_ORDER: SprintStatus[] = ['current', 'upcoming', 'draft', 'completed'];

/** The heading each group carries. */
export const SPRINT_GROUP_LABEL: Record<SprintStatus, string> = {
  current: 'Current',
  upcoming: 'Upcoming',
  draft: 'Draft',
  completed: 'Completed',
};

/** One sentence under each heading, saying what puts a sprint in it. */
export const SPRINT_GROUP_HINT: Record<SprintStatus, string> = {
  current: 'Running today, by its dates.',
  upcoming: 'Scheduled, not started yet.',
  draft: 'No dates: give one a start and an end date to schedule it.',
  completed: 'Its end date has passed.',
};

/**
 * Groups by derived status, dropping the groups that are empty so a workspace
 * with one sprint does not read as three empty headings.
 */
export function groupSprints(rows: SprintSummary[]): Array<[SprintStatus, SprintSummary[]]> {
  const groups: Array<[SprintStatus, SprintSummary[]]> = SPRINT_STATUS_ORDER.map((status) => [
    status,
    rows.filter((row) => row.status === status),
  ]);
  return groups.filter(([, group]) => group.length > 0);
}
