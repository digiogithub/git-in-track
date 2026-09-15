/**
 * `show_items` rendered inline in the transcript (task GIT-T-0094).
 *
 * The cards come off the *tool result* rather than from a fresh query: the
 * executor already resolved the ids through the page's lookup, which reads the
 * TanStack Query cache, so by the time this renders the data is on the message
 * and no card costs a request. That also makes the block stable — scrolling
 * back through a conversation shows what the agent showed then, not what the
 * backlog looks like now.
 *
 * Ids the agent got wrong are reported back to it in the same result and shown
 * here as a muted note, because "three of the four exist" is information the
 * reader needs as much as the model does.
 */

import { useMemo } from 'react';

import { parseShowItemsResult, type ShowItemsResult } from '@/features/agent/tools/backlog';
import type { AgentToolCall } from '@/features/agent/types';
import { ItemLink, PriorityBadge, StatusBadge, TypeBadge } from '@/features/backlog/Badges';
import { projectKeyOf } from '@/features/backlog/item-meta';
import { useProject } from '@/features/backlog/queries';

export type ItemCardsProps = {
  result: ShowItemsResult;
  /** Project key used when an id carries no prefix of its own. */
  fallbackProject: string;
};

export function ItemCards({ result, fallbackProject }: ItemCardsProps) {
  const projectKey = projectKeyOf(result.items[0]?.id ?? '', fallbackProject);
  const project = useProject(projectKey);

  if (result.items.length === 0 && result.unresolved.length === 0) return null;

  return (
    <div className="space-y-1.5" data-testid="agent-item-cards">
      {result.items.map((item) => (
        <article
          key={item.id}
          className="rounded-md border border-border bg-card px-3 py-2"
          aria-label={`${item.id} ${item.title}`}
        >
          <div className="flex min-w-0 items-center gap-2">
            <ItemLink project={projectKeyOf(item.id, fallbackProject)} id={item.id} />
            <span className="min-w-0 flex-1 truncate text-sm font-medium">{item.title}</span>
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
            <TypeBadge type={item.type} />
            <StatusBadge status={item.status} project={project.data} />
            <PriorityBadge priority={item.priority} />
          </div>
        </article>
      ))}

      {result.unresolved.length === 0 ? null : (
        <p className="text-xs text-muted-foreground">
          Not in this workspace: <span className="font-mono">{result.unresolved.join(', ')}</span>
        </p>
      )}
    </div>
  );
}

/**
 * The transcript's hook for frontend-tool results: everything but `show_items`
 * renders as nothing here and falls through to the default `ToolCallCard`.
 */
export function AgentToolResult({
  call,
  fallbackProject,
}: {
  call: AgentToolCall;
  fallbackProject: string;
}) {
  const result = useMemo(
    () =>
      call.name === 'show_items' && call.result !== undefined
        ? parseShowItemsResult(call.result)
        : null,
    [call.name, call.result],
  );
  if (result === null) return null;
  return <ItemCards result={result} fallbackProject={fallbackProject} />;
}
