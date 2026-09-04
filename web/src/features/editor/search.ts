/**
 * Search parameters of `/p/$project/items/new`
 * (`?type=story&parent=ACME-EP-0001&milestone=ACME-M-0001`).
 */

import type { EditableItemType } from '@/features/editor/templates';
import { isEditableItemType } from '@/features/editor/templates';

export type NewItemSearch = {
  type: EditableItemType;
  parent?: string;
  milestone?: string;
};

function readString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' ? value.trim() : undefined;
}

/** `validateSearch` for the route: unknown values fall back to a story. */
export function validateNewItemSearch(input: Record<string, unknown>): NewItemSearch {
  const type: EditableItemType = isEditableItemType(input.type) ? input.type : 'story';
  const parent = readString(input.parent);
  const milestone = readString(input.milestone);
  return {
    type,
    ...(parent ? { parent } : {}),
    ...(milestone ? { milestone } : {}),
  };
}

/** Same validation from the raw query string, so the page needs no route import. */
export function parseNewItemSearch(searchStr: string): NewItemSearch {
  const params = new URLSearchParams(searchStr.startsWith('?') ? searchStr.slice(1) : searchStr);
  return validateNewItemSearch({
    type: params.get('type') ?? undefined,
    parent: params.get('parent') ?? undefined,
    milestone: params.get('milestone') ?? undefined,
  });
}
