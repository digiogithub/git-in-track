/**
 * Reading and writing the backlog filter state, which lives entirely in the
 * URL search params.
 *
 * The router's typed `navigate` is deliberately narrowed here: the rest of the
 * feature works with `ItemSearchInput` (comma-separated lists) and never with
 * the router's generated union type.
 */

import { useNavigate, useSearch } from '@tanstack/react-router';
import { useCallback, useMemo } from 'react';

import type { ItemSearch, ItemSearchInput } from '@/features/backlog/search';
import { parseItemSearch } from '@/features/backlog/search';

type SearchRecord = Record<string, unknown>;

type NavigateWithSearch = (options: {
  search: (prev: SearchRecord) => SearchRecord;
  replace?: boolean;
}) => void;

/** The current, validated filter state. */
export function useItemSearch(): ItemSearch {
  const raw = useSearch({ strict: false });
  return useMemo(() => parseItemSearch(raw), [raw]);
}

/** Drops params that carry no information so URLs stay short. */
function pruneSearch(search: SearchRecord): SearchRecord {
  const next: SearchRecord = {};
  for (const [key, value] of Object.entries(search)) {
    if (value === undefined || value === null || value === '') continue;
    if (Array.isArray(value) && value.length === 0) continue;
    next[key] = value;
  }
  return next;
}

/**
 * Merges a patch into the current search params. `undefined` clears a param,
 * and history is replaced rather than pushed so filtering does not fill the
 * back stack.
 */
export function useSetItemSearch(): (patch: ItemSearchInput) => void {
  const navigate = useNavigate() as unknown as NavigateWithSearch;

  return useCallback(
    (patch: ItemSearchInput) => {
      navigate({
        search: (prev: SearchRecord) => pruneSearch({ ...prev, ...patch }),
        replace: true,
      });
    },
    [navigate],
  );
}
