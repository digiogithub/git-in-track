/**
 * The question dialog (task GIT-T-0074).
 *
 * `AskUserQuestion` is a real tool whose implementation Pando swaps for one
 * that blocks on the client (`internal/agui/hitl.go:176-220`), so the model
 * sees the same schema it always sees and the browser owns the answer. Two
 * things follow.
 *
 * **A dropped answer is not silence, it is a cancellation.** Pando reads no
 * answer as "the user did not answer; continue on your own judgement"
 * (`questionCancelled`, `hitl.go:222`). So cancelling here sends that
 * explicitly — the transcript then records that the agent asked and got
 * nothing — and Escape asks first, because a turn should not end on a stray
 * keypress.
 *
 * **Offered choices are not the only answer.** Every question also gets a free
 * text field: Pando's own answer shape carries `otherText` alongside
 * `selected`, and a question whose options all miss the point is common enough
 * that not offering prose would make the dialog a dead end.
 */

import { MessagesSquare } from 'lucide-react';
import { useState } from 'react';

import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Textarea } from '@/components/ui/textarea';
import type { QuestionPrompt, QuestionSelection } from '@/features/agent/hitl';

export type QuestionDialogProps = {
  prompt: QuestionPrompt;
  readOnly?: boolean;
  onAnswer: (selections: QuestionSelection[]) => void;
  onCancel: () => void;
};

function emptySelections(count: number): QuestionSelection[] {
  return Array.from({ length: count }, () => ({ selected: [], otherText: '' }));
}

export function QuestionDialog({
  prompt,
  readOnly = false,
  onAnswer,
  onCancel,
}: QuestionDialogProps) {
  const [selections, setSelections] = useState(() => emptySelections(prompt.questions.length));
  const [confirming, setConfirming] = useState(false);

  function update(index: number, patch: Partial<QuestionSelection>): void {
    setSelections((previous) =>
      previous.map((entry, position) => (position === index ? { ...entry, ...patch } : entry)),
    );
  }

  function toggle(index: number, label: string, multi: boolean): void {
    const current = selections[index]?.selected ?? [];
    if (multi) {
      update(index, {
        selected: current.includes(label)
          ? current.filter((entry) => entry !== label)
          : [...current, label],
      });
      return;
    }
    update(index, { selected: current.includes(label) ? [] : [label] });
  }

  const answered = selections.some(
    (entry) => entry.selected.length > 0 || entry.otherText.trim() !== '',
  );

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) setConfirming(true);
      }}
    >
      <DialogContent
        aria-labelledby="agent-question-title"
        className="w-[min(40rem,calc(100vw-2rem))]"
        onInteractOutside={(event) => {
          event.preventDefault();
          setConfirming(true);
        }}
      >
        <DialogHeader>
          <DialogTitle id="agent-question-title" className="flex items-center gap-2">
            <MessagesSquare aria-hidden="true" className="h-4 w-4 text-accent" />
            The agent has a question
          </DialogTitle>
          <DialogDescription>
            Your answer goes straight into the run. Cancelling tells the agent to carry on with its
            own judgement.
          </DialogDescription>
        </DialogHeader>

        <div className="max-h-[50vh] space-y-5 overflow-y-auto">
          {prompt.questions.map((question, index) => {
            const multi = question.multiSelect === true;
            const selection = selections[index] ?? { selected: [], otherText: '' };
            return (
              <fieldset key={`${question.header}-${String(index)}`} className="min-w-0">
                <legend className="mb-1 text-sm font-medium">{question.header}</legend>
                <p className="mb-2 whitespace-pre-wrap break-words text-sm text-muted-foreground">
                  {question.question}
                </p>

                {question.options.length === 0 ? null : (
                  <div className="space-y-1.5" role={multi ? 'group' : 'radiogroup'}>
                    {question.options.map((option) => {
                      const checked = selection.selected.includes(option.label);
                      return (
                        <label
                          key={option.label}
                          className="flex cursor-pointer items-start gap-2 rounded-sm px-1 py-1 text-sm hover:bg-secondary"
                        >
                          <Checkbox
                            className="mt-0.5"
                            checked={checked}
                            onChange={() => toggle(index, option.label, multi)}
                          />
                          <span className="min-w-0">
                            <span className="break-words">{option.label}</span>
                            {option.description === '' ? null : (
                              <span className="block text-xs text-muted-foreground">
                                {option.description}
                              </span>
                            )}
                          </span>
                        </label>
                      );
                    })}
                  </div>
                )}

                <Textarea
                  className="mt-2 min-h-16"
                  aria-label={
                    question.options.length === 0
                      ? `Answer: ${question.header}`
                      : `Something else: ${question.header}`
                  }
                  placeholder={
                    question.options.length === 0 ? 'Your answer' : 'Something else (optional)'
                  }
                  value={selection.otherText}
                  onChange={(event) => update(index, { otherText: event.target.value })}
                />
              </fieldset>
            );
          })}
        </div>

        {readOnly ? (
          <p className="mt-4 text-sm text-muted-foreground">
            Another tab owns this conversation, so it has to answer there.
          </p>
        ) : confirming ? (
          <div
            role="alertdialog"
            aria-label="Confirm the cancellation"
            className="mt-4 rounded-md border border-warning/40 bg-warning/5 p-3 text-sm"
          >
            <p>Cancel the question? The agent continues without an answer.</p>
            <div className="mt-3 flex justify-end gap-2">
              <Button variant="ghost" size="sm" onClick={() => setConfirming(false)}>
                Keep answering
              </Button>
              <Button variant="destructive" size="sm" onClick={onCancel}>
                Cancel the question
              </Button>
            </div>
          </div>
        ) : (
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirming(true)}>
              Cancel
            </Button>
            <Button variant="accent" disabled={!answered} onClick={() => onAnswer(selections)}>
              Send the answer
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}
