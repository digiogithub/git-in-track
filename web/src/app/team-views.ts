/**
 * The team surface (GIT-US-0204): the top-level routes that only make sense
 * with a team repository. The "Show team views" setting hides their navigation
 * entries, and the shell answers a direct visit to any of them, or to one of
 * their children, with a notice instead of the page.
 */
export const TEAM_VIEW_ROUTES = ['/boards', '/sprints', '/retros', '/metrics'] as const;

/** Whether `pathname` is a team view or one of its children. */
export function isTeamViewPath(pathname: string): boolean {
  return TEAM_VIEW_ROUTES.some((root) => pathname === root || pathname.startsWith(`${root}/`));
}
