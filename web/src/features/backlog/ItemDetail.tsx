import { useParams } from '@tanstack/react-router';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

/** Item read view: metadata, body, children, links and comments (Phase 1). */
export function ItemDetail() {
  const params = useParams({ strict: false });
  const project = params.project ?? 'unknown';
  const id = params.id ?? 'unknown';

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">{id}</h1>
        <p className="text-sm text-muted-foreground">
          Project <strong>{project}</strong>
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Item not loaded</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Reads go through <code>DataProvider.getItem()</code>; writes are revision-checked.
        </CardContent>
      </Card>
    </div>
  );
}
