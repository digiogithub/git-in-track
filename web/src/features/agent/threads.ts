/**
 * One conversation: identity, persistence, the tab guard and the view model
 * (tasks GIT-T-0034 and GIT-T-0041).
 *
 * The reducer here is the SDK's. `PandoThread` owns the transcript, the
 * shared-state document, the tool-call index and the interrupt handoff, and
 * {@link AgentThread} composes it. What this module adds is what the SDK
 * deliberately does not do (its own doc comment says so): it never calls the
 * server's thread API, it has no notion of a run's status, and it reduces only
 * the subset of events a transcript needs. So, in order:
 *
 * - **Local:** thread-id persistence per repository, the `BroadcastChannel`
 *   ownership guard, the `AgentMessage` projection, the server-side thread
 *   calls (`hydrate`, `reattach`), and a supplementary fold for the three
 *   transcript-bearing events `PandoThread.reduce` has no case for
 *   (`MESSAGES_SNAPSHOT`, `TEXT_MESSAGE_CHUNK`, `ACTIVITY_SNAPSHOT`).
 * - **SDK:** everything else.
 */

import type {
  AguiEvent,
  AguiMessage,
  AguiMessageContentPart,
  AguiTool,
  PandoState,
  PendingToolCall,
} from '@pando-ai/sdk/agui/client';
import { PandoThread } from '@pando-ai/sdk/agui/client';

import type { DataProvider } from '@/api/provider';
import { asThreadClient, createAgentTransport } from '@/features/agent/client';
import type { AgentMessage, AgentToolCall, AgentToolCallStatus } from '@/features/agent/types';

// ------------------------------------------------------------- persistence

/** Where a repository's known thread ids live. */
export function threadStorageKey(repo: string): string {
  return `gintrack:agent-threads:${repo}`;
}

/** Where a repository's last active thread id lives. */
export function activeThreadStorageKey(repo: string): string {
  return `gintrack:agent-active-thread:${repo}`;
}

/**
 * `localStorage`, not `sessionStorage`: a thread id is not a credential, and
 * the whole point is that a conversation keeps its identity across a reload
 * and across a tab being closed and reopened. Every access is guarded —
 * private modes and sandboxed frames throw on the property itself.
 */
