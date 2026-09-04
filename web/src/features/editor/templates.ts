/**
 * Body templates per item type (docs/03-data-model.md §7.2, §8.2, §9.2, §10.2).
 * They are conventions, not validator rules, so the user can delete them.
 */

export type EditableItemType = 'epic' | 'story' | 'task' | 'milestone';

export const editableItemTypes: EditableItemType[] = ['epic', 'story', 'task', 'milestone'];

const templates: Record<EditableItemType, string> = {
  epic: '## Description\n\n\n\n## Goals\n\n- \n',
  story: '## Description\n\n\n\n## Acceptance Criteria\n\n- [ ] \n',
  task: '## Description\n\n\n',
  milestone: '## Description\n\n\n\n## Exit Criteria\n\n- [ ] \n',
};

export function bodyTemplate(type: EditableItemType): string {
  return templates[type];
}

/** True when the body is still an untouched template, so switching type may replace it. */
export function isPristineTemplate(body: string): boolean {
  return body.trim() === '' || editableItemTypes.some((type) => templates[type] === body);
}

export function isEditableItemType(value: unknown): value is EditableItemType {
  return typeof value === 'string' && (editableItemTypes as string[]).includes(value);
}
