/**
 * Backlinks panel — "referenced by…" from the core link graph
 * (docs/03-data-model.md §14.2). The provider ships the resolved edges with the
 * page, so the frontend never recomputes the graph.
 */

import { itemHref, kbHref } from '@/features/kb/kb-links';
import { RouterLink } from '@/features/kb/KbLink';
import { isItemId } from '@/markdown';

export type KbBacklinksProps = {
  project: string;
  backlinks: string[];
};

export function KbBacklinks({ project, backlinks }: KbBacklinksProps) {
  return (
    <section aria-labelledby="kb-backlinks-title" className="space-y-2 border-t pt-4">
      <h2 id="kb-backlinks-title" className="text-sm font-medium">
        Backlinks
        <span className="ml-2 text-muted-foreground">{backlinks.length}</span>
      </h2>
      {backlinks.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nothing links here yet.</p>
      ) : (
        <ul className="flex flex-wrap gap-2">
          {backlinks.map((target) => (
            <li key={target}>
              <RouterLink
                to={isItemId(target) ? itemHref(project, target) : kbHref(project, target)}
                className="inline-flex rounded border px-2 py-1 text-xs hover:bg-secondary"
              >
                {target}
              </RouterLink>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
