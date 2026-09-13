import { Link } from '@tanstack/react-router';
import { Inbox } from 'lucide-react';
import { useId, useState } from 'react';

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
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { useToast } from '@/components/ui/toast';
import { useProject } from '@/features/backlog/queries';
import { useCreateInboxItem } from '@/features/inbox/queries';
import { hasTriageStatus } from '@/features/inbox/search';

/** `source` recorded on everything this form files (docs/03 §6.4). */
const WEB_SOURCE = 'web';

const styles = {
  bar: 'inline-flex h-9 items-center gap-2 rounded-md border border-input px-4 text-sm font-medium text-muted-foreground hover:bg-secondary hover:text-foreground',
  inline:
    'inline-flex h-7 items-center gap-1 rounded-md border border-input px-2 text-xs font-medium text-muted-foreground hover:bg-secondary hover:text-foreground',
} as const;

export type AddToInboxButtonProps = {
  /** Project the submission is filed into. */
  project: string;
  /** `bar` sits in a page header, `inline` next to a row or a card. */
  variant?: 'bar' | 'inline';
};

/**
 * "Add to inbox": the capture form, and the only create surface in the app that
 * asks no type, no parent and no status question.
 *
 * That restraint is the point (ADR-033). Everything else that creates an item
 * — `NewItemLink` and the editor behind it — asks a person to place the work in
 * the plan before it exists. A report is not a plan: it arrives, it is real
 * from that moment, and a triager decides the rest later. So this form asks for
 * a title, optionally what happened, and nothing else; the item is filed with
 * the project's triage status and `inbox.source: web`.
 *
 * It renders nothing at all for a project that declares no triage status, for
 * the same reason the sidebar entry does not: such a project has no inbox, and
 * a control leading to an explanation of why a submission cannot be made is
 * worse than no control.
 */
export function AddToInboxButton({ project, variant = 'inline' }: AddToInboxButtonProps) {
  const projectQuery = useProject(project);
  const [open, setOpen] = useState(false);

  if (!projectQuery.isSuccess || !hasTriageStatus(projectQuery.data)) return null;

  return (
    <>
      <button type="button" className={styles[variant]} onClick={() => setOpen(true)}>
        <Inbox aria-hidden="true" className={variant === 'bar' ? 'h-4 w-4' : 'h-3 w-3'} />
        Add to inbox
      </button>
      <AddToInboxDialog project={project} open={open} onOpenChange={setOpen} />
    </>
  );
}

/** What the form has filed, so the confirmation can link to it. */
type Filed = { id: string; title: string };

/**
 * The dialog itself, exported for the screens that own their own trigger.
 *
 * After a successful submission the form does not simply close: it swaps to a
 * confirmation naming the id that was allocated and linking to the queue, which
 * is the only way a person can tell that "somebody will look at this" is true.
 */
export function AddToInboxDialog({
  project,
  open,
  onOpenChange,
}: {
  project: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [filed, setFiled] = useState<Filed | undefined>(undefined);
  const create = useCreateInboxItem(project);
  const { toast } = useToast();
  const titleId = useId();
  const bodyId = useId();

  const close = (next: boolean) => {
    if (!next) {
      setTitle('');
      setBody('');
      setFiled(undefined);
    }
    onOpenChange(next);
  };

  const trimmed = title.trim();

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add to inbox</DialogTitle>
          <DialogDescription>
            Submissions wait in {project}&apos;s triage queue until somebody decides what they are.
            Nothing here is a commitment, so there is no type, parent or status to pick.
          </DialogDescription>
        </DialogHeader>

        {filed === undefined ? (
          <form
            aria-label="Add to inbox"
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (trimmed === '') return;
              create.mutate(
                {
                  project,
                  type: 'story',
                  title: trimmed,
                  source: WEB_SOURCE,
                  ...(body.trim() === '' ? {} : { body }),
                },
                {
                  onSuccess: (item) => setFiled({ id: item.id, title: item.title }),
                  onError: (error) => {
                    toast({
                      variant: 'destructive',
                      title:
                        error instanceof ProviderError && error.code === 'no_triage_status'
                          ? `${project} has no inbox`
                          : 'The submission could not be filed',
                      description: error.message,
                    });
                  },
                },
              );
            }}
          >
            <div className="space-y-1.5">
              <Label htmlFor={titleId}>Title</Label>
              <Input
                id={titleId}
                value={title}
                required
                autoComplete="off"
                placeholder="Checkout hangs on Safari after the address step"
                onChange={(event) => setTitle(event.target.value)}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor={bodyId}>What happened (optional)</Label>
              <Textarea
                id={bodyId}
                rows={5}
                value={body}
                placeholder="Steps, the version it was seen on, who reported it."
                onChange={(event) => setBody(event.target.value)}
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => close(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={trimmed === '' || create.isPending}>
                {create.isPending ? 'Filing…' : 'Add to inbox'}
              </Button>
            </DialogFooter>
          </form>
        ) : (
          <div className="space-y-4">
            <p className="text-sm text-muted-foreground">
              Filed as <span className="font-mono">{filed.id}</span> — {filed.title}. It is waiting
              in the triage queue.
            </p>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => close(false)}>
                Close
              </Button>
              <Link
                to="/p/$project/inbox"
                params={{ project }}
                onClick={() => close(false)}
                className="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
              >
                Open the inbox
              </Link>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
