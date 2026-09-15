/**
 * The interrupt slot — the seam, and now the dialogs behind it
 * (task GIT-T-0067, stories GIT-US-0061 and GIT-US-0064).
 *
 * A run parks when the agent calls a tool the browser owns. Three kinds arrive
 * through the same door and are told apart by name (`hitl.ts`):
 *
 * - `pando_permission_request` → {@link PermissionDialog}. **The security
 *   boundary.** Nothing is answered by default; see that file.
 * - `AskUserQuestion` → {@link QuestionDialog}.
 * - anything else → a frontend tool, which the store executes and resumes on
 *   its own (`features/agent/tools/`). Nothing is asked of the user, so this
 *   renders the honest minimum: what is pending, and a way out.
 *
 * Everything that closes a dialog resolves to an explicit answer: a denial for
 * a permission, a cancellation for a question. Pando reads anything else as a
 * refusal anyway (`internal/agui/hitl.go:145`), but a refusal the user never
 * saw is a run that looks frozen.
 *
 * The seam itself is unchanged: `AgentPage` takes a `renderInterrupt` of type
 * {@link AgentInterruptRenderer}, and {@link DefaultInterrupt} is what it
 * replaces.
 */

import { approve, deny } from '@pando-ai/sdk/agui/client';
import { CircleHelp } from 'lucide-react';
import type { ReactNode } from 'react';

import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import {
  buildQuestionAnswer,
  classifyHitl,
  refusalFor,
  type QuestionSelection,
} from '@/features/agent/hitl';
import type { AgentInterrupt } from '@/features/agent/types';
import { PermissionDialog } from '@/features/agent/ui/PermissionDialog';
import { QuestionDialog } from '@/features/agent/ui/QuestionDialog';

export type AgentInterruptRenderProps = {
  /** Every tool call the run is blocked on; usually exactly one. */
  interrupt: AgentInterrupt;
  /** Answers one call and lets the suspended run continue. */
  resume: (toolCallId: string, result: string) => Promise<void>;
  /** Abandons the turn. The transcript is kept. */
  cancel: () => Promise<void>;
  /** This tab may not run: render, but do not offer to answer. */
  readOnly: boolean;
  /** Tool names already granted for this thread, so the dialog can be skipped. */
  alwaysAllowed?: readonly string[];
  /** Records a per-thread grant. In memory only; never persisted. */
  allowAlways?: (toolName: string) => void;
};

export type AgentInterruptRenderer = (props: AgentInterruptRenderProps) => ReactNode;

/** The frontend-tool case: nothing to ask, but the turn is visibly parked. */
function PendingNotice({
  names,
  cancel,
  readOnly,
}: {
  names: string;
  cancel: () => Promise<void>;
  readOnly: boolean;
}) {
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

/**
 * Routes one interrupt to its dialog.
 *
 * Only the *first* HITL prompt is rendered: Pando parks on one call at a time
 * in practice, and stacking two modal dialogs would make the second one
 * unanswerable. The rest stay pending and surface as soon as this one is
 * answered.
 */
export function DefaultInterrupt({
  interrupt,
  resume,
  cancel,
  readOnly,
  allowAlways,
}: AgentInterruptRenderProps) {
  const prompts = interrupt.toolCalls.map((call) => ({ call, prompt: classifyHitl(call) }));
  const hitl = prompts.find((entry) => entry.prompt !== null);

  if (hitl?.prompt?.kind === 'permission') {
    const prompt = hitl.prompt;
    return (
      <PermissionDialog
        prompt={prompt}
        readOnly={readOnly}
        onApprove={({ always }) => {
          if (always) allowAlways?.(prompt.request.toolName);
          void resume(prompt.toolCallId, approve());
        }}
        onDeny={() => void resume(prompt.toolCallId, deny())}
      />
    );
  }

  if (hitl?.prompt?.kind === 'question') {
    const prompt = hitl.prompt;
    return (
      <QuestionDialog
        prompt={prompt}
        readOnly={readOnly}
        onAnswer={(selections: QuestionSelection[]) =>
          void resume(prompt.toolCallId, buildQuestionAnswer(prompt.questions, selections))
        }
        onCancel={() => void resume(prompt.toolCallId, refusalFor(prompt))}
      />
    );
  }

  // A malformed HITL payload is refused by the store before it ever reaches a
  // renderer; anything left here is a frontend tool mid-flight.
  const names = interrupt.toolCalls.map((call) => call.name).join(', ');
  return <PendingNotice names={names} cancel={cancel} readOnly={readOnly} />;
}
