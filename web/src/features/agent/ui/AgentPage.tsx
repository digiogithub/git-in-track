/**
 * The `/agent` page (story GIT-US-0057, tasks GIT-T-0047 and GIT-T-0062).
 *
 * Three columns: the conversations, the conversation, and a rail the shared-
 * state panel will move into (story GIT-US-0064). Below `lg` it collapses to
 * one — the rail disappears and the list becomes a panel behind a toggle,
 * because a phone has room for the conversation and nothing else.
 *
 * The page owns no conversation state. `useAgentStore` is the single source
 * for messages, run status, the interrupt and the read-only flag; what lives
 * here is which repository the agent runs against, the derived thread rows,
 * and the two seams later waves drop into (`renderInterrupt`, `renderRail`).
 *
 * On mount it attaches, lists the adapter's threads, and re-attaches to the
 * restored one *only when the adapter still knows it*. Re-attaching to a
 * thread id the server has never seen would fail on its own first request and
 * put an error banner on an empty page, which is the wrong first impression
 * for a conversation nobody has started yet.
 */

import { useQuery } from '@tanstack/react-query';
import { PanelLeft, TriangleAlert, X } from 'lucide-react';
import { useEffect, useMemo, useState, type ReactNode } from 'react';

import { useProvider } from '@/api/provider-context';
import { useAppStore } from '@/app/store';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import { useAgentStore } from '@/features/agent/store';
import { Composer } from '@/features/agent/ui/Composer';
import { AgentUnavailable, EmptyState } from '@/features/agent/ui/EmptyState';
import type { AgentInterruptRenderer } from '@/features/agent/ui/InterruptSlot';
import { DefaultInterrupt } from '@/features/agent/ui/InterruptSlot';
import { MessageList } from '@/features/agent/ui/MessageList';
import { mergeThreadRows } from '@/features/agent/ui/model';
import { ThreadList } from '@/features/agent/ui/ThreadList';
import {
  deriveThreadTitle,
  forgetThreadMeta,
  readThreadMeta,
  rememberThreadMeta,
  UNTITLED_THREAD,
  type AgentThreadMeta,
} from '@/features/agent/ui/threadMeta';
import { cn } from '@/lib/cn';

export type AgentPageProps = {
  /**
   * Replaces the placeholder shown while a run is parked on a question
   * (story GIT-US-0061). See `InterruptSlot.tsx` for the contract.
   */
  renderInterrupt?: AgentInterruptRenderer;
  /** Fills the right rail; the shared-state panel lands here (GIT-US-0064). */
  renderRail?: () => ReactNode;
};

/** Capability gate. Every branch is on `capabilities.agent`, never on the provider kind. */
export function AgentPage(props: AgentPageProps) {
  const enabled = useAppStore((state) => state.capabilities.agent);
  if (!enabled) return <AgentUnavailable />;
  return <AgentChat {...props} />;
}

