/**
 * The two nothing-here states of the agent page.
 *
 * They are separate components because they are separate answers: one is "you
 * have not said anything yet", which is an invitation, and the other is "this
 * runtime has no agent", which is an explanation and must not look like a
 * feature that is merely empty.
 */

import { Bot, PlugZap } from 'lucide-react';

/** A conversation nobody has started. */
export function EmptyState() {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 py-12 text-center">
      <span
        aria-hidden="true"
        className="flex h-11 w-11 items-center justify-center rounded-full bg-accent-subtle text-accent"
      >
        <Bot className="h-5 w-5" />
      </span>
      <h2 className="text-base font-semibold">Ask about this workspace</h2>
      <p className="max-w-md text-sm text-muted-foreground">
        The agent reads the backlog and the knowledge base of this repository. It asks before it
        changes anything.
      </p>
    </div>
  );
}

/**
 * Browser-only mode, or a companion built without the agent routes. The
 * capability is the only thing branched on — never the provider kind — so this
 * is also what a companion whose adapter is not configured shows.
 */
export function AgentUnavailable() {
  return (
    <div className="mx-auto flex max-w-lg flex-col items-center gap-3 py-16 text-center">
      <span
        aria-hidden="true"
        className="flex h-11 w-11 items-center justify-center rounded-full bg-secondary text-muted-foreground"
      >
        <PlugZap className="h-5 w-5" />
      </span>
      <h1 className="text-base font-semibold">The agent is not available here</h1>
      <p className="text-sm text-muted-foreground">
        It needs a local Pando adapter, which only the companion can start and reach. Run{' '}
        <code className="rounded-sm bg-secondary px-1 py-0.5 font-mono text-xs">
          gintrack serve
        </code>{' '}
        and configure the agent to use it.
      </p>
    </div>
  );
}
