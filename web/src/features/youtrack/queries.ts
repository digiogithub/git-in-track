/**
 * TanStack Query hooks for the YouTrack import dialog (story GIT-US-0059).
 *
 * Three operations, three very different cache stories. The **search** is a
 * query: it is read repeatedly while the user types, it is keyed on exactly
 * what was asked for, and it is debounced here rather than in the picker so
 * that one keystroke can never cost more than one request no matter which
 * surface issues it. The **preview** and the **run** are mutations: they are
 * explicit actions with a result the dialog keeps, and nothing else in the app
 * caches them.
 *
 * The dialog never writes items itself. It asks for a preview, shows it, and
 * asks for a run — the vault does the writing, which is what keeps one
 * implementation of "import an issue" behind REST, MCP and the CLI alike.
 */

import { useMutation, useQuery, type UseQueryResult } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import type {
  YouTrackImportOptions,
  YouTrackImportPreviewResult,
  YouTrackImportRun,
  YouTrackIssuePage,
  YouTrackIssuePreset,
  YouTrackScope,
} from '@/api/provider';
import { useProvider } from '@/api/provider-context';

/** How long typing is coalesced for before a search is issued. */
export const IMPORT_SEARCH_DEBOUNCE_MS = 200;

/** How many issues one page of the autosuggest asks for. */
export const IMPORT_SEARCH_LIMIT = 20;

/** Key factory. Everything the import owns lives under one prefix. */
export const youtrackKeys = {
  all: () => ['youtrack'] as const,
  issues: () => ['youtrack', 'issues'] as const,
  search: (projectKey: string, q: string, preset: YouTrackIssuePreset) =>
    ['youtrack', 'issues', projectKey, preset, q] as const,
};

/**
 * Holds `value` back until it has stopped changing for `delayMs`.
 *
 * It lives here rather than in the picker because the debounce belongs to the
 * *request*, not to the input: a preset chip and a typed word both feed the
 * same query, and only one of them comes from a keyboard.
 */
export function useDebounced<T>(value: T, delayMs: number): T {
  const [settled, setSettled] = useState(value);

  useEffect(() => {
    if (delayMs <= 0) {
      setSettled(value);
      return;
    }
    const timer = setTimeout(() => {
      setSettled(value);
    }, delayMs);
    return () => {
      clearTimeout(timer);
    };
  }, [value, delayMs]);

  return settled;
}

export type IssueSearchInput = {
  /** What the user typed; it is debounced inside the hook. */
  q: string;
  preset: YouTrackIssuePreset;
  /** False while the list is closed, so a hidden picker issues no request. */
  enabled?: boolean;
  scope?: YouTrackScope;
  /** Injected by tests; production waits `IMPORT_SEARCH_DEBOUNCE_MS`. */
  debounceMs?: number;
};

/**
 * The issue autosuggest. The query key carries the project, the preset and the
 * debounced text and nothing else, so switching preset back and forth re-reads
 * a cached page instead of the instance.
 */
export function useYouTrackIssueSearch({
  q,
  preset,
  enabled = true,
  scope,
  debounceMs = IMPORT_SEARCH_DEBOUNCE_MS,
}: IssueSearchInput): UseQueryResult<YouTrackIssuePage> {
  const provider = useProvider();
  const text = useDebounced(q.trim(), debounceMs);
  const projectKey = scope?.projectKey ?? '';

  return useQuery({
    queryKey: youtrackKeys.search(projectKey, text, preset),
    queryFn: () =>
      provider.searchYouTrackIssues({ q: text, preset, limit: IMPORT_SEARCH_LIMIT }, scope ?? {}),
    enabled,
    // A search is a question about a remote system: keeping the answer for a
    // minute makes going back to a preset instant without ever showing an
    // issue list that is meaningfully out of date.
    staleTime: 60_000,
  });
}

/** What an import would do, with nothing written. */
export function useYouTrackImportPreview(scope?: YouTrackScope) {
  const provider = useProvider();
  return useMutation<YouTrackImportPreviewResult, Error, YouTrackImportOptions>({
    mutationFn: (options) => provider.previewYouTrackImport(options, scope ?? {}),
  });
}

/**
 * Runs the import. The answer is either a job id to follow over the
 * `sync.job.*` events or the finished result inline — see `YouTrackImportRun`.
 * A single failing issue is reported inside the result, never as a rejection.
 */
export function useYouTrackImportRun(scope?: YouTrackScope) {
  const provider = useProvider();
  return useMutation<YouTrackImportRun, Error, YouTrackImportOptions>({
    mutationFn: (options) => provider.runYouTrackImport(options, scope ?? {}),
  });
}
