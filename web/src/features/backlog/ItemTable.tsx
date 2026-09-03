import { useParams } from '@tanstack/react-router';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

/** Item list view. TanStack Table over the core index lands in Phase 1. */
export function ItemTable() {
  const params = useParams({ strict: false });
  const project = params.project ?? 'unknown';

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Items</h1>
        <p className="text-sm text-muted-foreground">
          Epics, stories, tasks and milestones of <strong>{project}</strong>.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Index empty</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Filters live in the URL search params so a view is shareable.
        </CardContent>
      </Card>
    </div>
  );
}
