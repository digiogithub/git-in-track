/**
 * The YouTrack connection of a project, as a query rather than as card state.
 *
 * The settings card owns the *editing* of these values, but two other surfaces
 * have to read them: the comment thread needs `pushComments` to know whether a
 * per-comment action is still meaningful, and the knowledge-base toolbar needs
 * `kbSync`. Reading them through one cached query is what keeps those surfaces
 * from each holding their own, quietly diverging, copy.
 *
 * It is gated on `youtrackSupported` rather than on `youtrack`: a runtime that
 * cannot reach YouTrack at all — browser-only mode — must not issue the call,
 * because the provider there fails loudly by design.
 */

import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import type { YouTrackSettings } from '@/api/provider';
import { useOptionalProvider } from '@/api/provider-context';

export const youtrackSettingsKey = (project = '') => ['youtrack', 'settings', project] as const;

export function useYouTrackSettings(project = ''): UseQueryResult<YouTrackSettings | null, Error> {
  const provider = useOptionalProvider();
  const supported = provider?.capabilities.youtrackSupported === true;

  return useQuery<YouTrackSettings | null, Error>({
    queryKey: youtrackSettingsKey(project),
    queryFn: () =>
      provider?.getYouTrackSettings(project === '' ? {} : { projectKey: project }) ??
      Promise.resolve(null),
    enabled: supported,
    // A connection that is not configured is an answer, not a transient
    // failure: retrying it only delays the screen that has to say so.
    retry: false,
  });
}

/**
 * Whether this project is linked to a YouTrack project at all.
 *
 * Being "supported" and being "linked" are different facts and the UI needs
 * both: the settings card exists on the first, every outbound action on the
 * second.
 */
export function isYouTrackLinked(settings: YouTrackSettings | null | undefined): boolean {
  return settings?.configured === true && settings.project.trim() !== '';
}
