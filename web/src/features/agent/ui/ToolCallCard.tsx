/**
 * One tool call, as a collapsed card (task GIT-T-0053).
 *
 * A turn can contain a dozen of these and none of them is the answer, so the
 * closed state is the design: name, status, nothing else. Arguments and the
 * result are escaped text inside a `<pre>` — never Markdown, never HTML. A
 * tool result is the least trustworthy string on the page (it is whatever a
 * command printed), and the one place it must not be able to become markup.
 *
 * The disclosure is a native `<details>` rather than a Radix `Collapsible`:
 * the kit ships neither `Accordion` nor `Collapsible` (docs/13 §4.12), and
 * adding a package for a triangle is not a trade worth making.
 */

import { Check, ChevronRight, Loader2, Wrench } from 'lucide-react';
import { useState } from 'react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import type { AgentToolCall, AgentToolCallStatus } from '@/features/agent/types';

/**
 * How much of a result is shown before the clamp. Roughly a screenful of dense
 * output: enough that the common case is never truncated, small enough that a
 * `cat` of a large file cannot push the composer off the screen.
 */
export const RESULT_CLAMP = 4096;

export type ToolCallCardProps = {
  call: AgentToolCall;
};

const statusLabel: Record<AgentToolCallStatus, string> = {
  streaming: 'Calling',
  pending: 'Waiting',
  done: 'Done',
};

function StatusBadge({ status }: { status: AgentToolCallStatus }) {
  if (status === 'done') {
    return (
      <Badge variant="success" size="sm">
        <Check aria-hidden="true" />
        {statusLabel.done}
      </Badge>
    );
  }
  return (
    <Badge variant={status === 'pending' ? 'warning' : 'info'} size="sm">
      <Loader2 aria-hidden="true" className="animate-spin" />
      {statusLabel[status]}
    </Badge>
  );
}

/** Arguments as the agent meant them, or the raw stream while it is still partial. */
function prettyArgs(call: AgentToolCall): string {
  if (call.args === undefined) return call.argsText;
  try {
    return JSON.stringify(call.args, null, 2);
  } catch {
    return call.argsText;
  }
}

export function ToolCallCard({ call }: ToolCallCardProps) {
  const [expanded, setExpanded] = useState(false);
  const args = prettyArgs(call);
  const result = call.result ?? '';
  const clamped = result.length > RESULT_CLAMP && !expanded;
  const shown = clamped ? result.slice(0, RESULT_CLAMP) : result;

  return (
    <details
      className="group rounded-md border border-border bg-card"
      data-testid={`tool-call-${call.id}`}
    >
      <summary className="flex cursor-pointer list-none items-center gap-2 rounded-md px-3 py-2 text-xs transition-colors duration-fast hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <ChevronRight
          aria-hidden="true"
          className="h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform duration-fast group-open:rotate-90"
        />
        <Wrench aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-mono font-medium">{call.name}</span>
        <StatusBadge status={call.status} />
      </summary>

      <div className="space-y-3 px-3 pb-3 pt-1">
        <section aria-label={`Arguments of ${call.name}`}>
          <h4 className="section-label mb-1">Arguments</h4>
          <pre
            data-testid="tool-args"
            className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-sm bg-surface-muted p-2 font-mono text-xs leading-relaxed"
          >
            {args === '' ? '(none)' : args}
          </pre>
        </section>

        {call.result === undefined ? null : (
          <section aria-label={`Result of ${call.name}`}>
            <h4 className="section-label mb-1">Result</h4>
            <pre
              data-testid="tool-result"
              className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-sm bg-surface-muted p-2 font-mono text-xs leading-relaxed"
            >
              {shown}
              {clamped ? '\n…' : null}
            </pre>
            {result.length > RESULT_CLAMP ? (
              <Button
                variant="ghost"
                size="sm"
                className="mt-1"
                aria-expanded={expanded}
                onClick={() => setExpanded((open) => !open)}
              >
                {expanded
                  ? 'Show less'
                  : `Show more (${(result.length / 1024).toFixed(1)} kB total)`}
              </Button>
            ) : null}
          </section>
        )}
      </div>
    </details>
  );
}
