import { Link } from '@tanstack/react-router';
import { EyeOff } from 'lucide-react';

/**
 * What a team route shows while "Show team views" is off (GIT-US-0204): a
 * short notice and the way back, never the page. The team repository is still
 * connected; only this browser's view of it is folded away.
 */
export function TeamViewsOff() {
  return (
    <section
      aria-labelledby="team-views-off-heading"
      className="mx-auto max-w-xl space-y-3 rounded-lg border border-border bg-surface-muted/70 p-6 text-sm"
    >
      <h1
        id="team-views-off-heading"
        className="flex items-center gap-2 text-base font-semibold tracking-tight"
      >
        <EyeOff aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
        Team views are hidden
      </h1>
      <p className="text-muted-foreground">
        Boards, sprints, retrospectives and metrics are turned off in this browser. Your team
        repository is still connected and nothing in it has changed.
      </p>
      <p>
        <Link to="/settings" className="font-medium underline underline-offset-4">
          Turn team views back on in Settings
        </Link>
      </p>
    </section>
  );
}
