import { MessageSquarePlus } from 'lucide-react';
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';

import { Button } from '@/components/ui/button';
import { fieldClasses } from '@/components/ui/field';
import type { FeedbackNote, FeedbackNoteInput } from '@/features/feedback/feedback-store';
import { lineLabel, readSelection, type SelectionAnchor } from '@/features/feedback/selection';
import { cn } from '@/lib/cn';

/** Width of the note overlay, in pixels; it is clamped to the content. */
const OVERLAY_WIDTH = 320;

type Pending = { anchor: SelectionAnchor; top: number; left: number };

export type FeedbackSelectionProps = {
  /** The Markdown the content was rendered from; the fallback for line lookup. */
  source: string;
  /** Whether feedback mode is on. */
  active: boolean;
  /** The notes already taken, whose lines are marked in the content. */
  notes: FeedbackNote[];
  /** Changes whenever the rendered content does (`path@rev`). */
  contentKey: string;
  onEnable: () => void;
  onAddNote: (note: FeedbackNoteInput) => void;
  children: ReactNode;
};

/**
 * Wraps rendered Markdown and turns a text selection into a feedback note.
 *
 * Outside feedback mode a selection offers to switch it on; in feedback mode
 * it opens an overlay under the selection where the note is typed. The lines
 * of every note already taken are marked in the content, so the reader sees
 * what has been covered.
 */
export function FeedbackSelection({
  source,
  active,
  notes,
  contentKey,
  onEnable,
  onAddNote,
  children,
}: FeedbackSelectionProps) {
  const host = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [text, setText] = useState('');
  const textRef = useRef(text);
  textRef.current = text;

  const capture = useCallback(() => {
    const container = content.current;
    const box = host.current?.getBoundingClientRect();
    if (!container || !box) return;
    const anchor = readSelection(container, source);
    if (!anchor) {
      // A plain click in the content dismisses an overlay nobody typed in.
      if (textRef.current.trim() === '') setPending(null);
      return;
    }
    const maxLeft = Math.max(0, box.width - OVERLAY_WIDTH);
    setPending({
      anchor,
      top: Math.max(0, anchor.rect.bottom - box.top) + 8,
      left: Math.min(maxLeft, Math.max(0, anchor.rect.left - box.left)),
    });
    setText('');
  }, [source]);

  const close = useCallback(() => {
    setPending(null);
    setText('');
  }, []);

  const submit = () => {
    if (!pending || text.trim() === '') return;
    const { anchor } = pending;
    onAddNote({
      quote: anchor.quote,
      note: text.trim(),
      ...(anchor.startLine === undefined ? {} : { startLine: anchor.startLine }),
      ...(anchor.endLine === undefined ? {} : { endLine: anchor.endLine }),
    });
    globalThis.getSelection?.()?.removeAllRanges();
    close();
  };

  // Marks the innermost block of every noted line range.
  useEffect(() => {
    const container = content.current;
    if (!container) return;
    const blocks = [...container.querySelectorAll<HTMLElement>('[data-line-start]')];
    const hit = new Set<HTMLElement>();
    for (const block of blocks) {
      const start = Number(block.dataset['lineStart']);
      const end = Number(block.dataset['lineEnd'] ?? start);
      if (
        notes.some(
          (note) =>
            note.startLine !== undefined &&
            note.startLine <= end &&
            (note.endLine ?? note.startLine) >= start,
        )
      ) {
        hit.add(block);
      }
    }
    for (const block of blocks) {
      const inner = [...hit].some((other) => other !== block && block.contains(other));
      block.classList.toggle('feedback-marked', hit.has(block) && !inner);
    }
  }, [notes, contentKey]);

  const where = pending ? lineLabel(pending.anchor.startLine, pending.anchor.endLine) : '';

  return (
    <div ref={host} className="relative">
      {/* The handlers only observe the selection the reader made in the
          document; they add no interaction of their own to keyboard users,
          whose Shift+arrow selections are read on key up. */}
      {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions */}
      <div
        ref={content}
        data-feedback-active={active}
        onMouseUp={capture}
        onKeyUp={(event) => {
          if (event.shiftKey || event.key === 'Shift') capture();
        }}
        onTouchEnd={() => {
          // The selection settles after the touch ends.
          setTimeout(capture, 0);
        }}
      >
        {children}
      </div>

      {pending ? (
        // Escape closes the overlay from any of its controls, as in a dialog.
        // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
        <div
          role="dialog"
          aria-label={active ? 'Add a feedback note' : 'Enable feedback mode'}
          className="absolute z-30 max-w-full space-y-2 rounded-md border border-border bg-popover p-3 text-sm text-popover-foreground shadow-pop"
          style={{ top: pending.top, left: pending.left, width: OVERLAY_WIDTH }}
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.preventDefault();
              close();
            }
          }}
        >
          <blockquote className="line-clamp-3 border-l-2 border-accent pl-2 text-xs text-muted-foreground">
            {pending.anchor.quote}
          </blockquote>
          {active ? (
            <>
              {where ? <p className="text-xs text-muted-foreground">{where}</p> : null}
              <label htmlFor="feedback-note" className="sr-only">
                Feedback note
              </label>
              <textarea
                id="feedback-note"
                // The overlay opens on purpose, right after the selection.
                // eslint-disable-next-line jsx-a11y/no-autofocus
                autoFocus
                rows={3}
                value={text}
                placeholder="Comment or clarification…"
                onChange={(event) => setText(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
                    event.preventDefault();
                    submit();
                  }
                }}
                className={cn(fieldClasses, 'p-2')}
              />
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="ghost" onClick={close}>
                  Cancel
                </Button>
                <Button size="sm" disabled={text.trim() === ''} onClick={submit}>
                  Add note
                </Button>
              </div>
            </>
          ) : (
            <>
              <p>Add feedback on this text?</p>
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="ghost" onClick={close}>
                  Not now
                </Button>
                <Button size="sm" onClick={onEnable}>
                  <MessageSquarePlus className="size-4" aria-hidden="true" />
                  Enable feedback mode
                </Button>
              </div>
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}
