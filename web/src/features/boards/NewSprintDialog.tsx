import { TriangleAlert } from 'lucide-react';
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
 * Opens a sprint on a board (docs/04-team-repository.md §8, GIT-US-0032,
 * GIT-US-0089).
 *
 * It lives outside `SprintPanel` on purpose: the panel only renders once a
 * board already points at a sprint, so a brand-new scrum board could never
 * reach the form that gives it its first one. `attach` covers exactly that
 * case — the board is pointed at the sprint that has just been created, which
 * is otherwise something only `sprint.start` does.
 *
 * **The dates are optional.** A sprint without them is a draft (ADR-034):
 * scheduling is a separate decision from deciding a sprint exists, and forcing
 * dates up front is what made people invent fake ones. That is also the escape
 * hatch out of an overlap — a refusal here says so rather than only saying no.
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
  const [overlap, setOverlap] = useState<string | null>(null);
  const create = useCreateSprint();
  const updateBoard = useUpdateBoard();
  const { toast } = useToast();

  // One date without the other is not a schedule, and the core would refuse it
  // anyway; saying so here costs one round trip less.
  const halfDated = (draft.start === '') !== (draft.end === '');
  const dated = draft.start !== '' && draft.end !== '';

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New sprint</DialogTitle>
          <DialogDescription>
            A sprint belongs to one board and cannot share a day with another sprint of that board.
            Leave the dates empty to keep it a draft until it is scheduled.
            {attach ? ' The board will show this sprint as soon as it exists.' : ''}
          </DialogDescription>
        </DialogHeader>
        <form
          aria-label="New sprint"
          className="space-y-3"
          onSubmit={(event) => {
            event.preventDefault();
            setOverlap(null);
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
                      onSuccess: () => {
                        onOpenChange(false);
                      },
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
                  // An overlap is answered in the form rather than in a toast:
                  // the fix is one of the fields the person is looking at.
                  if (error instanceof ProviderError && error.code === 'sprint_overlap') {
                    setOverlap(error.message);
                    toast({
                      variant: 'destructive',
                      title: 'These dates overlap another sprint',
                      description: error.message,
                    });
                    return;
                  }
                  toast({
                    variant: 'destructive',
                    title: 'The sprint could not be created',
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
              onChange={(event) => {
                setDraft({ ...draft, title: event.target.value });
              }}
            />
          </div>
          <div className="flex gap-2 text-xs">
            <div>
              <span className="mb-1 block text-muted-foreground">Start (optional)</span>
              <Input
                aria-label="Start"
                type="date"
                value={draft.start}
                onChange={(event) => {
                  setDraft({ ...draft, start: event.target.value });
                }}
              />
            </div>
            <div>
              <span className="mb-1 block text-muted-foreground">End (optional)</span>
              <Input
                aria-label="End"
                type="date"
                value={draft.end}
                onChange={(event) => {
                  setDraft({ ...draft, end: event.target.value });
                }}
              />
            </div>
          </div>
          {halfDated ? (
            <p role="alert" className="text-xs text-destructive">
              Give both dates or neither: one date on its own does not schedule a sprint.
            </p>
          ) : null}
          {!dated && !halfDated ? (
            <p className="text-xs text-muted-foreground">
              With no dates this sprint is created as a draft. Add them whenever it is scheduled.
            </p>
          ) : null}
          <div className="text-xs">
            <span className="mb-1 block text-muted-foreground">Goal</span>
            <Input
              aria-label="Goal"
              value={draft.goal}
              onChange={(event) => {
                setDraft({ ...draft, goal: event.target.value });
              }}
            />
          </div>

          {overlap === null ? null : (
            <div
              role="alert"
              className="space-y-2 rounded-lg border border-destructive/40 bg-destructive/15 p-3 text-xs text-destructive"
            >
              <p className="flex items-center gap-2 font-medium">
                <TriangleAlert aria-hidden="true" className="h-4 w-4 shrink-0" />
                These dates overlap another sprint
              </p>
              <p>{overlap}</p>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => {
                  setDraft({ ...draft, start: '', end: '' });
                  setOverlap(null);
                }}
              >
                Remove the dates and keep it a draft
              </Button>
            </div>
          )}

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                onOpenChange(false);
              }}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={halfDated || create.isPending}>
              Create sprint
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
