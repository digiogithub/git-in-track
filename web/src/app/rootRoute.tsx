import { createRootRoute } from '@tanstack/react-router';

import { AppShell } from '@/app/layout/AppShell';
import { NotFound } from '@/app/layout/NotFound';

/**
 * The root of the code-based route tree. Feature modules import this to declare
 * their own routes (`features/<name>/routes.tsx`) and `router.tsx` assembles
 * the tree, so several features can add routes without editing one file.
 */
export const rootRoute = createRootRoute({
  component: AppShell,
  notFoundComponent: NotFound,
});
