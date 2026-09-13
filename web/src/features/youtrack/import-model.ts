/**
 * The vocabulary of the import dialog: the saved queries it offers and the
 * options it sends (story GIT-US-0059).
 *
 * They live apart from the components that render them because three of those
 * components share them, and because a constant exported from a module that
 * also exports a component costs the dev server its fast refresh.
 */

import type { YouTrackImportOptions, YouTrackIssuePreset } from '@/api/provider';

/** The saved queries, in the order they are offered. */
export const IMPORT_PRESETS: { value: YouTrackIssuePreset; label: string }[] = [
  { value: '', label: 'Anything' },
  { value: 'epics', label: 'Epics' },
  { value: 'stories', label: 'Stories' },
  { value: 'tasks', label: 'Tasks' },
  { value: 'versions', label: 'Versions' },
  { value: 'unresolved', label: 'Unresolved' },
];

/** The deepest subtask recursion the vault accepts (`YouTrackMaxDepth`). */
export const IMPORT_MAX_DEPTH = 5;

/** The editable half of the dialog: everything preview and run are sent. */
export type ImportOptionsDraft = Pick<
  YouTrackImportOptions,
  'depth' | 'includeLinks' | 'includeComments' | 'includeAttachments'
>;

/**
 * Nothing beyond the issues that were picked. An import is a graph walk, and
 * the walk only happens because someone asked for it.
 */
export const defaultImportOptions: ImportOptionsDraft = {
  depth: 0,
  includeLinks: false,
  includeComments: false,
  includeAttachments: false,
};
