/**
 * The conflict notice above a knowledge-base page (story GIT-US-0093,
 * task GIT-T-0218).
 *
 * A conflict is the one state a person has to resolve by hand, so the notice
 * says three things and nothing else: **this page was not modified**, the
 * incoming content is in `<page>.conflict.md`, and which side is newer. There
 * is no three-way merge behind this and there is deliberately none, so the
 * notice offers a file to read rather than a button that would pretend to
 * decide.
 */

import { AlertTriangle } from 'lucide-react';

import { kbHref } from '@/features/kb/kb-links';
import { RouterLink } from '@/features/kb/KbLink';

export type KbConflictNoticeProps = {
  project: string;
  /** The page that diverged. */
  path: string;
  /** The file the incoming content was written to. */
  conflictPath: string;
  articleId?: string | undefined;
  /**
   * The job that found the divergence. It is how the notice can name which
   * side is newer: a `pull` brought a newer article down, a `publish` found one
   * on the way up.
   */
  direction?: 'publish' | 'pull' | undefined;
};

export function KbConflictNotice({
  project,
  path,
  conflictPath,
  articleId,
  direction,
}: KbConflictNoticeProps) {
  const newerSide =
    direction === 'pull'
      ? 'The article in YouTrack changed most recently, and this page changed too.'
      : direction === 'publish'
        ? 'This page changed most recently, and the article in YouTrack changed too.'
        : 'This page and its article both changed since they were last synchronized.';

  return (
    <div
      role="alert"
      className="flex items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/10 p-4 text-sm text-foreground"
    >
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" aria-hidden="true" />
      <div className="space-y-1">
        <p className="font-medium text-destructive">
          This page and its YouTrack article have diverged
        </p>
        <p>
          {newerSide} Nothing was merged: <strong>this page was left exactly as it was</strong>, and
          the incoming content was written beside it.
        </p>
        <p>
          Read{' '}
          <RouterLink
            to={kbHref(project, conflictPath)}
            className="text-accent underline underline-offset-2"
          >
            {conflictPath}
          </RouterLink>{' '}
          and copy across whatever should survive, then delete it
          {articleId ? ` — the article is ${articleId}` : ''}.
        </p>
        <p className="sr-only">The page in conflict is {path}.</p>
      </div>
    </div>
  );
}
