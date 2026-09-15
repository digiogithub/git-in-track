/**
 * The interrupt slot — a seam, not a feature.
 *
 * A run parks when the agent calls a tool the browser owns: a permission
 * prompt, a question, a frontend tool. The dialogs that answer those are a
 * later wave (stories GIT-US-0061 and GIT-US-0064). Until they land, the page
 * still has to show that the turn is waiting and still has to let someone get
 * out of it, so this renders the honest minimum: what is pending, and Cancel.
 *
 * The seam is deliberately two-sided, so the wave that lands the dialogs need
 * not touch a single file in this directory:
 *
 * - `AgentPage` takes a `renderInterrupt` prop of type
 *   {@link AgentInterruptRenderer}; passing one replaces this entirely.
 * - `DefaultInterrupt` is the fallback when no renderer is passed; it can be
 *   re-pointed at the real dialogs in this one file.
 *
 * `resume` is the store's `resume(toolCallId, result)`, and the SDK's HITL
 * helpers (`approve`, `deny`, `answerQuestion`, `cancelQuestion`) build the
 * `result` string for it. That is the whole contract.
 */

import { CircleHelp } from 'lucide-react';
import type { ReactNode } from 'react';

import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import type { AgentInterrupt } from '@/features/agent/types';

export type AgentInterruptRenderProps = {
  /** Every tool call the run is blocked on; usually exactly one. */
  interrupt: AgentInterrupt;
  /** Answers one call and lets the suspended run continue. */
  resume: (toolCallId: string, result: string) => Promise<void>;
  /** Abandons the turn. The transcript is kept. */
  cancel: () => Promise<void>;
  /** This tab may not run: render, but do not offer to answer. */
  readOnly: boolean;
};

export type AgentInterruptRenderer = (props: AgentInterruptRenderProps) => ReactNode;

/** The placeholder the later wave replaces. */
export function DefaultInterrupt({ interrupt, cancel, readOnly }: AgentInterruptRenderProps) {
  const names = interrupt.toolCalls.map((call) => call.name).join(', ');

  return (
    <Card className="mx-auto w-full max-w-4xl border-warning/40 bg-warning/5" role="status">
      <div className="flex items-center gap-3 px-4 py-3">
        <CircleHelp aria-hidden="true" className="h-4 w-4 shrink-0 text-warning" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">The agent is waiting for an answer</p>
          {names === '' ? null : (
            <p className="truncate font-mono text-xs text-muted-foreground">{names}</p>
          )}
        </div>
        <Button variant="outline" size="sm" disabled={readOnly} onClick={() => void cancel()}>
          Cancel
        </Button>
      </div>
    </Card>
  );
}
