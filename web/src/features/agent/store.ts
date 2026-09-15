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
import { approve } from '@pando-ai/sdk/agui/client';
import type { StateCreator } from 'zustand';
import { create } from 'zustand';
import { createStore } from 'zustand/vanilla';

import type { AgentThreadSummary, DataProvider } from '@/api/provider';
import { ProviderError } from '@/api/provider';
import { classifyHitl, permissionSubject, refusalFor } from '@/features/agent/hitl';
import {
  AgentThread,
  ThreadGuard,
  forgetThreadId,
  readActiveThreadId,
  readThreadIds,
  rememberThreadId,
  writeActiveThreadId,
} from '@/features/agent/threads';
import type { FrontendToolRunner } from '@/features/agent/tools/registry';
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
  /** Frontend tools declared on every run; see `features/agent/tools/`. */
  tools?: FrontendToolRunner;
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
  /**
   * Tool names a permission prompt may auto-approve for the rest of this
   * thread. In memory only, and dropped on every thread switch: a grant that
   * outlived a reload would be a standing cross-session permission, which is a
   * security decision needing its own ADR (story GIT-US-0061).
   */
  alwaysAllowed: string[];

  /** Live objects, not render state. Kept here so one store owns one thread. */
  thread: AgentThread | null;
  guard: ThreadGuard | null;
  controller: AbortController | null;
  /** The frontend tools this page implements, or `null` before `registerTools`. */
  runner: FrontendToolRunner | null;

  attach(options: AgentAttachOptions): Promise<void>;
  /** Starts a conversation: a fresh thread id, an empty transcript. */
  newThread(): Promise<void>;
  /** Switches to a thread, restoring its transcript from the adapter. */
  selectThread(threadId: string): Promise<void>;
  send(prompt: string): Promise<void>;
  /** Answers an interrupt with a tool result built by the SDK's HITL helpers. */
  resume(toolCallId: string, result: string): Promise<void>;
  cancel(): Promise<void>;
  /**
   * Restores the transcript from the adapter without opening a stream. This is
   * what a page does on mount for a thread that is not running: `reattach`
   * would additionally subscribe to a run that is not there.
   */
  hydrate(): Promise<void>;
  /** After a reload: restore the transcript, then re-attach to a live run. */
  reattach(): Promise<void>;
  /** Declares the browser's own tools; pass `null` to withdraw them. */
  registerTools(runner: FrontendToolRunner | null): void;
  /** Auto-approves later permission prompts for `toolName` in this thread. */
  allowToolForThread(toolName: string): void;
  /** Revokes a grant made by {@link allowToolForThread}. */
  revokeToolForThread(toolName: string): void;
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

/** A tool call's arguments, when the SDK could not parse them itself. */
function parseArgs(argsText: string): unknown {
  if (argsText.trim() === '') return {};
  try {
    return JSON.parse(argsText);
  } catch {
    return undefined;
  }
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
  alwaysAllowed: [],
  runner: null,
  thread: null,
  guard: null,
  controller: null,
};

export const agentStoreCreator: StateCreator<AgentState> = (set, get) => {
  /** Tool calls already answered without a human, so none is answered twice. */
  const settled = new Set<string>();

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
    if (interrupted && runError === null) await settle();
  }

  /**
   * Answers the parts of an interrupt that need no human.
   *
   * Two cases, and only two. A tool this browser implements is executed and
   * its result resumes the run — that is the whole frontend-tool protocol
   * (story GIT-US-0064), and it has to be automatic or a navigation the agent
   * asked for would sit behind a button nobody knows to press. A permission
   * prompt for a tool the user granted "always" in this thread is approved.
   *
   * Everything else is left parked for a dialog, which is the point: the
   * approval card is the security boundary, so nothing may answer it by
   * default (story GIT-US-0061).
   *
   * `resume` re-enters `drive`, which re-enters this — that is how a chain of
   * frontend tool calls unwinds. `settled` stops a call that somehow arrives
   * twice from looping.
   */
  async function settle(): Promise<void> {
    const { interrupt, alwaysAllowed, runner, readOnly } = get();
    if (interrupt === null || readOnly) return;
    for (const call of interrupt.toolCalls) {
      if (settled.has(call.id)) continue;
      const prompt = classifyHitl(call);
      if (prompt === null) {
        // Not a HITL prompt: a frontend tool, or a name nobody implements.
        // Either way the runner answers — including for a name it does not
        // know, because a parked run must never hang. With no runner at all
        // (a headless store) the call stays pending and the UI says so.
        if (runner === null) continue;
        settled.add(call.id);
        const result = await runner.run(call.name, call.args ?? parseArgs(call.argsText));
        await get().resume(call.id, result);
        return;
      }
      if (prompt.kind === 'malformed') {
        settled.add(call.id);
        await get().resume(call.id, refusalFor(prompt));
        return;
      }
      if (
        prompt.kind === 'permission' &&
        alwaysAllowed.includes(permissionSubject(prompt.request))
      ) {
        settled.add(call.id);
        await get().resume(call.id, approve());
        return;
      }
    }
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
        ...(options.tools === undefined ? {} : { runner: options.tools }),
      });
      settled.clear();
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
      settled.clear();
      const thread = makeThread();
      set({
        thread,
        threadId: thread.threadId,
        messages: [],
        stateDoc: undefined,
        runStatus: 'idle',
        interrupt: null,
        error: null,
        alwaysAllowed: [],
        knownThreadIds: rememberThreadId(repo, thread.threadId),
      });
      writeActiveThreadId(repo, thread.threadId);
      await claim(thread.threadId);
    },

    async selectThread(threadId: string): Promise<void> {
      const { repo, guard, threadId: current } = get();
      if (repo === null || threadId === current) return;
      if (current !== null) guard?.release(current);
      settled.clear();
      const thread = makeThread(threadId);
      set({
        thread,
        threadId,
        messages: [],
        stateDoc: undefined,
        runStatus: 'idle',
        interrupt: null,
        error: null,
        alwaysAllowed: [],
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
      const tools = get().runner?.declarations;
      await drive(
        thread.send(prompt, {
          signal: controller.signal,
          ...(tools === undefined ? {} : { tools: [...tools] }),
        }),
      );
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
      const tools = get().runner?.declarations;
      await drive(
        thread.resume(toolCallId, result, {
          signal: controller.signal,
          ...(tools === undefined ? {} : { tools: [...tools] }),
        }),
      );
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

    async hydrate(): Promise<void> {
      const thread = get().thread;
      if (thread === null) return;
      try {
        await thread.hydrate();
        sync();
      } catch (error) {
        set({ error: toAgentError(error) });
      }
    },

    registerTools(runner: FrontendToolRunner | null): void {
      set({ runner });
    },

    allowToolForThread(toolName: string): void {
      const current = get().alwaysAllowed;
      if (current.includes(toolName)) return;
      set({ alwaysAllowed: [...current, toolName] });
    },

    revokeToolForThread(toolName: string): void {
      set({ alwaysAllowed: get().alwaysAllowed.filter((name) => name !== toolName) });
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
      settled.clear();
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
