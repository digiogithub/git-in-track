/**
 * The backlog remembers the last filter applied to each project (GIT-US-0203).
 *
 * The URL stays the source of truth for what the list shows (`search.ts`): this
 * module only decides what an *empty* URL means. A URL that carries any filter,
 * view or sort param wins and is stored as the project's last filter; an empty
 * one is filled from storage, or from the default (pending work only) when
 * nothing usable is stored. Like `app/ui-prefs.ts` it is a per-browser
 * convenience, so it lives in `localStorage` and never in the repository.
 */

import { useEffect } from 'react';

import type { ProjectSummary } from '@/api/provider';
import {
  defaultSearch,
  parseItemSearch,
  quickViews,
  toSearchInput,
  type ItemSearch,
  type ItemSearchInput,
} from '@/features/backlog/search';
import { useSetItemSearch } from '@/features/backlog/use-search';

const STORAGE_PREFIX = 'gintrack:backlog-filter:';

/**
 * Storage key of one project's filter. The repository id joins the key when
 * the provider reports it, so two repositories that reuse a project key keep
 * independent filters.
 */
export function filterStorageKey(projectKey: string, vaultId?: string): string {
  return `${STORAGE_PREFIX}${vaultId ? `${vaultId}/` : ''}${projectKey}`;
}

/** True when the search carries any param at all: a filter, a view or a sort. */
export function hasSearchParams(search: ItemSearch): boolean {
  return Object.values(search).some((value) =>
    Array.isArray(value) ? value.length > 0 : value !== undefined,
  );
}

function forget(key: string): void {
  try {
    globalThis.localStorage?.removeItem(key);
  } catch {
    // Nothing to do: a storage that cannot be written cannot hold a stale value.
  }
}

/**
 * Reads a project's stored filter, or `null` when there is none or it is no
 * longer usable: bad JSON, a non-object, nothing left after validation, a
 * status the project's workflow does not declare, or an unknown quick view.
 * An unusable value is removed so it is not re-read on every visit.
 */
export function readStoredFilter(
  key: string,
  project: ProjectSummary | undefined,
): ItemSearchInput | null {
  let raw: string | null | undefined;
  try {
    raw = globalThis.localStorage?.getItem(key);
  } catch {
    // Private modes and sandboxes can throw: behave as if nothing was stored.
    return null;
  }
  if (!raw) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    forget(key);
    return null;
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    forget(key);
    return null;
  }

  const search = parseItemSearch(parsed);
  const known = new Set((project?.statuses ?? []).map((status) => status.id));
  const staleStatus =
    project !== undefined && (search.status ?? []).some((status) => !known.has(status));
  const staleView =
    search.view !== undefined && !quickViews.some((view) => view.id === search.view);
  if (!hasSearchParams(search) || staleStatus || staleView) {
    forget(key);
    return null;
  }
  return toSearchInput(search);
}

/** Stores a project's filter. A failed write only loses the persistence. */
export function writeStoredFilter(key: string, search: ItemSearch): void {
  try {
    globalThis.localStorage?.setItem(key, JSON.stringify(toSearchInput(search)));
  } catch {
    // The filter still holds in the URL; only its memory is lost.
  }
}

export type BacklogFilterMemoryOptions = {
  projectKey: string;
  project: ProjectSummary | undefined;
  /** The project summary has been answered (or failed): staleness can be judged. */
  settled: boolean;
  search: ItemSearch;
};

/**
 * Keeps the URL and the stored filter of the current project in step. Returns
 * `ready: false` while an empty URL is still being filled, so the caller does
 * not fetch the unfiltered list only to throw it away.
 */
export function useBacklogFilterMemory({
  projectKey,
  project,
  settled,
  search,
}: BacklogFilterMemoryOptions): { ready: boolean } {
  const setSearch = useSetItemSearch();
  const hasParams = hasSearchParams(search);
  const vaultId = project?.vaultId;

  useEffect(() => {
    if (!settled || projectKey === '') return;
    const key = filterStorageKey(projectKey, vaultId);
    if (hasParams) {
      writeStoredFilter(key, search);
      return;
    }
    setSearch(readStoredFilter(key, project) ?? defaultSearch);
  }, [settled, projectKey, vaultId, project, hasParams, search, setSearch]);

  return { ready: settled && hasParams };
}
