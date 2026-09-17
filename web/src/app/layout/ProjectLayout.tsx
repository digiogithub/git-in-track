import { Outlet } from '@tanstack/react-router';

import { ProjectSearchOverlay } from '@/features/search/ProjectSearchOverlay';

/**
 * The layout of every `/p/$project/...` route: the page itself, plus what only
 * exists inside a project — today the Ctrl+Shift+F search overlay
 * (story GIT-US-0103).
 */
export function ProjectLayout() {
  return (
    <>
      <Outlet />
      <ProjectSearchOverlay />
    </>
  );
}
