import { Link, useSearch } from '@tanstack/react-router';
import { useState } from 'react';

import { useProvider } from '@/api/provider-context';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Select } from '@/components/ui/select';
import { useToast } from '@/components/ui/toast';
import { NewSprintDialog } from '@/features/boards/NewSprintDialog';
import { useBoards, useUpdateBoard } from '@/features/boards/queries';
import { useSprints } from '@/features/boards/sprint-queries';

/**
 * The sprint index (docs/04-team-repository.md §8, story GIT-US-0032).
 *
 * It exists because a scrum board with no sprint used to be a dead end: the
 * sprint panel only renders once the board points at one. From here a sprint
 * can be opened for any board, and a board can be pointed at a sprint that
 * already exists — the two ways out of that state.
 */
export function SprintList() {
  // `strict: false` so the panel also renders outside the /sprints route, in a
  // component test and wherever it is embedded.
  const search: Record<string, unknown> = useSearch({ strict: false });
  const initialBoard = typeof search['board'] === 'string' ? search['board'] : '';
  const provider = useProvider();
  const boards = useBoards();
  const [board, setBoard] = useState(initialBoard);
  const [creating, setCreating] = useState(false);
  const sprints = useSprints(board ? { board } : {});
  const updateBoard = useUpdateBoard();
  const { toast } = useToast();

  const rows = sprints.data ?? [];
  const boardOf = (id: string) => (boards.data ?? []).find((b) => b.id === id);

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">Sprints</h1>
        <p className="text-sm text-muted-foreground">
          Sprints live in the team repository. A scrum board shows the one it points at; pointing it
          at another one changes the board file and no item.
        </p>
      </header>

      <div className="flex flex-wrap items-end gap-2">
        <label className="text-xs">
          <span className="mb-1 block text-muted-foreground">Board</span>
          <Select
            aria-label="Filter by board"
            className="h-8 w-56 text-xs"
            value={board}
            onChange={(event) => setBoard(event.target.value)}
          >
            <option value="">Every board</option>
            {(boards.data ?? []).map((entry) => (
              <option key={entry.id} value={entry.id}>
                {entry.title}
              </option>
            ))}
          </Select>
        </label>
        <Button
          size="sm"
          disabled={!board || !provider.capabilities.write}
          onClick={() => setCreating(true)}
        >
          New sprint
        </Button>
      </div>

      {board ? (
        <NewSprintDialog
          board={board}
          {...(() => {
            const rev = boardOf(board)?.rev;
            return rev === undefined ? {} : { boardRev: rev };
          })()}
          open={creating}
          onOpenChange={setCreating}
          attach={boardOf(board)?.sprint === undefined}
        />
      ) : null}

      {sprints.isPending ? <p className="text-sm text-muted-foreground">Loading sprints…</p> : null}

      {!sprints.isPending && rows.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>No sprint yet</CardTitle>
          </CardHeader>
          <CardContent className="text-sm text-muted-foreground">
            Pick a scrum board above and open its first sprint.
          </CardContent>
        </Card>
      ) : null}

      <ul className="space-y-3">
        {rows.map((sprint) => {
          const target = boardOf(sprint.board);
          const shown = target?.sprint === sprint.id;
          return (
            <li key={sprint.id}>
              <Card>
                <CardHeader>
                  <CardTitle className="flex flex-wrap items-center gap-2 text-base">
                    <span>{sprint.title}</span>
                    <Badge variant="outline" size="sm" className="font-normal">
                      {sprint.state}
                    </Badge>
                    <span className="font-mono text-xs font-normal text-muted-foreground">
                      {sprint.id}
                    </span>
                  </CardTitle>
                  <CardDescription>
                    {sprint.start} → {sprint.end} · {sprint.metrics.donePoints} of{' '}
                    {sprint.metrics.points} points ·{' '}
                    <Link
                      to="/boards/$slug"
                      params={{ slug: sprint.board }}
                      className="underline underline-offset-2"
                    >
                      {target?.title ?? sprint.board}
                    </Link>
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-wrap items-center gap-2 text-xs">
                  {shown ? (
                    <span className="text-muted-foreground">Shown on its board.</span>
                  ) : (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={!target || !provider.capabilities.write}
                      onClick={() =>
                        updateBoard.mutate(
                          {
                            slug: sprint.board,
                            patch: { sprint: sprint.id },
                            ...(target?.rev === undefined ? {} : { rev: target.rev }),
                          },
                          {
                            onError: (error) => {
                              toast({
                                variant: 'destructive',
                                title: 'The board could not be pointed at this sprint',
                                description: error.message,
                              });
                            },
                          },
                        )
                      }
                    >
                      Show it on {target?.title ?? sprint.board}
                    </Button>
                  )}
                  <Link
                    to="/metrics/$sprintId"
                    params={{ sprintId: sprint.id }}
                    className="underline underline-offset-2"
                  >
                    Metrics
                  </Link>
                </CardContent>
              </Card>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
