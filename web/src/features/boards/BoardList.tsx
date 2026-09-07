import { Link } from '@tanstack/react-router';
import { Columns3 } from 'lucide-react';
import { useState } from 'react';

import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { NewBoardDialog } from '@/features/boards/NewBoardDialog';
import { useBoards } from '@/features/boards/queries';
import { useActiveTeam } from '@/features/workspace/active-team';
import { TeamSelector } from '@/features/workspace/TeamSelector';

/**
 * Board index (docs/04-team-repository.md §5). Boards live in the team
 * repository; without one open there is nothing to list, which is a state and
 * not an error.
 */
export function BoardList() {
  const boards = useBoards();
  const provider = useProvider();
  const team = useActiveTeam();
  const [creating, setCreating] = useState(false);

  /**
   * Why "New board" cannot be pressed, or null when it can. A disabled button
   * with no explanation is a dead end (story GIT-US-0035): boards live in a
   * team repository, so without one open there is nothing to create them in.
   */
  const blocked = !provider.capabilities.write
    ? 'This workspace is read-only, so a board cannot be written.'
    : team.isPending
      ? 'Loading the team repositories…'
      : team.team === undefined
        ? 'Boards live in a team repository, and none is open.'
        : null;
  const canCreate = blocked === null;

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="page-title">Boards</h1>
          <TeamSelector />
          <Button
            size="sm"
            disabled={!canCreate}
            title={blocked ?? undefined}
            onClick={() => setCreating(true)}
          >
            New board
          </Button>
        </div>
        <p className="text-sm text-muted-foreground">
          Boards live in the team repository and pull cards from every configured project. A board
          holds no items of its own: its cards are the work its projects and filters select.
        </p>
        {blocked ? <p className="text-sm text-muted-foreground">{blocked}</p> : null}
      </header>

      <NewBoardDialog
        open={creating}
        onOpenChange={setCreating}
        projects={(team.team?.projects ?? []).map((project) => project.key)}
      />

      {boards.isPending ? <p className="text-sm text-muted-foreground">Loading boards…</p> : null}

      {!boards.isPending && (boards.data ?? []).length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>{team.team ? 'No board yet' : 'No team repository is open'}</CardTitle>
            <CardDescription>
              {team.team
                ? 'This team repository holds no board. Create the first one with "New board".'
                : 'Boards, sprints and retrospectives all live in a team repository — a folder holding a team.yaml.'}
            </CardDescription>
          </CardHeader>
          {team.team ? null : (
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <p>Add one from a folder on this device, or create one where there is none yet.</p>
              <Link
                to="/repos/add"
                className="inline-flex items-center gap-1 rounded-full border border-border px-3 py-1 text-xs hover:bg-secondary"
              >
                Add or create a team repository
              </Link>
            </CardContent>
          )}
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
                {(board.projects ?? []).map((key) => (
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
