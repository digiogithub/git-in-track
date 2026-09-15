/**
 * One message (task GIT-T-0050).
 *
 * Two rules decide everything here.
 *
 * **Agent output is untrusted.** It is rendered by the same pipeline as
 * repository Markdown (`@/markdown`, public surface only), which sanitises as
 * its last transform, so raw HTML in a reply is text and never markup. Nothing
 * in this file builds HTML, and `externalImages` is off: an image URL a model
 * chose is a request a model chose to make.
 *
 * **Streaming must not re-parse the document on every frame.** `renderMarkdown`
 * is a full unified pass; running it per delta on a long answer burns the main
 * thread. So the parsed source is throttled and the unparsed tail — always a
 * suffix of it, because text only grows — is rendered as plain text next to it.
 * The seam is invisible: the tail becomes parsed a beat later, and when the
 * stream ends the whole message settles as Markdown.
 */

import { Bot, TriangleAlert, User } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';

import type { AgentMessage } from '@/features/agent/types';
import { ReasoningBlock } from '@/features/agent/ui/ReasoningBlock';
import { ToolCallCard } from '@/features/agent/ui/ToolCallCard';
import { cn } from '@/lib/cn';
import { MarkdownContent, useMarkdown } from '@/markdown';

/** How often the streaming text is re-parsed, in milliseconds. */
export const PARSE_INTERVAL = 120;

export type MessageBubbleProps = {
  message: AgentMessage;
  /** This message is the one the live run is still writing into. */
  streaming?: boolean;
};

/**
 * A value that follows `source` but changes at most every `intervalMs`, and
 * always lands on the final value.
 *
 * The trailing edge is the part that matters: a throttle that only fires on
 * the leading edge would leave the last few hundred characters of an answer
 * unparsed forever.
 */
function useThrottled(source: string, intervalMs: number): string {
  const [value, setValue] = useState(source);
  const lastRef = useRef(0);

  useEffect(() => {
    const elapsed = Date.now() - lastRef.current;
    if (elapsed >= intervalMs) {
      lastRef.current = Date.now();
      setValue(source);
      return;
    }
    const timer = setTimeout(() => {
      lastRef.current = Date.now();
      setValue(source);
    }, intervalMs - elapsed);
    return () => {
      clearTimeout(timer);
    };
  }, [source, intervalMs]);

  return value;
}

/** Assistant text: parsed prefix, plain tail, both sanitised by construction. */
function AssistantText({ text, streaming }: { text: string; streaming: boolean }) {
  const parsedSource = useThrottled(text, PARSE_INTERVAL);
  // Only a prefix of the live text can be trusted as "already parsed": a
  // thread switch replaces the string outright.
  const prefix = text.startsWith(parsedSource) ? parsedSource : text;
  const tail = text.slice(prefix.length);
  // Shiki is a lazily loaded chunk and a full highlight pass per frame is
  // exactly the work the throttle exists to avoid, so it waits for the end.
  const options = useMemo(
    () => ({ wikilinks: false, externalImages: false, highlight: !streaming }),
    [streaming],
  );
  const markdown = useMarkdown(prefix, options);

  return (
    <div className="min-w-0">
      {markdown.result ? (
        <MarkdownContent result={markdown.result} className="text-sm" />
      ) : (
        <p className="whitespace-pre-wrap break-words text-sm leading-relaxed">{prefix}</p>
      )}
      {tail === '' ? null : (
        <p className="whitespace-pre-wrap break-words text-sm leading-relaxed">{tail}</p>
      )}
      {streaming ? (
        <span
          data-testid="stream-caret"
          aria-hidden="true"
          className="ml-0.5 inline-block h-4 w-[2px] translate-y-0.5 animate-pulse bg-accent align-baseline"
        />
      ) : null}
    </div>
  );
}

export function MessageBubble({ message, streaming = false }: MessageBubbleProps) {
  if (message.role === 'user') {
    return (
      <article aria-label="You" className="flex justify-end gap-3">
        <div className="max-w-[46rem] rounded-lg rounded-tr-sm bg-secondary px-3.5 py-2.5">
          {/* Escaped text, not Markdown: what the user typed is what they see. */}
          <p className="whitespace-pre-wrap break-words text-sm leading-relaxed">{message.text}</p>
        </div>
        <span
          aria-hidden="true"
          className="mt-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-secondary text-muted-foreground"
        >
          <User className="h-3.5 w-3.5" />
        </span>
      </article>
    );
  }

  if (message.role === 'activity') {
    return (
      <p className="pl-9 text-xs italic text-muted-foreground" data-testid="activity">
        {message.activityType ?? 'activity'}
      </p>
    );
  }

  return (
    <article aria-label="Agent" className="flex gap-3">
      <span
        aria-hidden="true"
        className={cn(
          'mt-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full',
          message.error === undefined
            ? 'bg-accent-subtle text-accent'
            : 'bg-destructive/15 text-destructive',
        )}
      >
        {message.error === undefined ? (
          <Bot className="h-3.5 w-3.5" />
        ) : (
          <TriangleAlert className="h-3.5 w-3.5" />
        )}
      </span>

      <div className="min-w-0 flex-1 space-y-2">
        {message.reasoning === undefined ? null : (
          <ReasoningBlock reasoning={message.reasoning} messageId={message.id} />
        )}
        {message.text === '' ? null : <AssistantText text={message.text} streaming={streaming} />}
        {message.toolCalls.map((call) => (
          <ToolCallCard key={call.id} call={call} />
        ))}
        {message.error === undefined ? null : (
          <p className="text-sm text-destructive">{message.error}</p>
        )}
      </div>
    </article>
  );
}
