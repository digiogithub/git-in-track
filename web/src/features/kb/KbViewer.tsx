import { useParams } from '@tanstack/react-router';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

/** Knowledge base viewer. The Markdown pipeline lands in Phase 1. */
export function KbViewer() {
  const params = useParams({ strict: false });
  const project = params.project ?? 'unknown';
  const path = params._splat ?? '';

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Knowledge base</h1>
        <p className="text-sm text-muted-foreground">
          Project <strong>{project}</strong>
          {path ? (
            <>
              {' '}
              — <code>{path}</code>
            </>
          ) : null}
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Page renderer pending</CardTitle>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          The unified/remark/rehype pipeline, wikilink resolution and the file tree arrive with
          Phase 1.
        </CardContent>
      </Card>
    </div>
  );
}
