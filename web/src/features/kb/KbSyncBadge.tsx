/**
 * The knowledge-base synchronization badge (story GIT-US-0093).
 *
 * One chip, five states, always labelled: the tint is the fast signal and the
 * word is the actual one, because nothing here may be communicated by colour
 * alone. When the page is linked, the badge also names the article and links
 * to it — the id is what a reader needs to go and look, and the badge is the
 * only place on this screen that knows it.
 */

import {
  AlertTriangle,
  ArrowDownToLine,
  ArrowUpFromLine,
  Check,
  Link2Off,
  type LucideIcon,
} from 'lucide-react';

import type { KbPageSyncStatus, KbSyncState } from '@/api/provider';
import { Badge } from '@/components/ui/badge';
import { KB_SYNC_HINTS, KB_SYNC_LABELS, KB_SYNC_TONES } from '@/features/kb/kb-sync';
import { cn } from '@/lib/cn';

const ICONS: Record<KbSyncState, LucideIcon> = {
  unlinked: Link2Off,
  in_sync: Check,
  local_ahead: ArrowUpFromLine,
  remote_ahead: ArrowDownToLine,
  conflict: AlertTriangle,
};

export type KbSyncBadgeProps = {
  status: KbPageSyncStatus;
  /**
   * Whether the article was actually read. It changes what "in sync" is
   * claiming, so it changes the hint rather than the label: a local answer is
   * still a true answer about the local side.
   */
  remote?: boolean;
  /** Extra text after the label, such as the count a folder summary carries. */
  detail?: string;
  /** `sm` is the dense form a tree row uses. */
  size?: 'default' | 'sm';
  className?: string;
};

/**
 * The badge itself, without the article link — used wherever space is tight
 * (a tree row) and by the full badge below.
 */
export function KbSyncChip({
  status,
  remote = false,
  detail,
  size = 'default',
  className,
}: KbSyncBadgeProps) {
  const Icon = ICONS[status.state];
  const hint =
    status.state === 'in_sync' && !remote
      ? 'This page matches the content it last published. The article itself was not read.'
      : KB_SYNC_HINTS[status.state];

  return (
    <Badge
      variant={KB_SYNC_TONES[status.state]}
      size={size}
      className={cn('gap-1', className)}
      title={status.error ? `${hint} ${status.error}` : hint}
      data-state={status.state}
    >
      <Icon className="h-3.5 w-3.5" aria-hidden={true} />
      <span>
        {KB_SYNC_LABELS[status.state]}
        {detail === undefined ? '' : ` · ${detail}`}
      </span>
    </Badge>
  );
}

/**
 * The page badge: the state, the article it mirrors, and the reason the remote
 * side could not be consulted when that is what happened.
 *
 * A per-page `error` never fails the status call — one unreachable article must
 * not hide the state of every other page — so it is rendered here, beside the
 * page it belongs to, rather than as a screen-level failure.
 */
export function KbSyncBadge({ status, remote = false, className }: KbSyncBadgeProps) {
  return (
    <span className={cn('inline-flex flex-wrap items-center gap-2', className)}>
      <KbSyncChip status={status} remote={remote} />
      {status.articleId ? (
        status.url ? (
          <a
            href={status.url}
            target="_blank"
            rel="noreferrer noopener"
            className="text-xs text-accent underline underline-offset-2"
          >
            {status.articleId}
          </a>
        ) : (
          <span className="text-xs text-muted-foreground">{status.articleId}</span>
        )
      ) : null}
      {status.error ? (
        <span className="text-xs text-warning" role="status">
          The article could not be read: {status.error}
        </span>
      ) : null}
    </span>
  );
}