function readStorage(key: string): string | null {
  try {
    return globalThis.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function writeStorage(key: string, value: string | null): void {
  try {
    if (value === null) globalThis.localStorage?.removeItem(key);
    else globalThis.localStorage?.setItem(key, value);
  } catch {
    // Persistence is a convenience: a thread without it simply starts fresh.
  }
}

/** Thread ids known for a repository, newest first. */
export function readThreadIds(repo: string): string[] {
  const raw = readStorage(threadStorageKey(repo));
  if (raw === null) return [];
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((entry): entry is string => typeof entry === 'string');
  } catch {
    return [];
  }
}

/** Records a thread id, newest first, without duplicating it. */
export function rememberThreadId(repo: string, threadId: string): string[] {
  const ids = [threadId, ...readThreadIds(repo).filter((id) => id !== threadId)];
  writeStorage(threadStorageKey(repo), JSON.stringify(ids));
  return ids;
}

/** Drops a thread id, e.g. after the adapter forgot the thread. */
export function forgetThreadId(repo: string, threadId: string): string[] {
  const ids = readThreadIds(repo).filter((id) => id !== threadId);
  writeStorage(threadStorageKey(repo), JSON.stringify(ids));
  if (readActiveThreadId(repo) === threadId) writeActiveThreadId(repo, null);
  return ids;
}

export function readActiveThreadId(repo: string): string | null {
  return readStorage(activeThreadStorageKey(repo));
}

export function writeActiveThreadId(repo: string, threadId: string | null): void {
  writeStorage(activeThreadStorageKey(repo), threadId);
}

// ------------------------------------------------------------- tab guard

/** The messages the ownership channel carries. */
type OwnershipMessage =
  | { kind: 'claim'; threadId: string; tab: string }
  | { kind: 'owned'; threadId: string; tab: string }
  | { kind: 'release'; threadId: string; tab: string };

/** The slice of `BroadcastChannel` the guard uses, so tests can supply a fake. */
export type ChannelLike = {
  postMessage(message: unknown): void;
  close(): void;
  onmessage: ((event: { data: unknown }) => void) | null;
};

export type ThreadGuardOptions = {
  /** Defaults to a real `BroadcastChannel`, or to a no-op where there is none. */
  channelFactory?: (name: string) => ChannelLike | null;
  /** How long to wait for another tab to answer a claim. */
  timeoutMs?: number;
  /** Injected by tests so the wait is deterministic. */
  wait?: (ms: number) => Promise<void>;
  /** This tab's identity; generated when omitted. */
  tabId?: string;
};

/** Channel name; one for the whole app, threads are distinguished in the payload. */
export const THREAD_CHANNEL = 'gintrack:agent-threads';

function defaultChannelFactory(name: string): ChannelLike | null {
  const ctor = (globalThis as { BroadcastChannel?: new (name: string) => ChannelLike })
    .BroadcastChannel;
  if (typeof ctor !== 'function') return null;
  try {
    return new ctor(name);
  } catch {
    return null;
  }
}

/**
 * One live thread per browser tab.
 *
 * A live run is a hard Pando constraint — a second POST on a thread that
 * already has one is refused with `session_busy`, and dropping a stream
 * cancels the turn. Until park-on-disconnect and reattach land on the Pando
 * side (PANDO-EP-0003), the cheapest correct answer is for tabs to agree
 * among themselves: a tab announces the thread it wants, whoever already owns
 * it says so, and the newcomer falls back to read-only rather than racing.
 *
 * It is advisory, not a lock: a tab that never hears an answer takes
 * ownership. That is the right failure direction — worst case two tabs race
 * and the server refuses the second, which is exactly where we were before.
 */
export class ThreadGuard {
  readonly tabId: string;
  readonly #channel: ChannelLike | null;
  readonly #timeoutMs: number;
  readonly #wait: (ms: number) => Promise<void>;
  readonly #owned = new Set<string>();
  #answers = new Map<string, Set<string>>();

  constructor(options: ThreadGuardOptions = {}) {
    this.tabId = options.tabId ?? `tab-${Math.random().toString(36).slice(2, 10)}`;
    this.#timeoutMs = options.timeoutMs ?? 100;
    this.#wait =
      options.wait ??
      ((ms) =>
        new Promise((resolve) => {
          setTimeout(resolve, ms);
        }));
    const factory = options.channelFactory ?? defaultChannelFactory;
    this.#channel = factory(THREAD_CHANNEL);
    if (this.#channel) {
      this.#channel.onmessage = (event) => {
        this.#receive(event.data);
      };
    }
  }

  /** Threads this tab owns. */
  get owned(): string[] {
    return [...this.#owned];
  }

  /**
   * Asks for a thread. `true` means this tab owns it and may run; `false`
   * means another tab already does and this one is read-only.
   */
  async claim(threadId: string): Promise<boolean> {
    if (this.#owned.has(threadId)) return true;
    if (this.#channel === null) {
      this.#owned.add(threadId);
      return true;
    }
    this.#answers.set(threadId, new Set());
    this.#channel.postMessage({ kind: 'claim', threadId, tab: this.tabId });
    await this.#wait(this.#timeoutMs);
    const answers = this.#answers.get(threadId) ?? new Set<string>();
    this.#answers.delete(threadId);
    if (answers.size > 0) return false;
    this.#owned.add(threadId);
    return true;
  }

  /** Gives a thread up, so another tab can take it without waiting. */
  release(threadId: string): void {
    if (!this.#owned.delete(threadId)) return;
    this.#channel?.postMessage({ kind: 'release', threadId, tab: this.tabId });
  }

  /** Releases everything and closes the channel. */
  dispose(): void {
    for (const threadId of [...this.#owned]) this.release(threadId);
    this.#channel?.close();
  }

  #receive(data: unknown): void {
    const message = data as OwnershipMessage | null;
    if (!message || typeof message !== 'object' || typeof message.threadId !== 'string') return;
    if (message.tab === this.tabId) return;
    if (message.kind === 'claim') {
      // Someone wants a thread we hold: say so, and they stand down.
      if (this.#owned.has(message.threadId)) {
        this.#channel?.postMessage({
          kind: 'owned',
          threadId: message.threadId,
          tab: this.tabId,
        });
      }
      return;
    }
    if (message.kind === 'owned') {
      const answers = this.#answers.get(message.threadId);
      answers?.add(message.tab);
    }
  }
}

// ---------------------------------------------------------------- the thread

export type AgentThreadOptions = {
  provider: DataProvider;
  repo?: string;
  /** Reuse an existing id — a reload, or a row of the thread list. */
  threadId?: string;
  /** Agent name; the adapter's default when omitted. */
  agent?: string;
  /** Frontend tools declared on every run of this thread. */
  tools?: AguiTool[];
  onDroppedPatch?: (reason: string) => void;
};

export type AgentRunHandle = { signal?: AbortSignal };

/**
 * A conversation: the SDK's `PandoThread` plus the server-side thread API it
 * documents itself as not touching.
 */
export class AgentThread {
  readonly thread: PandoThread;
  readonly #provider: DataProvider;
  readonly #repo: string | undefined;
  /** Set only while `reattach` is driving the thread; see `client.ts`. */
  #override: (() => AsyncIterable<AguiEvent>) | null = null;

  constructor(options: AgentThreadOptions) {
    this.#provider = options.provider;
    this.#repo = options.repo;
    const transport = createAgentTransport({
      provider: options.provider,
      ...(options.repo === undefined ? {} : { repo: options.repo }),
      getState: () => this.thread.state,
      streamOverride: () => this.#override?.() ?? null,
      ...(options.onDroppedPatch === undefined
        ? {}
        : { onDroppedPatch: (_ops, reason) => options.onDroppedPatch?.(reason) }),
    });
    this.thread = new PandoThread({
      client: asThreadClient(transport),
      ...(options.threadId === undefined ? {} : { threadId: options.threadId }),
      ...(options.agent === undefined ? {} : { agent: options.agent }),
      ...(options.tools === undefined ? {} : { tools: options.tools }),
    });
  }

  get threadId(): string {
    return this.thread.threadId;
  }

  get state(): PandoState | undefined {
    return this.thread.state;
  }

  get isInterrupted(): boolean {
    return this.thread.isInterrupted;
  }

  get pendingToolCalls(): PendingToolCall[] {
    return this.thread.pendingToolCalls;
  }

  /** The transcript, projected for rendering. */
  view(): AgentMessage[] {
    return toAgentMessages(this.thread);
  }

  /** A new turn. The whole transcript goes back on the wire, as AG-UI requires. */
  async *send(prompt: string, handle: AgentRunHandle = {}): AsyncGenerator<AguiEvent> {
    yield* this.#drive(this.thread.send(prompt, signalOf(handle)));
  }

  /**
   * Answers an interrupt. The SDK appends the trailing `tool` message in
   * exactly the shape `TrailingToolMessages` requires, so the suspended run
   * re-attaches rather than a new one starting — which is why the transcript
   * continues instead of restarting.
   */
  async *resume(
    toolCallId: string,
    result: string,
    handle: AgentRunHandle = {},
  ): AsyncGenerator<AguiEvent> {
    yield* this.#drive(this.thread.resume(toolCallId, result, signalOf(handle)));
  }

  /**
   * Re-attaches to a run this thread started before a reload. It is the
   * server's stream, not a new run, so nothing is posted and no `runId` is
   * allocated; events fold into the transcript exactly as a run's would.
   */
  async *reattach(handle: AgentRunHandle = {}): AsyncGenerator<AguiEvent> {
    this.#override = () =>
      this.#provider.streamAgentThread(this.threadId, {
        ...(this.#repo === undefined ? {} : { repo: this.#repo }),
        ...(handle.signal === undefined ? {} : { signal: handle.signal }),
      });
    // `PandoThread.reduce` is private and only reachable by driving a run, so
    // the re-attached stream is fed through `send` with the transport swapped
    // out above. `send` pushes its prompt synchronously, so the placeholder is
    // removed before anything can observe it — and nothing is posted anyway,
    // because the override replaces the POST entirely.
    const stream = this.thread.send('', signalOf(handle));
    this.thread.messages.pop();
    try {
      yield* this.#drive(stream);
    } finally {
      this.#override = null;
    }
  }

  /**
   * Replaces the transcript with the adapter's stored one. This is how a
   * reload gets its history back: `PandoThread` is a pure client-side
   * reduction and remembers nothing across a page load.
   */
  async hydrate(handle: AgentRunHandle = {}): Promise<AguiMessage[]> {
    const messages = await this.#provider.getAgentThreadMessages(this.threadId, {
      ...(this.#repo === undefined ? {} : { repo: this.#repo }),
      ...(handle.signal === undefined ? {} : { signal: handle.signal }),
    });
    replaceMessages(this.thread, messages);
    return messages;
  }

  /** Folds the events the SDK's reducer skips, then passes each one on. */
  async *#drive(stream: AsyncGenerator<AguiEvent>): AsyncGenerator<AguiEvent> {
    for await (const event of stream) {
      supplementaryReduce(this.thread, event);
      yield event;
    }
  }
}

