/**
 * The model's reasoning trace, collapsed (task GIT-T-0053).
 *
 * Collapsed by default and never rendered as Markdown. Reasoning is a separate
 * channel on the wire precisely so it is not mistaken for the reply, and
 * giving it the same typography as the answer would undo that distinction at a
 * glance. It is monospace, muted, and behind a disclosure.
 *
 * The disclosure is a native `<details>`: the kit has no `Accordion` or
 * `Collapsible` primitive (docs/13 §4.12), and a native one is already
 * keyboard- and screen-reader-correct on every platform.
 */

import { Brain, ChevronRight } from 'lucide-react';

export type ReasoningBlockProps = {
  reasoning: string;
  /** Distinguishes several disclosures on one page for the accessible name. */
  messageId?: string;
};

export function ReasoningBlock({ reasoning, messageId }: ReasoningBlockProps) {
  const lines = reasoning.trim().split('\n').length;

  return (
    <details className="group rounded-md border border-border bg-surface-muted">
      <summary
        className="flex cursor-pointer list-none items-center gap-2 rounded-md px-3 py-1.5 text-xs text-muted-foreground transition-colors duration-fast hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        aria-label={messageId === undefined ? 'Reasoning' : `Reasoning for message ${messageId}`}
      >
        <ChevronRight
          aria-hidden="true"
          className="h-3.5 w-3.5 transition-transform duration-fast group-open:rotate-90"
        />
        <Brain aria-hidden="true" className="h-3.5 w-3.5" />
        <span className="font-medium">Reasoning</span>
        <span className="text-2xs">
          {lines} {lines === 1 ? 'line' : 'lines'}
        </span>
      </summary>
      <pre className="overflow-x-auto whitespace-pre-wrap break-words px-3 pb-3 pt-1 font-mono text-xs leading-relaxed text-muted-foreground">
        {reasoning}
      </pre>
    </details>
  );
}
