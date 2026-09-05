import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Columns3 } from 'lucide-react';
import { useState } from 'react';

import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { NewBoardDialog } from '@/features/boards/NewBoardDialog';
import { useBoards } from '@/features/boards/queries';

/**
 * Board index (docs/04-team-repository.md §5). Boards live in the team
 * repository; without one open there is nothing to list, which is a state and
 * not an error.
 */
export function BoardList() {
  const boards = useBoards();
  const provider = useProvider();
  const team = useQuery({ queryKey: ['team'], queryFn: () => provider.getTeam() });
  const [creating, setCreating] = useState(false);

  const canCreate = provider.capabilities.write && team.data !== null && team.data !== undefined;

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="text-2xl font-semibold tracking-tight">Boards</h1>
          <Button size="sm" disabled={!canCreate} onClick={() => setCreating(true)}>
            New board
          </Button>
        </div>
        <p className="text-sm text-muted-foreground">
          Boards live in the team repository and pull cards from every configured project. A board
          holds no items of its own: its cards are the work its projects and filters select.
        </p>
      </header>

      <NewBoardDialog
        open={creating}
        onOpenChange={setCreating}
        projects={(team.data?.projects ?? []).map((project) => project.key)}
      />

      {boards.isPending ? <p className="text-sm text-muted-foreground">Loading boards…</p> : null}

      {!boards.isPending && (boards.data ?? []).length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>No board to show</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Mount a team repository with <code>.pmngr/boards/</code> to see its boards, sprints and
            retrospectives.
          </CardContent>
        </Card>
      ) : null}

      <ul className="grid gap-3 sm:grid-cols-2">
        {(boards.data ?? []).map((board) => (
          <li key={board.id}>
            <Card>
              <CardHeader>
                <CardTitle className="flex flex-wrap items-center gap-2 text-base">
                  <Columns3 aria-hidden="true" className="h-4 w-4" />
                  <Link
                    to="/boards/$slug"
                    params={{ slug: board.id }}
                    className="text-accent underline-offset-4 hover:underline"
                  >
                    {board.title}
                  </Link>
                  <Badge variant="outline" size="sm" className="font-normal">
                    {board.kind}
                  </Badge>
                  {board.sprint ? (
                    <Badge variant="outline" size="sm" className="font-normal">
                      {board.sprint}
                    </Badge>
                  ) : null}
                </CardTitle>
                <CardDescription>
                  {board.description ?? `${board.columns} columns`}
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-wrap gap-1 text-xs">
                {board.projects.map((key) => (
                  <Badge key={key} variant="outline" size="sm" className="font-normal">
                    {key}
                  </Badge>
                ))}
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>
    </div>
  );
}