function signalOf(handle: AgentRunHandle): { signal?: AbortSignal } {
  return handle.signal === undefined ? {} : { signal: handle.signal };
}

/**
 * `PandoThread.messages` is `readonly` as a binding, not as an array: the SDK
 * mutates it in place itself. Replacing the contents keeps the identity the
 * SDK's internal tool-call index holds.
 */
function replaceMessages(thread: PandoThread, messages: AguiMessage[]): void {
  const target = thread.messages;
  target.splice(0, target.length, ...messages);
}

/**
 * The events `PandoThread.reduce` has no case for.
 *
 * It is not a parallel reducer: it never touches text, tool-call arguments,
 * state, reasoning or the interrupt flag — every one of those is the SDK's.
 * It covers the four the SDK's `default:` branch drops and the transcript
 * needs anyway.
 */
export function supplementaryReduce(thread: PandoThread, event: AguiEvent): void {
  switch (event.type) {
    case 'MESSAGES_SNAPSHOT':
      // A full replacement of the visible transcript, e.g. after the server
      // compacted the context.
      replaceMessages(thread, event.messages);
      return;
    case 'TEXT_MESSAGE_CHUNK': {
      // A whole message in one frame. Declared in `events.go` but not emitted
      // yet; handled so the day it is, text does not silently vanish.
      if (event.messageId === undefined || event.delta === undefined) return;
      const messages = thread.messages;
      const existing = messages.find((message) => message.id === event.messageId);
      if (existing) {
        const prior = typeof existing.content === 'string' ? existing.content : '';
        existing.content = prior + event.delta;
        return;
      }
      messages.push({
        id: event.messageId,
        role: (event.role as AguiMessage['role'] | undefined) ?? 'assistant',
        content: event.delta,
      });
      return;
    }
    case 'ACTIVITY_SNAPSHOT': {
      const messages = thread.messages;
      const content = JSON.stringify(event.content);
      const existing = messages.find((message) => message.id === event.messageId);
      if (existing && event.replace !== false) {
        existing.content = content;
        existing.activityType = event.activityType;
        return;
      }
      messages.push({
        id: event.messageId,
        role: 'activity',
        content,
        activityType: event.activityType,
      });
      return;
    }
    default:
      // Every other event either belongs to the SDK's reducer or carries
      // nothing the transcript needs. Unknown types fall here too, which is
      // what keeps a newer adapter from breaking this one.
      return;
  }
}

