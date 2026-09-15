/**
 * The transcript (task GIT-T-0050).
 *
 * Two behaviours are worth naming.
 *
 * **Auto-scroll follows only while the reader is at the bottom.** A chat that
 * yanks the viewport down while someone is reading three answers back is worse
 * than one that never scrolls at all, so the list tracks whether it is pinned
 * and offers a "jump to latest" button when it is not.
 *
 * **It is a polite live region.** New text arrives without a user action, so a
 * screen reader has to be told; `role="log"` with `aria-live="polite"`
 * announces additions without interrupting whatever is being read.
 */

import { ArrowDown } from 'lucide-react';
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

import { Button } from '@/components/ui/button';
import type { AgentMessage, AgentRunStatus } from '@/features/agent/types';
import { MessageBubble } from '@/features/agent/ui/MessageBubble';
import { isVisible } from '@/features/agent/ui/model';

/** How close to the bottom still counts as "at the bottom", in pixels. */
const PIN_SLACK = 48;

export type MessageListProps = {
  messages: readonly AgentMessage[];
  runStatus: AgentRunStatus;
};

export function MessageList({ messages, runStatus }: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [pinned, setPinned] = useState(true);

  const empty = messages.length === 0;
  const visible = messages.filter(isVisible);
  const last = visible.at(-1);
  const streamingId = runStatus === 'running' && last?.role === 'assistant' ? last.id : null;

  const onScroll = useCallback(() => {
    const node = scrollRef.current;
    if (node === null) return;
    setPinned(node.scrollHeight - node.scrollTop - node.clientHeight <= PIN_SLACK);
  }, []);

  const toBottom = useCallback(() => {
    const node = scrollRef.current;
    if (node === null) return;
    node.scrollTop = node.scrollHeight;
    setPinned(true);
  }, []);

  // Layout effect: the scroll happens in the same frame the new text paints,
  // so the list never visibly jumps.
  useLayoutEffect(() => {
    if (!pinned) return;
    const node = scrollRef.current;
    if (node === null) return;
    node.scrollTop = node.scrollHeight;
  }, [messages, pinned]);

  // A thread switch starts at the bottom again, whatever the old one was doing.
  useEffect(() => {
    setPinned(true);
  }, [empty]);

  return (
    <div className="relative min-h-0 flex-1">
      <div
        ref={scrollRef}
        onScroll={onScroll}
        data-testid="message-scroll"
        className="h-full overflow-y-auto px-4 py-4"
      >
        <div
          role="log"
          aria-live="polite"
          aria-relevant="additions text"
          aria-label="Conversation"
          aria-busy={runStatus === 'running'}
          className="mx-auto w-full max-w-4xl space-y-5"
        >
          {visible.map((message) => (
            <MessageBubble
              key={message.id}
              message={message}
              streaming={message.id === streamingId}
            />
          ))}
        </div>
      </div>

      {pinned ? null : (
        <Button
          variant="outline"
          size="sm"
          className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-card"
          onClick={toBottom}
        >
          <ArrowDown aria-hidden="true" className="h-3.5 w-3.5" />
          Jump to latest
        </Button>
      )}
    </div>
  );
}
