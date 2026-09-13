/**
 * What a job-engine problem code means for the person reading it
 * (docs/07-cli-and-api.md §5.5).
 *
 * The three codes are three different situations and never collapse into one
 * "failed" line: a job that is no longer there, a transition the job's own
 * state does not allow, and an engine that is not running at all. Only the
 * middle one is the user's mistake, and only the last one is worth a restart.
 *
 * Anything else — a YouTrack failure raised while the queue was being read,
 * say — falls through to `youtrackMessage`, which knows those codes.
 */

import { ProviderError } from '@/api/provider';
import { youtrackMessage } from '@/features/settings/youtrack-messages';

export function syncJobMessage(error: unknown): string {
  if (!(error instanceof ProviderError)) {
    return error instanceof Error ? error.message : String(error);
  }
  switch (error.code) {
    case 'sync_job_not_found':
      return 'That job is no longer in the queue. A finished job is pruned after the retention window, so there is nothing left to act on — refresh the list.';
    case 'sync_job_not_retryable':
      return 'The job moved on before the action reached it: only a failed or cancelled job can be retried, and only a queued or running one can be cancelled. Refresh the list to see where it is now.';
    case 'sync_engine_not_running':
      return 'The background job engine is not running, so nothing can be queued, retried or cancelled. Restart the companion to bring it up.';
    default:
      return youtrackMessage(error);
  }
}
