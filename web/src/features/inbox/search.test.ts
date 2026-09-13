/**
 * The pure half of the triage screen: the URL schema, the gating, the status a
 * submission is accepted into, and the next-row computation that keeps a pass
 * moving.
 */

import { describe, expect, it } from 'vitest';

import { sampleProject, sampleTriageProject } from '@/api/fake-provider';
import type { ProjectSummary } from '@/api/provider';
import {
  acceptStatus,
  hasTriageStatus,
  inboxFilterName,
  neighbour,
  nextAfterTriage,
  parseInboxSearch,
  statusesForFilter,
  toInboxFilter,
} from '@/features/inbox/search';

describe('inbox search schema', () => {
  it('defaults to the pending slice when nothing is in the URL', () => {
    const search = parseInboxSearch({});

    expect(search.filter).toBeUndefined();
    expect(inboxFilterName(search)).toBe('pending');
    expect(toInboxFilter(search, { project: 'ACME' })).toEqual({
      project: 'ACME',
      limit: 25,
      status: ['pending'],
    });
  });

  it('round-trips the slice and the selected row, so a pass is a link', () => {
    const search = parseInboxSearch({ filter: 'snoozed', selected: 'ACME-US-0201' });

    expect(inboxFilterName(search)).toBe('snoozed');
    expect(search.selected).toBe('ACME-US-0201');
    expect(toInboxFilter(search, { project: 'ACME' }).status).toEqual(['snoozed']);
  });

  it('asks for no triage state at all under "all"', () => {
    expect(statusesForFilter('all')).toBeUndefined();
    expect(toInboxFilter(parseInboxSearch({ filter: 'all' }), { project: 'ACME' })).toEqual({
      project: 'ACME',
      limit: 25,
    });
  });

  it('degrades a hand-edited URL to the default instead of throwing', () => {
    expect(inboxFilterName(parseInboxSearch({ filter: 'whatever' }))).toBe('pending');
    expect(parseInboxSearch({ selected: '   ' }).selected).toBeUndefined();
    expect(parseInboxSearch(null)).toEqual({});
  });
});

describe('gating', () => {
  it('reports an inbox only for a project that declares a triage status', () => {
    expect(hasTriageStatus(sampleTriageProject)).toBe(true);
    expect(hasTriageStatus(sampleProject)).toBe(false);
    expect(hasTriageStatus(undefined)).toBe(false);
  });
});

describe('acceptStatus', () => {
  it('uses the workflow initial status', () => {
    expect(acceptStatus(sampleTriageProject)).toBe('backlog');
  });

  it('falls back to the first status that is not a triage status', () => {
    const project: ProjectSummary = {
      ...sampleTriageProject,
      statuses: [
        { id: 'triage', name: 'Triage', category: 'triage' },
        { id: 'todo', name: 'To Do', category: 'todo' },
      ],
    };
    delete (project as { workflow?: unknown }).workflow;

    expect(acceptStatus(project)).toBe('todo');
  });

  it('never answers with a triage status, even when the workflow names one', () => {
    // Accepting means "into the ordinary backlog": landing back in triage
    // would leave the submission in the queue it was just taken out of.
    const project: ProjectSummary = {
      ...sampleTriageProject,
      workflow: { initial: 'triage' },
    };

    expect(acceptStatus(project)).toBe('backlog');
  });
});

describe('next-item computation', () => {
  const ids = ['a', 'b', 'c'];

  it('moves to the row below the one that was decided', () => {
    expect(nextAfterTriage(ids, 'a')).toBe('b');
    expect(nextAfterTriage(ids, 'b')).toBe('c');
  });

  it('falls back to the row above for the last one, so the pane is never empty', () => {
    expect(nextAfterTriage(ids, 'c')).toBe('b');
  });

  it('answers null only when the queue is emptied', () => {
    expect(nextAfterTriage(['a'], 'a')).toBeNull();
    expect(nextAfterTriage([], 'a')).toBeNull();
  });

  it('walks with j and k and stops at both ends rather than wrapping', () => {
    expect(neighbour(ids, 'a', 1)).toBe('b');
    expect(neighbour(ids, 'b', -1)).toBe('a');
    expect(neighbour(ids, 'a', -1)).toBeNull();
    expect(neighbour(ids, 'c', 1)).toBeNull();
  });
});
