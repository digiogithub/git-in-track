import { describe, expect, it } from 'vitest';

import { sampleItems, sampleProject } from '@/api/fake-provider';

import {
  acceptanceProgress,
  linkKindInverse,
  rollup,
  statusCategory,
  statusName,
} from './item-meta';

describe('statusCategory', () => {
  it('maps a status id to its workflow category', () => {
    expect(statusCategory(sampleProject, 'in_review')).toBe('in_progress');
    expect(statusCategory(sampleProject, 'done')).toBe('done');
    expect(statusCategory(sampleProject, 'made-up')).toBe('unknown');
    expect(statusName(sampleProject, 'in_review')).toBe('In Review');
  });
});

describe('acceptanceProgress', () => {
  it('counts only the checkboxes of the acceptance criteria section', () => {
    const body = [
      '## Description',
      '',
      '- [x] not a criterion',
      '',
      '## Acceptance Criteria',
      '',
      '- [x] first',
      '- [ ] second',
      '- [ ] third',
      '',
      '## Notes',
      '',
      '- [x] not a criterion either',
    ].join('\n');

    expect(acceptanceProgress(body)).toEqual({ checked: 1, total: 3, percent: 33 });
  });

  it('falls back to every task list item when there is no section', () => {
    expect(acceptanceProgress('- [x] a\n- [ ] b\n')).toEqual({
      checked: 1,
      total: 2,
      percent: 50,
    });
  });

  it('reports nothing for a body without checkboxes', () => {
    expect(acceptanceProgress('Just prose.')).toEqual({ checked: 0, total: 0, percent: 0 });
    expect(acceptanceProgress(undefined)).toEqual({ checked: 0, total: 0, percent: 0 });
  });
});

describe('rollup', () => {
  it('summarises a set of items by category and points', () => {
    const stories = sampleItems.filter((item) => item.parent === 'ACME-EP-0001');
    const summary = rollup(stories, sampleProject);

    expect(summary.total).toBe(2);
    expect(summary.inProgress).toBe(1);
    expect(summary.todo).toBe(1);
    expect(summary.points).toBe(8);
  });
});

describe('linkKindInverse', () => {
  it('names the relation as the target sees it', () => {
    expect(linkKindInverse('blocked_by')).toBe('Blocks');
    expect(linkKindInverse('duplicates')).toBe('Duplicated by');
    expect(linkKindInverse('relates_to')).toBe('Relates to');
  });
});
