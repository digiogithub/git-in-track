import { Link } from '@tanstack/react-router';
import { FolderGit2, Plus } from 'lucide-react';

import { useAppStore } from '@/app/store';
import { buttonVariants } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

/**
 * Landing surface. Phase 1 replaces the placeholder with repository cards fed
 * by `DataProvider.listRepos()`.
 */
export function WorkspaceHome() {
  const mode = useAppStore((state) => state.mode);

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Workspace</h1>
        <p className="text-sm text-muted-foreground">
          Mounted repositories, recent edits and sync health. Running in <strong>{mode}</strong>{' '}
          mode.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <FolderGit2 aria-hidden="true" className="h-4 w-4" />
            No repositories yet
          </CardTitle>
          <CardDescription>
            Mount a project or team repository to index its epics, stories, tasks and knowledge
            base.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Link to="/repos/add" className={buttonVariants()}>
            <Plus aria-hidden="true" className="h-4 w-4" />
            Add repository
          </Link>
        </CardContent>
      </Card>
    </div>
  );
}
