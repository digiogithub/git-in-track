/**
 * How a card says where its fields came from.
 *
 * A remote card is rendered from the committed `.pmngr/index/<projectKey>.json`
 * of the team repository, which goes stale by design (docs/04 §6). The board
 * shows that age in words: nothing hides behind a fresh-looking card.
 */

/** Renders the age of a snapshot the way a card badge does (R-SNAP-9). */
export function snapshotAge(generated: string | undefined, now: Date = new Date()): string {
  if (!generated) return '';
  const at = Date.parse(generated);
  if (Number.isNaN(at)) return '';
  const seconds = Math.max(0, Math.round((now.getTime() - at) / 1000));
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} minute${minutes === 1 ? '' : 's'} ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} hour${hours === 1 ? '' : 's'} ago`;
  const days = Math.floor(hours / 24);
  return `${days} day${days === 1 ? '' : 's'} ago`;
}

/** The sentence a remote card shows under its fields. */
export function snapshotCaption(
  card: { source?: string; snapshotAt?: string; stale?: boolean },
  now: Date = new Date(),
): string {
  if (card.source !== 'snapshot') return '';
  const age = snapshotAge(card.snapshotAt, now);
  if (!age) return 'From the team snapshot';
  return card.stale ? `Stale snapshot, ${age}` : `Snapshot from ${age}`;
}
