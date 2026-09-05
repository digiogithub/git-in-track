import { describe, expect, it } from 'vitest';

import { sampleBoard } from '@/api/fake-provider';
import type { BoardView } from '@/api/provider';
import {
  columnId,
  columnsOf,
  defaultColumns,
  draftOf,
  emptyBoardForm,
  filtersOf,
  formOfBoard,
  listOf,
  patchOf,
} from '@/features/boards/board-form';

/** The sample kanban board as a rendered view, enough for `formOfBoard`. */
function viewOfSample(): BoardView {
  return {
    id: sampleBoard.id,
    kind: sampleBoard.kind,
    title: sampleBoard.title,
    ...(sampleBoard.description === undefined ? {} : { description: sampleBoard.description }),
    path: `.pmngr/boards/${sampleBoard.id}.md`,
    rev: sampleBoard.rev,
    projects: sampleBoard.projects,
    filters: sampleBoard.filters ?? {},
    swimlanes: {},
    card: {},
    columns: sampleBoard.columns.map((column) => ({
      id: column.id,
      name: column.name,
      ...(column.wip === undefined ? {} : { wip: column.wip }),
      statuses: column.statuses,
      cards: [],
      exceeded: false,
    })),
    unmapped: [],
    diagnostics: [],
  };
}

describe('board form', () => {
  it('derives a column id that the core accepts', () => {
    const cases: [string, string][] = [
      ['To Do', 'to_do'],
      ['In Review!', 'in_review'],
      ['2nd pass', 'c2nd_pass'],
      ['', 'column'],
    ];
    for (const [name, want] of cases) {
      expect(columnId(name)).toBe(want);
    }
  });

  it('splits a comma-separated field into trimmed entries', () => {
    expect(listOf(' frontend , , security ')).toEqual(['frontend', 'security']);
    expect(listOf('')).toEqual([]);
  });

  it('starts a scrum board on its backlog column', () => {
    const form = emptyBoardForm('scrum');
    expect(form.columns.map((c) => c.id)).toEqual(['sprint_backlog', 'in_progress', 'done']);
    expect(form.backlogColumn).toBe('sprint_backlog');
    expect(defaultColumns('kanban')[0]?.id).toBe('todo');
  });

  it('sends categories or explicit statuses, never both', () => {
    const form = emptyBoardForm();
    form.columns[1] = { ...form.columns[1]!, mapping: 'statuses', statuses: 'doing, review' };
    const columns = columnsOf(form);
    expect(columns[0]).toMatchObject({ categories: ['todo'] });
    expect(columns[0]?.statuses).toBeUndefined();
    expect(columns[1]).toMatchObject({ statuses: { '*': ['doing', 'review'] } });
    expect(columns[1]?.categories).toBeUndefined();
  });

  it('omits a filter nobody set', () => {
    expect(filtersOf(emptyBoardForm())).toEqual({});
  });

  it('builds a create payload with the kind and the scope', () => {
    const form = {
      ...emptyBoardForm('scrum'),
      title: '  Squad Sprint  ',
      projects: ['ACME'],
      types: ['story' as const],
      labelsNone: 'tech-debt',
    };
    const draft = draftOf(form);
    expect(draft).toMatchObject({
      title: 'Squad Sprint',
      kind: 'scrum',
      projects: ['ACME'],
      backlogColumn: 'sprint_backlog',
      filters: { types: ['story'], labelsNone: ['tech-debt'] },
    });
  });

  it('round-trips an existing board through the form', () => {
    const view = viewOfSample();
    const patch = patchOf(formOfBoard(view));
    expect(patch.title).toBe(view.title);
    expect(patch.projects).toEqual(view.projects);
    expect(patch.filters).toEqual({ types: ['story', 'task'] });
    expect(patch.columns?.map((c) => c.id)).toEqual(view.columns.map((c) => c.id));
    expect(patch.columns?.[1]).toMatchObject({ wip: 1, statuses: { '*': ['in_progress'] } });
  });
});
