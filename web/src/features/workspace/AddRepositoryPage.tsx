import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';

/**
 * Add repository wizard (docs/05-web-app.md §3.1). Phase 1 implements the four
 * steps; Phase 0 ships the route and the shell.
 */
export function AddRepositoryPage() {
  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Add repository</h1>
        <p className="text-sm text-muted-foreground">
          Choose a folder in browser-only mode, or a path served by the companion.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Location</CardTitle>
          <CardDescription>
            The wizard detects <code>.git</code>, <code>project.yaml</code> and the docs folder.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <label className="block space-y-1.5 text-sm font-medium" htmlFor="repo-location">
            Repository path
            <Input id="repo-location" placeholder="/home/me/projects/acme" disabled />
          </label>
          <Button disabled>Detect repository</Button>
        </CardContent>
      </Card>
    </div>
  );
}
