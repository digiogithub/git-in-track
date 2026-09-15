/**
 * The agent store (tasks GIT-T-0038 and GIT-T-0041).
 *
 * It is deliberately thin. The reducer is the SDK's `PandoThread`, wrapped by
 * `AgentThread`; this store owns only what a *store* has to own and the SDK
 * has no opinion about:
 *
 * - which thread this tab is on, and whether it owns it (the tab guard);
 * - the run's status, which the protocol expresses as a scatter of events;
 * - the abort controller behind `cancel()`;
 * - failures translated into something the UI can act on;
 * - the `AgentMessage` projection, recomputed as events land.
 *
 * Per `web/src/app/store.ts`, this holds conversation state only: no server
 * state that belongs in TanStack Query — the thread *list* is fetched here
 * only because it is part of restoring identity — and no navigational state.
 *
 * Reattach (PANDO-EP-0003) is already the shape of `reattach()`, so the day
 * park-on-disconnect lands the one-thread-per-tab guard can be deleted without
 * touching anything else here.
 */

import type {
  AguiEvent,
  PandoState,
  PendingToolCall,
  RunErrorEvent,
} from '@pando-ai/sdk/agui/client';
import type { StateCreator } from 'zustand';
import { create } from 'zustand';
import { createStore } from 'zustand/vanilla';

import type { AgentThreadSummary, DataProvider } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import {
  AgentThread,
  ThreadGuard,
  forgetThreadId,
  readActiveThreadId,
  readThreadIds,
  rememberThreadId,
  writeActiveThreadId,
} from '@/features/agent/threads';
import type {
  AgentError,
  AgentErrorCode,
  AgentInterrupt,
  AgentMessage,
  AgentRunStatus,
} from '@/features/agent/types';

/** What `attach` needs to bind the store to a provider and a repository. */
export type AgentAttachOptions = {
  provider: DataProvider;
  /** Repository whose adapter to run against; also the persistence scope. */
  repo: string;
  /** Agent name; the adapter's default when omitted. */
  agent?: string;
  /** Injected by tests so the tab guard is deterministic. */
  guard?: ThreadGuard;
};

export type AgentState = {
  provider: DataProvider | null;
  repo: string | null;
  /** The thread this tab is on; `null` before `attach`. */
  threadId: string | null;
  /** Thread ids persisted for this repository, newest first. */
  knownThreadIds: string[];
  /** The adapter's own thread list, when it has been fetched. */
  threads: AgentThreadSummary[];
  messages: AgentMessage[];
  /** The shared-state document; `undefined` until the first `STATE_SNAPSHOT`. */
  stateDoc: PandoState | undefined;
  runStatus: AgentRunStatus;
  interrupt: AgentInterrupt | null;
  error: AgentError | null;
  /**
   * Another tab already owns this thread. Everything renders; nothing runs.
   * Interim, until Pando can park a run on disconnect (PANDO-EP-0003).
   */
  readOnly: boolean;

  /** Live objects, not render state. Kept here so one store owns one thread. */
  thread: AgentThread | null;
  guard: ThreadGuard | null;
  controller: AbortController | null;

  attach(options: AgentAttachOptions): Promise<void>;
  /** Starts a conversation: a fresh thread id, an empty transcript. */
  newThread(): Promise<void>;
  /** Switches to a thread, restoring its transcript from the adapter. */
  selectThread(threadId: string): Promise<void>;
  send(prompt: string): Promise<void>;
  /** Answers an interrupt with a tool result built by the SDK's HITL helpers. */
  resume(toolCallId: string, result: string): Promise<void>;
  cancel(): Promise<void>;
  /** After a reload: restore the transcript, then re-attach to a live run. */
  reattach(): Promise<void>;
  refreshThreads(): Promise<void>;
  deleteThread(threadId: string): Promise<void>;
  dispose(): void;
};

// ---------------------------------------------------------------- failures

/** Translates a thrown failure into something the UI can act on. */
export function toAgentError(error: unknown): AgentError {
  if (error instanceof ProviderError) {
    const code: AgentErrorCode =
      error.code === 'not_supported'
        ? 'not_supported'
        : error.code === 'permission_denied'
          ? 'unauthorized'
          : 'transport';
    return { code, message: error.message };
  }
  if (isAbort(error)) {
    return { code: 'cancelled', message: 'The run was cancelled.' };
  }
  return {
    code: 'internal',
    message: error instanceof Error ? error.message : String(error),
  };
}

