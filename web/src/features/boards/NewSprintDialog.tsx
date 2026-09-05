import { useState } from 'react';

import { ProviderError } from '@/api/provider';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { useToast } from '@/components/ui/toast';
import { useUpdateBoard } from '@/features/boards/queries';
import { useCreateSprint } from '@/features/boards/sprint-queries';

/**
 * Opens a sprint on a board (docs/04-team-repository.md §8, GIT-US-0032).
 *
 * It lives outside `SprintPanel` on purpose: the panel only renders once a
 * board already points at a sprint, so a brand-new scrum board could never
 * reach the form that gives it its first one. `attach` covers exactly that
 * case — the board is pointed at the sprint that has just been created, which
 * is otherwise something only `sprint.start` does.
 */
export function NewSprintDialog({
  board,
  boardRev,
  open,
  onOpenChange,
  attach = false,
}: {
  board: string;
  boardRev?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Point the board at the sprint once it exists. */
  attach?: boolean;
}) {
  const [draft, setDraft] = useState({ title: '', start: '', end: '', goal: '' });
  const create = useCreateSprint();
  const updateBoard = useUpdateBoard();
  const { toast } = useToast();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New sprint</DialogTitle>
          <DialogDescription>
            A sprint belongs to one board and cannot share a day with another sprint of that board.
            {attach ? ' The board will show this sprint as soon as it exists.' : ''}
          </DialogDescription>
        </DialogHeader>
        <form
          aria-label="New sprint"
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            create.mutate(
              { board, ...draft },
              {
                onSuccess: (result) => {
                  if (!attach) {
                    onOpenChange(false);
                    return;
                  }
                  updateBoard.mutate(
                    {
                      slug: board,
                      patch: { sprint: result.sprint.sprint.id },
                      ...(boardRev === undefined ? {} : { rev: boardRev }),
                    },
                    {
                      onSuccess: () => onOpenChange(false),
                      onError: (error) => {
                        toast({
                          variant: 'destructive',
                          title: 'The sprint exists, but the board still points at nothing',
                          description: error.message,
                        });
                      },
                    },
                  );
                },
                onError: (error) => {
                  toast({
                    variant: 'destructive',
                    title:
                      error instanceof ProviderError && error.code === 'sprint_overlap'
                        ? 'These dates overlap another sprint'
                        : 'The sprint could not be created',
                    description: error.message,
                  });
                },
              },
            );
          }}
        >
          <div className="text-xs">
            <span className="mb-1 block text-muted-foreground">Title</span>
            <Input
              aria-label="Title"
              value={draft.title}
              onChange={(event) => setDraft({ ...draft, title: event.target.value })}
            />
          </div>
          <div className="flex gap-2 text-xs">
            <div>
              <span className="mb-1 block text-muted-foreground">Start</span>
              <Input
                aria-label="Start"
                type="date"
                required
                value={draft.start}
                onChange={(event) => setDraft({ ...draft, start: event.target.value })}
              />
            </div>
            <div>
              <span className="mb-1 block text-muted-foreground">End</span>
              <Input
                aria-label="End"
                type="date"
                required
                value={draft.end}
                onChange={(event) => setDraft({ ...draft, end: event.target.value })}
              />
            </div>
          </div>
          <div className="text-xs">
            <span className="mb-1 block text-muted-foreground">Goal</span>
            <Input
              aria-label="Goal"
              value={draft.goal}
              onChange={(event) => setDraft({ ...draft, goal: event.target.value })}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit">Create sprint</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
