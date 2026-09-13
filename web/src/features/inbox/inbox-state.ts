/**
 * How a triage state is read, as opposed to how it is stored.
 *
 * A snooze is not a timer: nothing wakes it up. The date is simply compared
 * when the queue is read, and the host does that comparison against its own
 * clock — which is why a submission whose date has arrived comes back under
 * `pending` and is counted there. This module exists so that a row rendered in
 * a list says the same thing the badge above it says, instead of reading the
 * stored `snoozed` off the file and disagreeing with the count.
 */

import type { InboxStatus, ItemInbox } from '@/api/provider';

/** The triage state as a reader sees it: an arrived snooze is pending again. */
export function effectiveInboxStatus(inbox: ItemInbox | undefined, today: string): InboxStatus {
  if (!inbox?.status) return 'pending';
  if (inbox.status !== 'snoozed') return inbox.status;
  if (!inbox.snoozedUntil) return 'snoozed';
  return inbox.snoozedUntil <= today ? 'pending' : 'snoozed';
}
