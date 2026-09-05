import { useNavigate } from '@tanstack/react-router';
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
import { useToast } from '@/components/ui/toast';
import { draftOf, emptyBoardForm } from '@/features/boards/board-form';
import { BoardFormFields } from '@/features/boards/BoardFormFields';
import { useCreateBoard } from '@/features/boards/queries';

/**
 * Creates a board in the team repository (docs/04-team-repository.md §5,
 * story GIT-US-0032).
 *
 * The core allocates the slug from the name and refuses to overwrite a board
 * that already exists, so the dialog never has to guess whether the file is
 * free. On success it navigates to the board it has just created.
 */
export function NewBoardDialog({
  open,
  onOpenChange,
  projects,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projects: string[];
}) {
  const [form, setForm] = useState(() => emptyBoardForm());
  const create = useCreateBoard();
  const navigate = useNavigate();
  const { toast } = useToast();

  const close = (next: boolean) => {
    if (!next) setForm(emptyBoardForm());
    onOpenChange(next);
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>New board</DialogTitle>
          <DialogDescription>
            A board is a view stored in the team repository. It shows the items its projects and
            filters select — nothing is copied into it, and deleting it later changes no item.
          </DialogDescription>
        </DialogHeader>
        <form
          aria-label="New board"
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            create.mutate(draftOf(form), {
              onSuccess: (view) => {
                close(false);
                void navigate({ to: '/boards/$slug', params: { slug: view.id } });
              },
              onError: (error) => {
                toast({
                  variant: 'destructive',
                  title:
                    error instanceof ProviderError && error.code === 'duplicate_id'
                      ? 'That board already exists'
                      : 'The board could not be created',
                  description: error.message,
                });
              },
            });
          }}
        >
          <BoardFormFields form={form} onChange={setForm} projects={projects} />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => close(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={form.title.trim() === '' || create.isPending}>
              Create board
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
