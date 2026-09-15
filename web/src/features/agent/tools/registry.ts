/**
 * The frontend-tool registry (tasks GIT-T-0082 and GIT-T-0098).
 *
 * Two invariants hold this file together.
 *
 * **The declared toolset must be byte-stable across a thread.** Pando keys its
 * agent pool by agent name plus a hash of the declared tools
 * (`internal/agui/agentpool.go:54-77`), so a list rebuilt per message would
 * rebuild the agent per message. {@link toolDeclarations} therefore returns
 * one frozen array, built once at module load, never assembled per call and
 * never conditional on what the user is looking at.
 *
 * **A frontend tool may never shadow a HITL prompt.** `pando_permission_request`
 * and `AskUserQuestion` are how the *security boundary* reaches the browser; a
 * registry entry with either name would let the agent route an approval
 * through an executor that answers it. The assertion below runs at import
 * time, because a violation is a bug in this repository, not a runtime
 * condition to recover from.
 */

import type { AguiTool } from '@pando-ai/sdk/agui/client';

import { isHitlToolName } from '@/features/agent/hitl';
import { applyBacklogFilterTool, showItemsTool } from '@/features/agent/tools/backlog';
import {
  focusBoardCardTool,
  openItemTool,
  openKbPageTool,
} from '@/features/agent/tools/navigation';
import type { FrontendTool, ToolContext, ToolResult } from '@/features/agent/tools/types';

/** Every tool the browser implements, in declaration order. */
export const frontendTools: readonly FrontendTool[] = Object.freeze([
  openItemTool,
  openKbPageTool,
  focusBoardCardTool,
  applyBacklogFilterTool,
  showItemsTool,
]);

/** A frontend tool named like a HITL prompt would hijack the approval path. */
export function assertNoHitlShadowing(tools: readonly FrontendTool[]): void {
  const shadowed = tools.filter((tool) => isHitlToolName(tool.name)).map((tool) => tool.name);
  if (shadowed.length > 0) {
    throw new Error(
      `frontend tools may not shadow Pando's human-in-the-loop tools: ${shadowed.join(', ')}`,
    );
  }
}

assertNoHitlShadowing(frontendTools);

const declarations: readonly AguiTool[] = Object.freeze(
  frontendTools.map((tool) =>
    Object.freeze({
      name: tool.name,
      description: tool.description,
      parameters: tool.parameters,
    }),
  ),
);

/**
 * The array sent as `RunAgentInput.tools`. The same instance every time, so no
 * caller can accidentally make the toolset hash move.
 */
export function toolDeclarations(): readonly AguiTool[] {
  return declarations;
}

const byName = new Map(frontendTools.map((tool) => [tool.name, tool]));

/** True when this registry owns the tool the run is parked on. */
export function isFrontendTool(name: string): boolean {
  return byName.has(name);
}

/** Runs tools for one page: the declarations plus the context to execute in. */
export type FrontendToolRunner = {
  declarations: readonly AguiTool[];
  has: (name: string) => boolean;
  /** Always resolves, always with a JSON string: a parked run must never hang. */
  run: (name: string, args: unknown) => Promise<string>;
};

function encode(result: ToolResult): string {
  try {
    return JSON.stringify(result);
  } catch {
    return JSON.stringify({ error: 'tool_failed', message: 'The result could not be encoded.' });
  }
}

/**
 * Binds the registry to a page.
 *
 * Every failure mode ends in a result string, never a rejection: an unknown
 * name, arguments that fail validation and an executor that throws all come
 * back as `{error: …}` so the store can resume the run. A frontend tool that
 * threw would leave the agent suspended until Pando's ten-minute window
 * elapsed (`internal/agui/frontend_tool.go:41`), which reads to the user as
 * the app having frozen.
 */
export function createToolRunner(context: ToolContext): FrontendToolRunner {
  return {
    declarations,
    has: isFrontendTool,
    async run(name: string, args: unknown): Promise<string> {
      const tool = byName.get(name);
      if (tool === undefined) {
        return encode({
          error: 'unknown_tool',
          message: `This client implements no tool named ${name}.`,
          available: frontendTools.map((entry) => entry.name),
        });
      }
      const validated = tool.validate(args);
      if (!validated.ok) return encode(validated.result);
      try {
        return encode(await tool.execute(validated.value, context));
      } catch (error) {
        return encode({
          error: 'tool_failed',
          message: error instanceof Error ? error.message : String(error),
        });
      }
    },
  };
}
