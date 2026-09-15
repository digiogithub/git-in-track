/**
 * The shared-state panel (task GIT-T-0077).
 *
 * AG-UI carries a JSON document beside the message stream: `STATE_SNAPSHOT`
 * publishes it, `STATE_DELTA` patches it (`internal/agui/state.go`). The
 * document belongs to the **thread**, not the run, so this renders whatever
 * the store last folded and is never cleared on `RUN_STARTED` — the plan and
 * the file list from the previous turn are still true at the start of the next
 * one.
 *
 * Every section is a native `<details>`: the kit ships no Accordion
 * (docs/13 §4.12) and a disclosure triangle is not worth a dependency. Empty
 * sections are omitted rather than rendered as empty headings, so a fresh
 * thread shows one line instead of five hollow ones.
 */

import { Bot, FileText, ListTodo, ShieldCheck, X } from 'lucide-react';
import type { ReactNode } from 'react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Progress } from '@/components/ui/progress';
import type { PandoState, PandoTodo } from '@/features/agent/types';

export type StatePanelProps = {
  state: PandoState | undefined;
  /** Tool names auto-approved for this thread; each one is revocable here. */
  alwaysAllowed?: readonly string[];
  onRevoke?: (toolName: string) => void;
};

const todoTone: Record<string, 'success' | 'info' | 'outline'> = {
  completed: 'success',
  in_progress: 'info',
  pending: 'outline',
};

const todoLabel: Record<string, string> = {
  completed: 'done',
  in_progress: 'doing',
  pending: 'to do',
};

function Section({
  title,
  icon,
  count,
  defaultOpen = true,
  children,
}: {
  title: string;
  icon: ReactNode;
  count?: number;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  return (
    <details open={defaultOpen} className="border-b border-border last:border-b-0">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs font-medium transition-colors duration-fast hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <span aria-hidden="true" className="text-muted-foreground">
          {icon}
        </span>
        <span className="min-w-0 flex-1 truncate">{title}</span>
        {count === undefined ? null : (
          <span className="text-2xs tabular-nums text-muted-foreground">{count}</span>
        )}
      </summary>
      <div className="px-3 pb-3">{children}</div>
    </details>
  );
}

function Todos({ todos }: { todos: PandoTodo[] }) {
  return (
    <ul className="space-y-1.5">
      {todos.map((todo, index) => (
        <li key={`${todo.content}-${String(index)}`} className="flex items-start gap-2 text-xs">
          <Badge variant={todoTone[todo.status] ?? 'outline'} size="sm" className="mt-0.5 shrink-0">
            {todoLabel[todo.status] ?? todo.status}
          </Badge>
          <span
            className={
              todo.status === 'completed'
                ? 'min-w-0 break-words text-muted-foreground line-through'
                : 'min-w-0 break-words'
            }
          >
            {todo.content}
          </span>
        </li>
      ))}
    </ul>
  );
}

export function StatePanel({ state, alwaysAllowed = [], onRevoke }: StatePanelProps) {
  if (state === undefined && alwaysAllowed.length === 0) {
    return (
      <div className="px-3 py-3">
        <p className="text-xs text-muted-foreground">
          The agent&rsquo;s plan, files and token usage will appear here once it starts working.
        </p>
      </div>
    );
  }

  const todos = state?.todos ?? [];
  const files = state?.files ?? [];
  const subAgents = state?.subAgents ?? [];
  const usage = state?.tokenUsage ?? null;
  const used = usage === null ? 0 : usage.promptTokens + usage.completionTokens;
  const budget = usage?.contextWindow ?? state?.model.contextWindow ?? 0;

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto" data-testid="agent-state-panel">
      {state === undefined ? null : (
        <div className="border-b border-border px-3 py-2">
          <p className="truncate text-xs font-medium" title={state.model.name ?? state.model.id}>
            {state.model.name === undefined || state.model.name === ''
              ? state.model.id
              : state.model.name}
          </p>
          <p className="truncate text-2xs text-muted-foreground">
            {state.agent}
            {state.model.provider === undefined || state.model.provider === ''
              ? ''
              : ` · ${state.model.provider}`}
          </p>
        </div>
      )}

      {usage === null ? null : (
        <Section title="Context" icon={<Bot className="h-3.5 w-3.5" />}>
          <Progress
            value={used}
            max={budget}
            label="Context window used"
            className="mb-2"
            indicatorClassName={budget > 0 && used / budget > 0.9 ? 'bg-destructive' : 'bg-accent'}
          />
          <dl className="grid grid-cols-2 gap-x-2 gap-y-1 text-2xs">
            <dt className="text-muted-foreground">Prompt</dt>
            <dd className="text-right tabular-nums">{usage.promptTokens.toLocaleString()}</dd>
            <dt className="text-muted-foreground">Completion</dt>
            <dd className="text-right tabular-nums">{usage.completionTokens.toLocaleString()}</dd>
            <dt className="text-muted-foreground">Window</dt>
            <dd className="text-right tabular-nums">{budget.toLocaleString()}</dd>
          </dl>
          {usage.estimated ? (
            <p className="mt-1 text-2xs text-muted-foreground">Estimated by the adapter.</p>
          ) : null}
        </Section>
      )}

      {todos.length === 0 ? null : (
        <Section title="Plan" icon={<ListTodo className="h-3.5 w-3.5" />} count={todos.length}>
          <Todos todos={todos} />
        </Section>
      )}

      {files.length === 0 ? null : (
        <Section title="Files" icon={<FileText className="h-3.5 w-3.5" />} count={files.length}>
          <ul className="space-y-1">
            {files.map((file) => (
              <li key={file.path} className="flex items-center gap-2 text-xs">
                <Badge variant="outline" size="sm" className="shrink-0">
                  {file.action}
                </Badge>
                <span className="min-w-0 truncate font-mono" title={file.path}>
                  {file.name}
                </span>
              </li>
            ))}
          </ul>
        </Section>
      )}

      {subAgents.length === 0 ? null : (
        <Section title="Sub-agents" icon={<Bot className="h-3.5 w-3.5" />} count={subAgents.length}>
          <ul className="space-y-1.5">
            {subAgents.map((sub) => (
              <li key={sub.id} className="text-xs">
                <div className="flex items-center gap-2">
                  <Badge variant="outline" size="sm" className="shrink-0">
                    {sub.status}
                  </Badge>
                  <span className="min-w-0 truncate font-mono">{sub.role ?? sub.id}</span>
                </div>
                {sub.summary === undefined ? null : (
                  <p className="mt-0.5 break-words text-muted-foreground">{sub.summary}</p>
                )}
              </li>
            ))}
          </ul>
        </Section>
      )}

      {alwaysAllowed.length === 0 ? null : (
        <Section
          title="Standing approvals"
          icon={<ShieldCheck className="h-3.5 w-3.5" />}
          count={alwaysAllowed.length}
        >
          <p className="mb-2 text-2xs text-muted-foreground">
            Approved for this conversation only, in memory. Gone on reload.
          </p>
          <ul className="space-y-1">
            {alwaysAllowed.map((toolName) => (
              <li key={toolName} className="flex items-center gap-1">
                <Badge variant="warning" size="sm" className="min-w-0">
                  <span className="truncate font-mono">{toolName}</span>
                </Badge>
                {onRevoke === undefined ? null : (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Revoke the standing approval for ${toolName}`}
                    onClick={() => onRevoke(toolName)}
                  >
                    <X aria-hidden="true" className="h-3 w-3" />
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </Section>
      )}
    </div>
  );
}
