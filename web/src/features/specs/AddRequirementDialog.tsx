import { useId, useState } from 'react';

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
import { requirementTemplate } from '@/features/editor/templates';
import { useCreateRequirement } from '@/features/specs/queries';

/**
 * "Add requirement": a title and the block text, prefilled with the EARS
 * template. It goes through `createRequirement`, so the core allocates the
 * `R<n>` (R-REQ-5), writes the `### <REF> — <title>` heading itself and
 * records the entry with the workflow's initial status — this form never
 * guesses a number or touches the rest of the spec.
 */
export function AddRequirementDialog({
  project,
  spec,
  specTitle,
  open,
  onOpenChange,
}: {
  project: string;
  spec: string;
  specTitle: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [title, setTitle] = useState('');
  const [text, setText] = useState(requirementTemplate);
  const create = useCreateRequirement(project);
  const { toast } = useToast();
  const titleId = useId();
  const textId = useId();

  const close = (next: boolean) => {
    if (!next) {
      setTitle('');
      setText(requirementTemplate);
      create.reset();
    }
    onOpenChange(next);
  };

  const trimmed = title.trim();

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add requirement</DialogTitle>
          <DialogDescription>
            Appended to {spec} — {specTitle}. Its number is allocated when it is written.
          </DialogDescription>
        </DialogHeader>
        <form
          aria-label="Add requirement"
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (trimmed === '') return;
            create.mutate(
              { spec, title: trimmed, ...(text.trim() === '' ? {} : { text }) },
              {
                onSuccess: (result) => {
                  toast({
                    title: `Added ${result.requirement.ref}`,
                    description: result.requirement.title,
                  });
                  const similar = result.similar ?? [];
                  if (similar.length > 0) {
                    // Advisory only (GIT-US-0111): the block is written; the
                    // author decides whether one of these makes it a duplicate.
                    toast({
                      title: 'Similar requirements exist',
                      description: similar.map((s) => `${s.ref} — ${s.title}`).join('; '),
                    });
                  }
                  close(false);
                },
                onError: (error) => {
                  toast({
                    variant: 'destructive',
                    title: 'The requirement could not be added',
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
              maxLength={200}
              placeholder="Allocate the next ID by index scan"
              onChange={(event) => setTitle(event.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor={textId}>Statement and scenarios</Label>
            <Textarea
              id={textId}
              rows={8}
              className="font-mono text-sm"
              value={text}
              onChange={(event) => setText(event.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              An EARS statement (<code>WHEN …, the … SHALL …</code>) followed by{' '}
              <code>#### Scenario:</code> blocks.
            </p>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => close(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={trimmed === '' || create.isPending}>
              {create.isPending ? 'Adding…' : 'Add requirement'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
