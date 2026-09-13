import { Link } from '@tanstack/react-router';
import { Inbox } from 'lucide-react';

import { useProject } from '@/features/backlog/queries';
import { useInboxEvents, useInboxPending } from '@/features/inbox/queries';
import { hasTriageStatus } from '@/features/inbox/search';
import { cn } from '@/lib/cn';

export type InboxNavLinkProps = {
  project: string;
  className?: string;
  activeClassName?: string;
};

/**
 * The sidebar entry for a project's triage queue, with the number still
 * waiting.
 *
 * It renders nothing at all for a project that declares no triage status: such
 * a project has no inbox, and an entry leading to an explanation of why there
 * is nothing there is worse than no entry (ADR-033).
 *
 * The count comes from the queue's own `pending`, which the host computes over
 * the whole queue against its own clock — so a snooze that has expired is
 * already included, and the badge cannot drift from the list.
 */
export function InboxNavLink({ project, className, activeClassName }: InboxNavLinkProps) {
  const projectQuery = useProject(project);
  const enabled = projectQuery.isSuccess && hasTriageStatus(projectQuery.data);
  const pending = useInboxPending(project, enabled);
  useInboxEvents(project);

  if (!enabled) return null;

  const waiting = pending.data ?? 0;

  return (
    <Link
      to="/p/$project/inbox"
      params={{ project }}
      className={cn(className)}
      {...(activeClassName === undefined ? {} : { activeProps: { className: activeClassName } })}
    >
      <Inbox aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
      <span className="truncate">{project} inbox</span>
      {waiting > 0 ? (
        <span
          className="ml-auto rounded-full bg-accent-subtle px-1.5 text-2xs font-medium text-accent"
          aria-label={`${waiting} waiting`}
        >
          {waiting}
        </span>
      ) : null}
    </Link>
  );
}