function AgentChat({ renderInterrupt, renderRail }: AgentPageProps) {
  const provider = useProvider();
  const repos = useQuery({ queryKey: ['repos'], queryFn: () => provider.listRepos() });
  const [repoId, setRepoId] = useState<string | null>(null);

  const rows = useMemo(() => repos.data ?? [], [repos.data]);
  const repo = repoId ?? rows[0]?.id ?? null;

  const threadId = useAgentStore((state) => state.threadId);
  const knownThreadIds = useAgentStore((state) => state.knownThreadIds);
  const threads = useAgentStore((state) => state.threads);
  const messages = useAgentStore((state) => state.messages);
  const runStatus = useAgentStore((state) => state.runStatus);
  const interrupt = useAgentStore((state) => state.interrupt);
  const error = useAgentStore((state) => state.error);
  const readOnly = useAgentStore((state) => state.readOnly);

  const [meta, setMeta] = useState<AgentThreadMeta[]>([]);
  const [listOpen, setListOpen] = useState(false);
  const [dismissed, setDismissed] = useState<string | null>(null);

  // Attach, list, and re-attach — in that order, and once per repository. The
  // cleanup disposes the store, which aborts any run this page started: a page
  // that is gone must not keep a thread busy.
  useEffect(() => {
    if (repo === null) return;
    let live = true;
    void (async () => {
      const store = useAgentStore.getState();
      await store.attach({ provider, repo });
      if (!live) return;
      setMeta(readThreadMeta(repo));
      await useAgentStore.getState().refreshThreads();
      if (!live) return;
      const current = useAgentStore.getState();
      const known = current.threads.some((row) => row.id === current.threadId);
      if (known) await current.reattach();
    })();
    return () => {
      live = false;
      useAgentStore.getState().dispose();
    };
  }, [provider, repo]);

  // The title is derived, so it is recorded wherever the transcript came from:
  // a turn just sent, a thread switch that hydrated, a re-attach after a
  // reload. Writing the same title twice is a no-op, so this stays cheap.
  const derivedTitle = deriveThreadTitle(messages);
  useEffect(() => {
    if (repo === null || threadId === null || derivedTitle === UNTITLED_THREAD) return;
    setMeta((previous) => {
      if (previous.some((row) => row.id === threadId && row.title === derivedTitle)) {
        return previous;
      }
      return rememberThreadMeta(repo, {
        id: threadId,
        title: derivedTitle,
        updatedAt: new Date().toISOString(),
      });
    });
  }, [repo, threadId, derivedTitle]);

  const threadRows = useMemo(
    () => mergeThreadRows(knownThreadIds, threads, meta),
    [knownThreadIds, threads, meta],
  );

  async function onSend(prompt: string): Promise<void> {
    await useAgentStore.getState().send(prompt);
    await useAgentStore.getState().refreshThreads();
  }

  async function onSelect(id: string): Promise<void> {
    setListOpen(false);
    await useAgentStore.getState().selectThread(id);
  }

  async function onNew(): Promise<void> {
    setListOpen(false);
    await useAgentStore.getState().newThread();
  }

  async function onDelete(id: string): Promise<void> {
    await useAgentStore.getState().deleteThread(id);
    if (repo !== null) setMeta(forgetThreadMeta(repo, id));
  }

  const banner = error !== null && error.message !== dismissed ? error : null;
  const interruptSlot = renderInterrupt ?? DefaultInterrupt;

  return (
    <div className="flex h-[calc(100dvh-6rem)] min-h-[32rem] gap-4">
      {/* Conversations. A panel on a phone, a column from `lg` up. */}
      <aside
        className={cn(
          'w-64 shrink-0 rounded-lg border border-border bg-surface lg:block',
          listOpen ? 'block' : 'hidden',
        )}
      >
        <ThreadList
          rows={threadRows}
          activeId={threadId}
          onNew={() => void onNew()}
          onSelect={(id) => void onSelect(id)}
          onDelete={(id) => void onDelete(id)}
        />
      </aside>

      <section
        aria-label="Agent conversation"
        className={cn(
          'flex min-w-0 flex-1 flex-col rounded-lg border border-border bg-surface',
          listOpen && 'hidden lg:flex',
        )}
      >
        <header className="flex items-center gap-2 border-b border-border px-4 py-2.5">
          <Button
            variant="ghost"
            size="icon-sm"
            className="lg:hidden"
            aria-label={listOpen ? 'Hide conversations' : 'Show conversations'}
            aria-expanded={listOpen}
            onClick={() => setListOpen((open) => !open)}
          >
            <PanelLeft aria-hidden="true" className="h-4 w-4" />
          </Button>
          <h1 className="min-w-0 flex-1 truncate text-sm font-semibold">
            {threadRows.find((row) => row.id === threadId)?.title ?? UNTITLED_THREAD}
          </h1>
          {rows.length > 1 ? (
            <Select
              aria-label="Repository"
              value={repo ?? ''}
              className="h-8 w-44 text-xs"
              onChange={(event) => setRepoId(event.target.value)}
            >
              {rows.map((row) => (
                <option key={row.id} value={row.id}>
                  {row.name}
                </option>
              ))}
            </Select>
          ) : null}
        </header>

        {banner === null ? null : (
          <div
            role="alert"
            className="flex items-start gap-2 border-b border-border bg-destructive/10 px-4 py-2 text-sm text-destructive"
          >
            <TriangleAlert aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
            <p className="min-w-0 flex-1">{banner.message}</p>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Dismiss the error"
              onClick={() => setDismissed(banner.message)}
            >
              <X aria-hidden="true" className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}

        {repo === null ? (
          <p className="flex-1 px-6 py-12 text-center text-sm text-muted-foreground">
            {repos.isPending
              ? 'Loading the workspace…'
              : 'Open a repository first: the agent runs against one repository at a time.'}
          </p>
        ) : messages.length === 0 ? (
          <div className="min-h-0 flex-1 overflow-y-auto">
            <EmptyState />
          </div>
        ) : (
          <MessageList messages={messages} runStatus={runStatus} />
        )}

        {interrupt === null ? null : (
          <div className="px-4 pb-2">
            {interruptSlot({
              interrupt,
              resume: (toolCallId, result) => useAgentStore.getState().resume(toolCallId, result),
              cancel: () => useAgentStore.getState().cancel(),
              readOnly,
            })}
          </div>
        )}

        <Composer
          onSend={onSend}
          onStop={() => useAgentStore.getState().cancel()}
          running={runStatus === 'running'}
          interrupted={interrupt !== null}
          readOnly={readOnly}
          disabled={repo === null}
        />
      </section>

      {/* The rail. Empty until the shared-state panel lands (GIT-US-0064). */}
      <aside
        aria-label="Agent state"
        className="hidden w-72 shrink-0 rounded-lg border border-border bg-surface xl:block"
      >
        {renderRail === undefined ? (
          <p className="px-3 py-3 text-xs text-muted-foreground">
            The agent&rsquo;s plan, files and token usage will appear here.
          </p>
        ) : (
          renderRail()
        )}
      </aside>
    </div>
  );
}
