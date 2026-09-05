/**
 * The shape a board form edits, and the translation between it and the
 * `board.create` / `board.update` payloads (docs/04-team-repository.md §5).
 *
 * A board is a view: its cards are the items the project scope and the filters
 * select, never a list stored in the board file. Everything below therefore
 * edits a query — which is why the form is the only way to "put items on a
 * board", and why the components say so.
 */

import type {
  BoardColumnPatch,
  BoardDraft,
  BoardPatch,
  BoardView,
  ItemType,
  Priority,
  StatusCategory,
} from '@/api/provider';
import type { BoardFilters, BoardKind } from '@/core-bridge/api';

/** The four item types a board may show. */
export const boardItemTypes: ItemType[] = ['epic', 'story', 'task', 'milestone'];

/** The four priorities, highest first. */
export const boardPriorities: Priority[] = ['critical', 'high', 'medium', 'low'];

/** The status categories a column may map (docs/04 R-COL-2). */
export const boardCategories: StatusCategory[] = ['todo', 'in_progress', 'done', 'cancelled'];

/** One column as the form edits it: a name, a mapping and a WIP limit. */
export type ColumnForm = {
  id: string;
  name: string;
  /** `categories` maps status categories; `statuses` maps explicit status ids. */
  mapping: 'categories' | 'statuses';
  categories: StatusCategory[];
  /** The default (`*`) status mapping, as a comma-separated list. */
  statuses: string;
  wip: string;
};

/** Everything the create and the edit forms hold. */
export type BoardForm = {
  title: string;
  kind: BoardKind;
  description: string;
  /** Empty means every project the team declares (docs/04 §5.1). */
  projects: string[];
  columns: ColumnForm[];
  backlogColumn: string;
  types: ItemType[];
  priorities: Priority[];
  labelsAny: string;
  labelsNone: string;
  assignees: string;
  query: string;
  includeClosed: boolean;
};

/** The default columns of a new board, mirroring `core.DefaultBoardColumns`. */
export function defaultColumns(kind: BoardKind): ColumnForm[] {
  return [
    column(kind === 'scrum' ? 'sprint_backlog' : 'todo', kind === 'scrum' ? 'Sprint Backlog' : 'To Do', [
      'todo',
    ]),
    column('in_progress', 'In Progress', ['in_progress']),
    column('done', 'Done', ['done', 'cancelled']),
  ];
}

function column(id: string, name: string, categories: StatusCategory[]): ColumnForm {
  return { id, name, mapping: 'categories', categories, statuses: '', wip: '' };
}

/** The empty form a create dialog opens with. */
export function emptyBoardForm(kind: BoardKind = 'kanban'): BoardForm {
  return {
    title: '',
    kind,
    description: '',
    projects: [],
    columns: defaultColumns(kind),
    backlogColumn: kind === 'scrum' ? 'sprint_backlog' : '',
    types: [],
    priorities: [],
    labelsAny: '',
    labelsNone: '',
    assignees: '',
    query: '',
    includeClosed: false,
  };
}

/** The form of a board that already exists, filled from its rendered view. */
export function formOfBoard(view: BoardView): BoardForm {
  return {
    title: view.title,
    kind: view.kind,
    description: view.description ?? '',
    projects: [...view.projects],
    columns: view.columns.map((c) => ({
      id: c.id,
      name: c.name,
      mapping: (c.categories ?? []).length > 0 ? 'categories' : 'statuses',
      categories: [...(c.categories ?? [])],
      statuses: (c.statuses?.['*'] ?? []).join(', '),
      wip: c.wip ? String(c.wip) : '',
    })),
    backlogColumn: view.backlogColumn ?? '',
    types: [...(view.filters.types ?? [])],
    priorities: [...(view.filters.priorities ?? [])],
    labelsAny: (view.filters.labelsAny ?? []).join(', '),
    labelsNone: (view.filters.labelsNone ?? []).join(', '),
    assignees: (view.filters.assignees ?? []).join(', '),
    query: view.filters.query ?? '',
    includeClosed: view.filters.includeClosed ?? false,
  };
}

/** Splits a comma-separated field into trimmed, non-empty entries. */
export function listOf(raw: string): string[] {
  return raw
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0);
}

/**
 * The id a column name becomes. Column ids are `[a-z][a-z0-9_-]{0,31}`, so a
 * name that starts with a digit gets a `c` in front of it rather than being
 * refused by the core after the round trip.
 */
export function columnId(name: string): string {
  const slug = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
    .slice(0, 32);
  if (slug === '') return 'column';
  return /^[a-z]/.test(slug) ? slug : `c${slug}`.slice(0, 32);
}

/** The columns of a form as the API takes them. */
export function columnsOf(form: BoardForm): BoardColumnPatch[] {
  return form.columns.map((c) => {
    const wip = Number.parseInt(c.wip, 10);
    return {
      id: c.id,
      name: c.name,
      ...(c.mapping === 'categories'
        ? { categories: c.categories }
        : { statuses: { '*': listOf(c.statuses) } }),
      ...(Number.isFinite(wip) && wip > 0 ? { wip } : {}),
    };
  });
}

/** The filter block of a form; absent keys constrain nothing (docs/04 §5.3). */
export function filtersOf(form: BoardForm): BoardFilters {
  const labelsAny = listOf(form.labelsAny);
  const labelsNone = listOf(form.labelsNone);
  const assignees = listOf(form.assignees);
  return {
    ...(form.types.length > 0 ? { types: form.types } : {}),
    ...(form.priorities.length > 0 ? { priorities: form.priorities } : {}),
    ...(labelsAny.length > 0 ? { labelsAny } : {}),
    ...(labelsNone.length > 0 ? { labelsNone } : {}),
    ...(assignees.length > 0 ? { assignees } : {}),
    ...(form.query.trim() ? { query: form.query.trim() } : {}),
    ...(form.includeClosed ? { includeClosed: true } : {}),
  };
}

/** The `board.create` payload of a form. */
export function draftOf(form: BoardForm): BoardDraft {
  const columns = columnsOf(form);
  return {
    title: form.title.trim(),
    kind: form.kind,
    ...(form.description.trim() ? { description: form.description.trim() } : {}),
    ...(form.projects.length > 0 ? { projects: form.projects } : {}),
    columns,
    filters: filtersOf(form),
    ...(form.kind === 'scrum'
      ? { backlogColumn: form.backlogColumn || (columns[0]?.id ?? '') }
      : {}),
  };
}

/**
 * The `board.update` payload of a form. Every field is sent: the dialog holds
 * the whole board, so an omitted key would mean "leave it alone" rather than
 * "the user cleared it".
 */
export function patchOf(form: BoardForm): BoardPatch {
  const columns = columnsOf(form);
  return {
    title: form.title.trim(),
    description: form.description.trim(),
    projects: form.projects,
    columns,
    filters: filtersOf(form),
    ...(form.kind === 'scrum'
      ? { backlogColumn: form.backlogColumn || (columns[0]?.id ?? '') }
      : {}),
  };
}