/** Translates a `RUN_ERROR` — a failure the agent reported over a live stream. */
export function toRunError(event: RunErrorEvent): AgentError {
  const code: AgentErrorCode =
    event.code === 'session_busy'
      ? 'session_busy'
      : event.code === 'cancelled'
        ? 'cancelled'
        : 'run_error';
  return { code, message: event.message };
}

function isAbort(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    'name' in error &&
    (error as { name?: unknown })['name'] === 'AbortError'
  );
}

// ------------------------------------------------------------------- store

const initialState = {
  provider: null,
  repo: null,
  threadId: null,
  knownThreadIds: [],
  threads: [],
  messages: [],
  stateDoc: undefined,
  runStatus: 'idle' as AgentRunStatus,
  interrupt: null,
  error: null,
  readOnly: false,
  thread: null,
  guard: null,
  controller: null,
};

export const agentStoreCreator: StateCreator<AgentState> = (set, get) => {
  /** Recomputes every derived field from the thread the SDK just updated. */
  function sync(): void {
    const thread = get().thread;
    if (thread === null) return;
    const pending: PendingToolCall[] = thread.pendingToolCalls;
    set({
      messages: thread.view(),
      stateDoc: thread.state,
      interrupt: thread.isInterrupted && pending.length > 0 ? { toolCalls: [...pending] } : null,
    });
  }

  /** Builds a thread object bound to the current provider and repository. */
  function makeThread(threadId?: string): AgentThread {
    const { provider, repo } = get();
    if (provider === null || repo === null) {
      throw new ProviderError('internal', 'The agent store was used before `attach`.');
    }
    return new AgentThread({
      provider,
      repo,
      ...(threadId === undefined ? {} : { threadId }),
    });
  }

  /**
   * Drives one run to its end.
   *
   * The status is decided here rather than by the reducer: `RUN_FINISHED`
   * without an outcome ends the turn, `outcome: 'interrupt'` parks it with the
   * pending tool calls readable, a `RUN_ERROR` is terminal, and so is a stream
   * that breaks mid-flight. An abort we asked for settles as `cancelled`, not
   * as an error, and never leaves a message half-open — the transcript keeps
   * whatever text arrived, which is what the user saw.
   */
  async function drive(stream: AsyncGenerator<AguiEvent>): Promise<void> {
    // The controller is the caller's: it was handed to the stream before this
    // ran, and `cancel()` reaches the run through it.
    set({ runStatus: 'running', error: null, interrupt: null });
    let runError: AgentError | null = null;
    let interrupted = false;
    try {
      for await (const event of stream) {
        if (event.type === 'RUN_ERROR') runError = toRunError(event);
        if (event.type === 'RUN_FINISHED') interrupted = event.outcome === 'interrupt';
        sync();
      }
    } catch (error) {
      sync();
      const mapped = get().controller?.signal.aborted === true ? null : toAgentError(error);
      set({
        controller: null,
        runStatus: mapped === null || mapped.code === 'cancelled' ? 'cancelled' : 'error',
        error: mapped !== null && mapped.code !== 'cancelled' ? mapped : null,
      });
      return;
    }
    sync();
    set({
      controller: null,
      runStatus: runError !== null ? 'error' : interrupted ? 'interrupted' : 'idle',
      error: runError,
    });
  }

  /** Refuses a run this tab does not own, or cannot make. */
  function blocked(): AgentError | null {
    const { thread, readOnly, provider } = get();
    if (provider === null || thread === null) {
      return { code: 'internal', message: 'The agent store was used before `attach`.' };
    }
    if (readOnly) {
      return {
        code: 'session_busy',
        message:
          'Another tab is already running this conversation. Open a new one to talk to the agent here.',
      };
    }
    return null;
  }

  /** Claims a thread, recording whether this tab may run it. */
  async function claim(threadId: string): Promise<void> {
    const guard = get().guard;
    const owned = guard === null ? true : await guard.claim(threadId);
    set({ readOnly: !owned });
  }

  return {
    ...initialState,

    async attach(options: AgentAttachOptions): Promise<void> {
      const guard = options.guard ?? new ThreadGuard();
      const knownThreadIds = readThreadIds(options.repo);
      const restored = readActiveThreadId(options.repo) ?? knownThreadIds[0];
      set({
        ...initialState,
        provider: options.provider,
        repo: options.repo,
        guard,
        knownThreadIds,
      });
      const thread = makeThread(restored);
      set({ thread, threadId: thread.threadId });
      writeActiveThreadId(options.repo, thread.threadId);
      set({ knownThreadIds: rememberThreadId(options.repo, thread.threadId) });
      await claim(thread.threadId);
    },

    async newThread(): Promise<void> {
      const { repo, guard, threadId } = get();
      if (repo === null) return;
      if (threadId !== null) guard?.release(threadId);
      const thread = makeThread();
      set({
        thread,
        threadId: thread.threadId,
        messages: [],
        stateDoc: undefined,
        runStatus: 'idle',
        interrupt: null,
        error: null,
        knownThreadIds: rememberThreadId(repo, thread.threadId),
      });
      writeActiveThreadId(repo, thread.threadId);
      await claim(thread.threadId);
    },

    async selectThread(threadId: string): Promise<void> {
      const { repo, guard, threadId: current } = get();
      if (repo === null || threadId === current) return;
      if (current !== null) guard?.release(current);
      const thread = makeThread(threadId);
      set({
        thread,
        threadId,
        messages: [],
        stateDoc: undefined,
        runStatus: 'idle',
        interrupt: null,
        error: null,
        knownThreadIds: rememberThreadId(repo, threadId),
      });
      writeActiveThreadId(repo, threadId);
      await claim(threadId);
      try {
        await thread.hydrate();
        sync();
      } catch (error) {
        set({ error: toAgentError(error) });
      }
    },

    async send(prompt: string): Promise<void> {
      const refusal = blocked();
      if (refusal !== null) {
        set({ error: refusal, runStatus: 'error' });
        return;
      }
      const thread = get().thread;
      if (thread === null) return;
      const controller = new AbortController();
      set({ controller });
      await drive(thread.send(prompt, { signal: controller.signal }));
    },

    async resume(toolCallId: string, result: string): Promise<void> {
      const refusal = blocked();
      if (refusal !== null) {
        set({ error: refusal, runStatus: 'error' });
        return;
      }
      const thread = get().thread;
      if (thread === null) return;
      const controller = new AbortController();
      set({ controller });
      await drive(thread.resume(toolCallId, result, { signal: controller.signal }));
    },

    async cancel(): Promise<void> {
      const { controller, provider, repo, threadId } = get();
      controller?.abort();
      set({ runStatus: 'cancelled', interrupt: null });
      if (provider === null || threadId === null) return;
      try {
        // Aborting the fetch only drops our end of the stream; the run itself
        // has to be told, or it keeps the thread busy.
        await provider.cancelAgentRun(threadId, ...(repo === null ? [] : [{ repo }]));
      } catch {
        // A run that already finished answers `not_found`; that is a cancel
        // that got what it wanted.
      }
    },

    async reattach(): Promise<void> {
      const { thread } = get();
      if (thread === null) return;
      try {
        await thread.hydrate();
        sync();
      } catch (error) {
        set({ error: toAgentError(error) });
        return;
      }
      const controller = new AbortController();
      set({ controller });
      await drive(thread.reattach({ signal: controller.signal }));
    },

    async refreshThreads(): Promise<void> {
      const { provider, repo } = get();
      if (provider === null) return;
      try {
        set({ threads: await provider.listAgentThreads(repo === null ? {} : { repo }) });
      } catch (error) {
        set({ error: toAgentError(error) });
      }
    },

    async deleteThread(threadId: string): Promise<void> {
      const { provider, repo } = get();
      if (provider === null) return;
      try {
        await provider.deleteAgentThread(threadId, repo === null ? {} : { repo });
      } catch (error) {
        set({ error: toAgentError(error) });
        return;
      }
      if (repo !== null) set({ knownThreadIds: forgetThreadId(repo, threadId) });
      set({ threads: get().threads.filter((row) => row.id !== threadId) });
      if (get().threadId === threadId) await get().newThread();
    },

    dispose(): void {
      const { controller, guard } = get();
      controller?.abort();
      guard?.dispose();
      set({ ...initialState });
    },
  };
};

/** The app's single agent store. */
export const useAgentStore = create<AgentState>(agentStoreCreator);

/** A fresh, independent store. Tests use this instead of resetting a singleton. */
export function createAgentStore() {
  return createStore<AgentState>(agentStoreCreator);
}
