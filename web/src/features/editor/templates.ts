/**
 * Body templates per item type (docs/03-data-model.md §7.2, §8.2, §9.2, §10.2,
 * §21.1). They are conventions, not validator rules, so the user can delete them.
 */

export type EditableItemType = 'epic' | 'story' | 'task' | 'milestone' | 'spec';

export const editableItemTypes: EditableItemType[] = ['epic', 'story', 'task', 'milestone', 'spec'];

const templates: Record<EditableItemType, string> = {
  epic: '## Description\n\n\n\n## Goals\n\n- \n',
  story: '## Description\n\n\n\n## Acceptance Criteria\n\n- [ ] \n',
  task: '## Description\n\n\n',
  milestone: '## Description\n\n\n\n## Exit Criteria\n\n- [ ] \n',
  // A spec starts with an empty `## Requirements` section: requirements are
  // appended one block at a time by `createRequirement`, which allocates R<n>.
  spec: '## Purpose\n\n\n\n## Scope\n\n\n\n## Requirements\n\n',
};

/**
 * The block text of a new requirement (doc 03 §21.2): an EARS statement and one
 * scenario. It is everything below the `### <REF> — <title>` heading, which the
 * core writes itself because it allocates the ref.
 */
export const requirementTemplate =
  'WHEN <trigger>, the <system> SHALL <response>.\n\n' +
  '#### Scenario: <name>\n' +
  '- **WHEN** <action>\n' +
  '- **THEN** <observable result>\n';

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
