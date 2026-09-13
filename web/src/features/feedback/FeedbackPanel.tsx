import { Trash2 } from 'lucide-react';
import { useId, useState } from 'react';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { fieldClasses } from '@/components/ui/field';
import { Label } from '@/components/ui/label';
import type { FeedbackDraftApi } from '@/features/feedback/feedback-store';
import { lineLabel } from '@/features/feedback/selection';
import { cn } from '@/lib/cn';

export type FeedbackPanelProps = {
  feedback: FeedbackDraftApi;
  /** What saving produces, for the copy: a comment, or the page's feedback block. */
  destination: 'comment' | 'page';
  canWrite: boolean;
  saving: boolean;
  /** Why the last save failed; the notes are kept. */
  error?: string | null;
  onSave: () => void;
  /**
   * The "send to YouTrack after saving" option.
   *
   * It is passed in rather than read here because only the caller knows the
   * three facts that make it meaningful — the runtime can reach YouTrack, the
   * project is connected, and the item mirrors an issue. Absent, the checkbox
   * does not exist, which is also the only correct answer for the `page`
   * destination: a note on a knowledge-base page is a block in the page, not a
   * comment, and it has no issue to reach.
   */
  push?: FeedbackPushOption;
};

/** The state of the "send to YouTrack after saving" checkbox. */
export type FeedbackPushOption = {
  enabled: boolean;
  onChange: (enabled: boolean) => void;
};

/**
 * The notes of a feedback session. They stay in this browser — across
 * reloads — until they are saved or discarded, and every one can be edited or
 * dropped before that.
 */
export function FeedbackPanel({
  feedback,
  destination,
  canWrite,
  saving,
  error,
  onSave,
  push,
}: FeedbackPanelProps) {
  const { draft, updateNote, removeNote, setActive, clear } = feedback;
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const count = draft.notes.length;
  const pushId = useId();
  // The KB destination never offers it: that feedback is a block in the page,
  // and it has no issue to reach.
  const pushOption = destination === 'comment' ? push : undefined;

  return (
    <Card aria-label="Feedback notes" role="region">
      <CardHeader className="space-y-1">
        <CardTitle>
          Feedback{count > 0 ? ` · ${count === 1 ? '1 note' : `${count} notes`}` : ''}
        </CardTitle>
        <p className="text-sm text-muted-foreground">
          {destination === 'comment'
            ? 'Select text in the description to attach a note to it. Notes are kept in this browser until you save them as one comment.'
            : 'Select text in the page to attach a note to it. Notes are kept in this browser until you save them at the end of the page, anchored to the text they quote.'}
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        {count === 0 ? <p className="empty-state">No notes yet.</p> : null}
        <ol aria-label="Feedback note list" className="space-y-3">
          {draft.notes.map((entry, index) => {
            const where = lineLabel(entry.startLine, entry.endLine);
            return (
              <li key={entry.id} className="space-y-2 rounded-md border border-border p-3">
                <div className="flex items-start justify-between gap-2">
                  <blockquote className="line-clamp-3 min-w-0 border-l-2 border-accent pl-2 text-sm text-muted-foreground">
                    {entry.quote}
                  </blockquote>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label={`Remove note ${index + 1}`}
                    onClick={() => removeNote(entry.id)}
                  >
                    <Trash2 className="size-4" aria-hidden="true" />
                  </Button>
                </div>
                {where ? <p className="text-xs text-muted-foreground">{where}</p> : null}
                <label htmlFor={`feedback-${entry.id}`} className="sr-only">
                  Note {index + 1}
                </label>
                <textarea
                  id={`feedback-${entry.id}`}
                  rows={2}
                  value={entry.note}
                  onChange={(event) => updateNote(entry.id, event.target.value)}
                  className={cn(fieldClasses, 'p-2')}
                />
              </li>
            );
          })}
        </ol>

        {error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : null}

        {pushOption ? (
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Checkbox
                id={pushId}
                checked={pushOption.enabled}
                disabled={!canWrite}
                onChange={(event) => pushOption.onChange(event.target.checked)}
              />
              <Label htmlFor={pushId} className="font-normal">
                Send to YouTrack after saving
              </Label>
            </div>
            <p className="text-xs text-muted-foreground">
              The note is saved as a comment here first, then queued for the linked issue. The
              choice is remembered for this project.
            </p>
          </div>
        ) : null}

        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            disabled={
              !canWrite ||
              saving ||
              count === 0 ||
              draft.notes.some((entry) => entry.note.trim() === '')
            }
            onClick={onSave}
          >
            {saving ? 'Saving…' : 'Save feedback'}
          </Button>
          {confirmDiscard ? (
            <>
              <span className="text-sm">
                Discard {count === 1 ? 'the note' : `${count} notes`}?
              </span>
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  setConfirmDiscard(false);
                  clear();
                }}
              >
                Discard
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setConfirmDiscard(false)}>
                Keep
              </Button>
            </>
          ) : (
            <Button
              size="sm"
              variant="ghost"
              disabled={saving}
              onClick={() => (count === 0 ? clear() : setConfirmDiscard(true))}
            >
              {count === 0 ? 'Close' : 'Discard'}
            </Button>
          )}
          {draft.active && count > 0 ? (
            <Button size="sm" variant="ghost" onClick={() => setActive(false)}>
              Pause selecting
            </Button>
          ) : null}
          {!canWrite ? (
            <p className="text-xs text-muted-foreground">
              This workspace is read-only: the notes stay in this browser.
            </p>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
