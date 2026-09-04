import { describe, expect, it } from 'vitest';

import { sampleProject } from '@/api/fake-provider';

import { isEmptySearch, parseItemSearch, toItemFilter, toSearchInput } from './search';

describe('parseItemSearch', () => {
  it('reads comma-separated lists', () => {
    const search = parseItemSearch({
      status: 'todo,in_progress',
      type: 'story,task',
      label: 'a,b',
    });

    expect(search.status).toEqual(['todo', 'in_progress']);
    expect(search.type).toEqual(['story', 'task']);
    expect(search.label).toEqual(['a', 'b']);
  });

  it('reads the priority list', () => {
    expect(parseItemSearch({ priority: 'critical,high' }).priority).toEqual(['critical', 'high']);
    expect(toSearchInput(parseItemSearch({ priority: 'low' })).priority).toBe('low');
  });

  it('drops values it does not understand instead of throwing', () => {
    const search = parseItemSearch({
      type: 'wombat',
      category: 'nope',
      priority: 'urgent',
      sort: 'colour',
      q: '',
    });

    expect(search.priority).toBeUndefined();
    expect(search.type).toBeUndefined();
    expect(search.category).toBeUndefined();
    expect(search.sort).toBeUndefined();
    expect(search.q).toBeUndefined();
  });

  it('round-trips through the URL shape', () => {
    const search = parseItemSearch({ status: 'todo,done', assignee: 'jose' });
    expect(toSearchInput(search).status).toBe('todo,done');
    expect(toSearchInput(search).assignee).toBe('jose');
  });
});

describe('isEmptySearch', () => {
  it('ignores sort and view, which are not filters', () => {
    expect(isEmptySearch(parseItemSearch({ sort: 'title', view: 'all' }))).toBe(true);
    expect(isEmptySearch(parseItemSearch({ assignee: 'jose' }))).toBe(false);
    expect(isEmptySearch(parseItemSearch({ priority: 'high' }))).toBe(false);
  });
});

describe('toItemFilter', () => {
  it('expands a category into the project status ids', () => {
    const filter = toItemFilter(parseItemSearch({ category: 'in_progress' }), {
      project: 'ACME',
      projectSummary: sampleProject,
    });

    expect(filter.status).toEqual(['in_progress', 'in_review']);
  });

  it('intersects an explicit status list with the category', () => {
    const filter = toItemFilter(parseItemSearch({ category: 'done', status: 'todo,done' }), {
      project: 'ACME',
      projectSummary: sampleProject,
    });

    expect(filter.status).toEqual(['done']);
  });

  it('resolves the "all open" quick view to the open statuses', () => {
    const filter = toItemFilter(parseItemSearch({ view: 'open' }), {
      project: 'ACME',
      projectSummary: sampleProject,
    });

    expect(filter.status).toEqual(['backlog', 'todo', 'in_progress', 'in_review']);
  });

  it('passes the priority list to the provider filter', () => {
    const filter = toItemFilter(parseItemSearch({ priority: 'critical,high' }), {
      project: 'ACME',
      projectSummary: sampleProject,
    });

    expect(filter.priority).toEqual(['critical', 'high']);
    expect(toItemFilter(parseItemSearch({}), { project: 'ACME' }).priority).toBeUndefined();
  });

  it('passes text, labels and parent through to the provider filter', () => {
    const filter = toItemFilter(
      parseItemSearch({ q: 'sso', label: 'security', parent: 'ACME-EP-0001' }),
      { project: 'ACME', projectSummary: sampleProject },
    );

    expect(filter).toMatchObject({
      project: 'ACME',
      text: 'sso',
      label: ['security'],
      parent: 'ACME-EP-0001',
    });
  });
});
