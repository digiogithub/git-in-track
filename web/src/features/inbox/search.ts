/**
 * Inbox filter state lives in the URL, exactly as the backlog filter does
 * (`features/backlog/search.ts`): the search params ARE the filter, so a
 * half-cleared triage queue can be pasted into a chat message and reopened by
 * someone else on the same row.
 *
 * Two params and no more: which slice of the queue is listed, and which
 * submission the detail pane is showing. Everything else a triage pass needs —
 * the counts, the pending badge — comes from the answer itself.
 */

import type { SearchSchemaInput } from '@tanstack/react-router';
import { z } from 'zod';

import type { InboxFilter, InboxStatus, ProjectSummary } from '@/api/provider';

/** The three slices the queue is read in. `pending` is the working view. */
export const inboxFilters = ['pending', 'snoozed', 'all'] as const;

export type InboxFilterName = (typeof inboxFilters)[number];

/** The reserved status category that makes a project have an inbox (ADR-033). */
const TRIAGE_CATEGORY = 'triage';

const optionalText = z.string().trim().min(1).optional().catch(undefined);

export const inboxSearchSchema = z.object({
  /** Which slice of the queue is listed; absent means `pending`. */
  filter: z.enum(inboxFilters).optional().catch(undefined),
  /** The submission in the detail pane, so a triage view is linkable. */
  selected: optionalText,
});

export type InboxSearch = z.infer<typeof inboxSearchSchema>;

/** What `navigate({ search })` and `<Link search={…}>` accept. */
export type InboxSearchInput = {
  filter?: string | undefined;
  selected?: string | undefined;
};

/** Parses raw search params. Never throws: a hand-edited URL degrades to the default. */
export function parseInboxSearch(input: unknown): InboxSearch {
  const result = inboxSearchSchema.safeParse(input ?? {});
  return result.success ? result.data : {};
}

/** Route `validateSearch` for the inbox. */
export function validateInboxSearch(input: InboxSearchInput & SearchSchemaInput): InboxSearch {
  return parseInboxSearch(input);
}

/** The slice being listed. An absent param is the working view, not "everything". */
export function inboxFilterName(search: InboxSearch): InboxFilterName {
  return search.filter ?? 'pending';
}

/** The triage states each slice asks the provider for; `all` asks for none. */
export function statusesForFilter(name: InboxFilterName): InboxStatus[] | undefined {
  if (name === 'pending') return ['pending'];
  if (name === 'snoozed') return ['snoozed'];
  return undefined;
}

export type ToInboxFilterOptions = { project: string; limit?: number };

/**
 * Translates URL state into a provider `InboxFilter`.
 *
 * Note what is *not* done here: a snooze whose date has arrived is not filtered
 * out client-side. Expiry is resolved by the host against its own clock, so an
 * expired snooze comes back under `pending` — and is counted there — without
 * this layer needing a clock it cannot agree on.
 */
export function toInboxFilter(search: InboxSearch, options: ToInboxFilterOptions): InboxFilter {
  const { project, limit = 25 } = options;
  const status = statusesForFilter(inboxFilterName(search));
  return {
    project,
    limit,
    ...(status ? { status } : {}),
  };
}

/** Whether a project declares any status in the reserved triage category. */
export function hasTriageStatus(project: ProjectSummary | undefined): boolean {
  return (project?.statuses ?? []).some((status) => status.category === TRIAGE_CATEGORY);
}

/**
 * The status an accepted submission lands in: the workflow's declared initial
 * one, and otherwise the first status that is not a triage status. Accepting
 * means "into the ordinary backlog", so the answer can never be a triage
 * status — that would leave the item in the queue it just left.
 */
export function acceptStatus(project: ProjectSummary | undefined): string {
  const statuses = project?.statuses ?? [];
  const initial = project?.workflow?.initial;
  const declared = statuses.find((status) => status.id === initial);
  if (declared && declared.category !== TRIAGE_CATEGORY) return declared.id;
  return statuses.find((status) => status.category !== TRIAGE_CATEGORY)?.id ?? '';
}

/**
 * The row the pane moves to once `current` leaves the queue.
 *
 * It is computed *before* the mutation runs, which is the whole trick: after
 * the write the row is gone from the list and there is nothing left to compute
 * a neighbour from, so the pane would fall back to empty. The one below wins,
 * and the one above is the fallback for the last row — which is what keeps a
 * pass moving in one direction until the queue is actually empty.
 */
export function nextAfterTriage(ids: string[], current: string): string | null {
  const index = ids.indexOf(current);
  if (index === -1) return ids[0] ?? null;
  return ids[index + 1] ?? ids[index - 1] ?? null;
}

/** The neighbour `j` and `k` move to; it stops at both ends rather than wrapping. */
export function neighbour(ids: string[], current: string, delta: 1 | -1): string | null {
  const index = ids.indexOf(current);
  if (index === -1) return ids[0] ?? null;
  return ids[index + delta] ?? null;
}
