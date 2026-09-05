import { Plus } from 'lucide-react';

import { FeatureLink } from '@/features/backlog/FeatureLink';
import type { EditableItemType } from '@/features/editor/templates';

type NewItemLinkProps = {
  /** Project key the new item belongs to. */
  project: string;
  /** Type the editor opens with. */
  type: EditableItemType;
  /** Owning epic for a story, owning story for a task. */
  parent?: string;
  /** Milestone the new item is filed under. */
  milestone?: string;
  /** Visible text; the accessible name adds the context when there is one. */
  label: string;
  /** `bar` sits in a page header, `inline` next to a row or a card. */
  variant?: 'bar' | 'inline';
};

const styles = {
  bar: 'inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90',
  inline:
    'inline-flex h-7 items-center gap-1 rounded-md border border-input px-2 text-xs font-medium text-muted-foreground hover:bg-secondary hover:text-foreground',
} as const;

/**
 * A create control that opens the shared new-item editor
 * (`/p/$project/items/new`) with the type — and, where the surface implies one,
 * the parent or the milestone — already filled in.
 *
 * There is one create implementation in the app, `NewItemPage`; every view that
 * lists items reaches it through this link rather than growing a form of its
 * own. The pre-filled values travel as search parameters, which the route
 * already validates (`validateNewItemSearch`).
 */
export function NewItemLink({
  project,
  type,
  parent,
  milestone,
  label,
  variant = 'inline',
}: NewItemLinkProps) {
  const context = parent ?? milestone;
  return (
    <FeatureLink
      to="/p/$project/items/new"
      params={{ project }}
      search={{
        type,
        ...(parent ? { parent } : {}),
        ...(milestone ? { milestone } : {}),
      }}
      title={context ? `${label} in ${context}` : label}
      aria-label={context ? `${label} in ${context}` : label}
      className={styles[variant]}
    >
      <Plus aria-hidden="true" className={variant === 'bar' ? 'h-4 w-4' : 'h-3 w-3'} />
      {label}
    </FeatureLink>
  );
}
