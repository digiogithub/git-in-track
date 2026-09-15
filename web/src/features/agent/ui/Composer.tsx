/**
 * The composer (task GIT-T-0058).
 *
 * Enter sends and Shift+Enter breaks the line, which is the convention every
 * chat has trained people into; the hint says so in the field, because a
 * convention nobody states is a guess.
 *
 * The draft is never cleared by anything but a successful send. Stopping a run
 * keeps it, a refusal keeps it, an interrupt keeps it: the one thing a person
 * cannot get back is what they typed.
 */

import { Send, Square } from 'lucide-react';
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';

import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

/** How tall the field may grow before it scrolls, in pixels (about 8 lines). */
const MAX_HEIGHT = 192;

export type ComposerProps = {
  onSend: (prompt: string) => void | Promise<void>;
  onStop: () => void | Promise<void>;
  /** A run is in flight: the send button becomes Stop. */
  running: boolean;
  /**
   * The run is parked on a question this tab has to answer. Sending is
   * blocked, because a second turn would race the suspended one.
   */
  interrupted?: boolean;
  /** Another tab owns this conversation; everything renders, nothing runs. */
  readOnly?: boolean;
  /** Disabled for a reason that is not the two above (no repository yet). */
  disabled?: boolean;
};

export function Composer({
  onSend,
  onStop,
  running,
  interrupted = false,
  readOnly = false,
  disabled = false,
}: ComposerProps) {
  const [draft, setDraft] = useState('');
  const fieldRef = useRef<HTMLTextAreaElement | null>(null);

  // Auto-grow: reset to `auto` first, or the field can only ever get taller.
  useEffect(() => {
    const node = fieldRef.current;
    if (node === null) return;
    node.style.height = 'auto';
    node.style.height = `${String(Math.min(node.scrollHeight, MAX_HEIGHT))}px`;
  }, [draft]);

  const blockedReason = readOnly
    ? 'Another tab owns this conversation. Open a new conversation to talk to the agent here.'
    : interrupted
      ? 'The agent is waiting for an answer above. Answer it to continue this turn.'
      : null;

  const canSend = !running && !disabled && blockedReason === null && draft.trim() !== '';

  function submit(): void {
    if (!canSend) return;
    const prompt = draft.trim();
    setDraft('');
    void onSend(prompt);
  }

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>): void {
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    submit();
  }

  return (
    <div className="border-t border-border bg-surface px-4 py-3">
      <div className="mx-auto w-full max-w-4xl space-y-2">
        {blockedReason === null ? null : (
          <p role="status" className="text-xs text-muted-foreground">
            {blockedReason}
          </p>
        )}
        <div className="flex items-end gap-2">
          <Textarea
            ref={fieldRef}
            rows={1}
            value={draft}
            aria-label="Message the agent"
            aria-describedby="composer-hint"
            placeholder="Ask about the backlog…"
            disabled={disabled || readOnly}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onKeyDown}
            className="max-h-48 min-h-9 resize-none py-2 font-sans"
          />
          {running ? (
            <Button variant="outline" onClick={() => void onStop()} aria-label="Stop the run">
              <Square aria-hidden="true" className="h-3.5 w-3.5" />
              Stop
            </Button>
          ) : (
            <Button variant="accent" disabled={!canSend} onClick={submit} aria-label="Send">
              <Send aria-hidden="true" className="h-3.5 w-3.5" />
              Send
            </Button>
          )}
        </div>
        <p id="composer-hint" className="text-2xs text-muted-foreground">
          Enter sends, Shift+Enter starts a new line.
        </p>
      </div>
    </div>
  );
}
