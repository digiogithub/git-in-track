/**
 * Backlog filter state lives in the URL (docs/05-web-app.md §3.1 and §5): the
 * search params ARE the filter, so a view is shareable, bookmarkable and
 * restored on reload. Nothing about a filter is kept in a store.
 *
 * Values are written as comma-separated lists (`?status=todo,in_progress`)
 * rather than JSON arrays so the URL stays readable, and every field is
 * `.catch()`-guarded: a hand-edited URL degrades to "no filter" instead of
 * blowing up the route.
 */

import type { SearchSchemaInput } from '@tanstack/react-router';
import { z } from 'zod';

import type { ItemFilter, ItemType, ProjectSummary } from '@/api/provider';

export const filterableItemTypes = ['epic', 'story', 'task', 'milestone'] as const;

export const statusCategories = ['todo', 'in_progress', 'done', 'cancelled'] as const;

export const sortFields = ['updated', 'created', 'priority', 'id', 'title'] as const;

export type SortField = (typeof sortFields)[number];

/** Splits `a,b` (or an already-parsed array) into a trimmed, non-empty list. */
function toList(value: unknown): string[] | undefined {
  if (value === undefined || value === null || value === '') return undefined;
  const raw: unknown[] = Array.isArray(value)
    ? value
    : typeof value === 'string'
      ? value.split(',')
      : [];
  const items = raw
    .filter((entry): entry is string => typeof entry === 'string')
    .map((entry) => entry.trim())
    .filter(Boolean);
  return items.length > 0 ? items : undefined;
}

/** Sentinel status id that matches nothing, for impossible filter combinations. */
const NO_MATCH_STATUS = '__no_status__';

const optionalText = z.string().trim().min(1).optional().catch(undefined);

const typeList = z
  .preprocess(toList, z.array(z.enum(filterableItemTypes)).optional())
  .catch(undefined);

const stringList = z.preprocess(toList, z.array(z.string()).optional()).catch(undefined);

export const itemSearchSchema = z.object({
  /** Full-text needle, matched by the provider across id, title and body. */
  q: optionalText,
  type: typeList,
  status: stringList,
  category: z.enum(statusCategories).optional().catch(undefined),
  label: stringList,
  assignee: optionalText,
  milestone: optionalText,
  parent: optionalText,
  /** Id of the saved quick view the user picked, kept so the chip stays lit. */
  view: optionalText,
  sort: z.enum(sortFields).optional().catch(undefined),
  order: z.enum(['asc', 'desc']).optional().catch(undefined),
});

export type ItemSearch = z.infer<typeof itemSearchSchema>;

/**
 * What `navigate({ search })` accepts. Lists are written as comma-separated
 * strings; `parseItemSearch` turns them back into arrays when reading.
 */
export type ItemSearchInput = {
  q?: string | undefined;
  type?: string | undefined;
  status?: string | undefined;
  category?: string | undefined;
  label?: string | undefined;
  assignee?: string | undefined;
  milestone?: string | undefined;
  parent?: string | undefined;
  view?: string | undefined;
  sort?: string | undefined;
  order?: string | undefined;
};

/** Parses raw search params. Never throws: unknown values fall back to unset. */
export function parseItemSearch(input: unknown): ItemSearch {
  const result = itemSearchSchema.safeParse(input ?? {});
  return result.success ? result.data : {};
}

/**
 * Route `validateSearch` for the item list.
 *
 * The `SearchSchemaInput` marker keeps the *write* side (`navigate({ search })`,
 * `<Link search={…}>`) on the comma-separated string shape while the *read*
 * side (`useSearch`) gets parsed lists.
 */
export function validateItemSearch(input: ItemSearchInput & SearchSchemaInput): ItemSearch {
  return parseItemSearch(input);
}

/** Serialises a parsed filter back into URL-shaped values. */
export function toSearchInput(search: ItemSearch): ItemSearchInput {
  const join = (value: string[] | undefined) =>
    value && value.length > 0 ? value.join(',') : undefined;
  return {
    q: search.q,
    type: join(search.type),
    status: join(search.status),
    category: search.category,
    label: join(search.label),
    assignee: search.assignee,
    milestone: search.milestone,
    parent: search.parent,
    view: search.view,
    sort: search.sort,
    order: search.order,
  };
}

