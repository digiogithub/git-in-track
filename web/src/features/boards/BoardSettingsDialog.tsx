import { useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

import type { BoardView } from '@/api/provider';
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
import { useToast } from '@/components/ui/toast';
import { formOfBoard, patchOf } from '@/features/boards/board-form';
import { BoardFormFields } from '@/features/boards/BoardFormFields';
import { useDeleteBoard, useUpdateBoard } from '@/features/boards/queries';

/**
 * Edits the board file itself (docs/04-team-repository.md §5, GIT-US-0032):
 * the name, the project scope, the filters, the columns with their mapped
 * statuses and WIP limits, and — on a scrum board — the backlog column.
 *
 * The card order is never patched here; it moves one card at a time. Deleting
 * lives in the same dialog because it is the same object's life cycle, behind a
 * confirmation and refused while a sprint still names the board.
 */
export function BoardSettingsDialog({
  view,
  projects,
  open,
  onOpenChange,
}: {
  view: BoardView;
  projects: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [form, setForm] = useState(() => formOfBoard(view));
  const [confirming, setConfirming] = useState(false);
  const update = useUpdateBoard();
  const remove = useDeleteBoard();
  const navigate = useNavigate();
  const { toast } = useToast();

  const close = (next: boolean) => {
    if (next) setForm(formOfBoard(view));
    setConfirming(false);
    onOpenChange(next);
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Board settings</DialogTitle>
          <DialogDescription>
            Everything here edits <code>{view.path}</code> in the team repository. No item is
            touched: the cards are a live query over the projects in scope.
          </DialogDescription>
        </DialogHeader>
        <form
          aria-label="Board settings"
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            update.mutate(
              { slug: view.id, patch: patchOf(form), rev: view.rev },
              {
                onSuccess: () => close(false),
                onError: (error) => {
                  toast({
                    variant: 'destructive',
                    title: 'The board could not be saved',
                    description: error.message,
                  });
                },
              },
            );
          }}
        >
          <BoardFormFields form={form} onChange={setForm} projects={projects} lockKind />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => close(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={form.title.trim() === '' || update.isPending}>
              Save board
            </Button>
          </DialogFooter>
        </form>

        <section aria-label="Delete this board" className="space-y-2 border-t pt-4">
          <h3 className="text-sm font-medium">Delete this board</h3>
          <p className="text-xs text-muted-foreground">
            Deleting removes the board file and nothing else — every epic, story and task it showed
            stays exactly where it is, in its own repository.
          </p>
          {confirming ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs">Delete “{view.title}” for good?</span>
              <Button type="button" size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                Keep it
              </Button>
              <Button
                type="button"
                size="sm"
                variant="destructive"
                disabled={remove.isPending}
                onClick={() =>
                  remove.mutate(
                    { slug: view.id, rev: view.rev },
                    {
                      onSuccess: () => {
                        close(false);
                        void navigate({ to: '/boards' });
                      },
                      onError: (error) => {
                        const running =
                          error instanceof ProviderError &&
                          (error.code === 'sprint_already_active' || error.code === 'board_in_use');
                        toast({
                          variant: 'destructive',
                          title: running
                            ? 'A sprint still belongs to this board'
                            : 'The board could not be deleted',
                          description: error.message,
                        });
                        setConfirming(false);
                      },
                    },
                  )
                }
              >
                Delete board
              </Button>
            </div>
          ) : (
            <Button type="button" size="sm" variant="destructive" onClick={() => setConfirming(true)}>
              Delete board
            </Button>
          )}
        </section>
      </DialogContent>
    </Dialog>
  );
}
