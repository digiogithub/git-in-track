import { Link } from '@tanstack/react-router';

import { buttonVariants } from '@/components/ui/button';

/** Rendered for any unmatched route. */
export function NotFound() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold tracking-tight">Page not found</h1>
      <p className="text-sm text-muted-foreground">
        The route does not exist. It may belong to a phase that is not implemented yet.
      </p>
      <Link to="/" className={buttonVariants({ variant: 'outline' })}>
        Back to the workspace
      </Link>
    </div>
  );
}