/** True when no filter at all is applied (drives the "no filters" empty state). */
export function isEmptySearch(search: ItemSearch): boolean {
  return (
    !search.q &&
    !search.type?.length &&
    !search.status?.length &&
    !search.category &&
    !search.label?.length &&
    !search.assignee &&
    !search.milestone &&
    !search.parent
  );
}

/** Status ids belonging to a coarse workflow category, from `project.yaml`. */
export function statusesInCategory(
  project: ProjectSummary | undefined,
  category: string,
): string[] {
  return (project?.statuses ?? []).filter((s) => s.category === category).map((s) => s.id);
}

export type ToFilterOptions = {
  project: string;
  projectSummary?: ProjectSummary | undefined;
  limit?: number;
};

/**
 * Translates URL state into a provider `ItemFilter`.
 *
 * `category` is expanded into the matching status ids here rather than passed
 * through, so the coarse buckets work against any provider and combine with an
 * explicit status filter by intersection.
 */
export function toItemFilter(search: ItemSearch, options: ToFilterOptions): ItemFilter {
  const { project, projectSummary, limit = 50 } = options;

  let status = search.status;
  if (!status && !search.category && search.view === 'open') {
    // "All open" spans two categories, so it resolves to a status list here.
    const open = openStatuses(projectSummary);
    if (open.length > 0) status = open;
  }
  if (search.category) {
    const fromCategory = statusesInCategory(projectSummary, search.category);
    status = status ? status.filter((id) => fromCategory.includes(id)) : fromCategory;
    // An impossible combination must return nothing, not everything.
    if (status.length === 0) status = [NO_MATCH_STATUS];
  }

  const types: ItemType[] | undefined = search.type;

  return {
    project,
    limit,
    sort: search.sort ?? 'updated',
    order: search.order ?? (search.sort === 'id' || search.sort === 'title' ? 'asc' : 'desc'),
    ...(types && types.length > 0 ? { type: types } : {}),
    ...(status && status.length > 0 ? { status } : {}),
    ...(search.label && search.label.length > 0 ? { label: search.label } : {}),
    ...(search.assignee ? { assignee: search.assignee } : {}),
    ...(search.milestone ? { milestone: search.milestone } : {}),
    ...(search.parent ? { parent: search.parent } : {}),
    ...(search.q ? { text: search.q } : {}),
  };
}

export type QuickView = {
  id: string;
  name: string;
  description: string;
  /** Needs the viewer's handle; disabled until one is known. */
  needsIdentity?: boolean;
  search: (identity: string | null) => ItemSearchInput;
};

const emptyView: ItemSearchInput = {
  q: undefined,
  type: undefined,
  status: undefined,
  category: undefined,
  label: undefined,
  assignee: undefined,
  milestone: undefined,
  parent: undefined,
  view: undefined,
  sort: undefined,
  order: undefined,
};

/** Saved views. They are pure URL states, so every one of them is shareable. */
export const quickViews: QuickView[] = [
  {
    id: 'all',
    name: 'All',
    description: 'Every item in the project',
    search: () => ({ ...emptyView, view: 'all' }),
  },
  {
    id: 'open',
    name: 'All open',
    description: 'Everything not done or cancelled',
    search: () => ({ ...emptyView, view: 'open' }),
  },
  {
    id: 'mine',
    name: 'Mine',
    description: 'Assigned to you',
    needsIdentity: true,
    search: (identity) => ({
      ...emptyView,
      view: 'mine',
      ...(identity ? { assignee: identity } : {}),
    }),
  },
  {
    id: 'backlog',
    name: 'Backlog',
    description: 'Waiting to be started',
    search: () => ({ ...emptyView, view: 'backlog', category: 'todo' }),
  },
  {
    id: 'in_progress',
    name: 'In progress',
    description: 'Being worked on right now',
    search: () => ({ ...emptyView, view: 'in_progress', category: 'in_progress' }),
  },
  {
    id: 'done',
    name: 'Done',
    description: 'Completed items',
    search: () => ({ ...emptyView, view: 'done', category: 'done' }),
  },
];

/**
 * The "All open" view has no single category, so it is applied as a status
 * list computed from the project workflow at render time.
 */
export function openStatuses(project: ProjectSummary | undefined): string[] {
  return (project?.statuses ?? [])
    .filter((s) => s.category === 'todo' || s.category === 'in_progress')
    .map((s) => s.id);
}