// ------------------------------------------------------------- view model

/** Flattens the two content shapes the protocol allows into one string. */
export function messageText(content: AguiMessage['content']): string {
  if (typeof content === 'string') return content;
  if (!Array.isArray(content)) return '';
  return content
    .map((part: AguiMessageContentPart) => (part.type === 'text' ? (part.text ?? '') : ''))
    .join('');
}

/** Projects a thread's transcript into the shape the chat UI renders. */
export function toAgentMessages(thread: PandoThread): AgentMessage[] {
  const results = new Map<string, string>();
  for (const message of thread.messages) {
    if (message.role === 'tool' && message.toolCallId !== undefined) {
      results.set(message.toolCallId, messageText(message.content));
    }
  }
  const pending = new Set(thread.pendingToolCalls.map((call) => call.id));

  return thread.messages.map((message) => {
    const reasoning = thread.reasoning.get(message.id);
    const toolCalls: AgentToolCall[] = (message.toolCalls ?? []).map((call) => {
      const result = results.get(call.id);
      const status: AgentToolCallStatus =
        result !== undefined ? 'done' : pending.has(call.id) ? 'pending' : 'streaming';
      return {
        id: call.id,
        name: call.function.name,
        argsText: call.function.arguments,
        args: safeParse(call.function.arguments),
        status,
        ...(result === undefined ? {} : { result }),
      };
    });
    return {
      id: message.id,
      role: message.role,
      text: messageText(message.content),
      toolCalls,
      ...(reasoning === undefined || reasoning === '' ? {} : { reasoning }),
      ...(message.toolCallId === undefined ? {} : { toolCallId: message.toolCallId }),
      ...(message.activityType === undefined ? {} : { activityType: message.activityType }),
      ...(message.error === undefined ? {} : { error: message.error }),
    };
  });
}

function safeParse(text: string): unknown {
  if (text === '') return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}
