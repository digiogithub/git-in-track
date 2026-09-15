/**
 * The AG-UI transport, as `PandoThread` wants to see it (task GIT-T-0027).
 *
 * ## Why this is not a bare `PandoAguiClient`
 *
 * The SDK ships `PandoAguiClient`, and it does exactly the right thing against
 * a Pando adapter you can talk to directly: `{baseUrl}{path}/{agent}`, bearer
 * token, `parseSSE`. Two facts about this app make instantiating it here the
 * wrong move:
 *
 * 1. **We never talk to Pando.** One `agui-serve` runs per repository and the
 *    companion routes `?repo=<id>` onto `{url, token}` of its own; the Pando
 *    token must never reach the browser (decision of 2026-09-13).
 *    `PandoAguiClient.agentUrl()` builds `{base}{path}/{agent}` with no room
 *    for a query string, so the repository selector has nowhere to go.
 * 2. **Features do not `fetch`.** Every call in this app goes through
 *    `DataProvider`, which owns the base URL, the companion bearer header and
 *    the RFC 7807 → `ProviderError` mapping. A client that held its own
 *    `fetch` would be a second, differently-behaved transport next to it.
 *
 * So the provider *is* the transport (`CompanionProvider.runAgent` POSTs and
 * hands back the SDK's own `parseSSE` output), and this module is the thin
 * shim that presents it with the one method `PandoThread` calls. Nothing here
 * re-implements the protocol: no SSE parsing, no reducer, no HITL answers —
 * those are all the SDK's, used as they ship.
 *
 * The cast to `PandoAguiClient` at the end is the one unavoidable wart:
 * `PandoThreadOptions.client` is typed as the class, and a class with private
 * fields cannot be matched structurally. The shim implements the whole surface
 * `PandoThread` actually touches (`run`), which is checked by
 * {@link AgentTransport}.
 */

import type {
  AguiEvent,
  AguiRunOptions,
  JsonPatchOperation,
  PandoAguiClient,
  PandoState,
  RunAgentInput,
} from '@pando-ai/sdk/agui/client';
import { applyJsonPatch } from '@pando-ai/sdk/agui/client';

import type { DataProvider } from '@/api/provider';

/** The slice of `PandoAguiClient` that `PandoThread` actually calls. */
export type AgentTransport = {
  run(options: AguiRunOptions): AsyncGenerator<AguiEvent, void, undefined>;
};

export type AgentTransportOptions = {
  provider: DataProvider;
  /** Which repository's adapter to run against. */
  repo?: string;
  /**
   * Reads the thread's current state document. It is a callback rather than a
   * value because the guard below runs on every event and the thread is built
   * *from* this transport, so neither can hold the other at construction.
   */
  getState?: () => PandoState | undefined;
  /**
   * Returns a stream to use *instead of* posting a run.
   *
   * It exists for one case: re-attaching to a run that is already live. That
   * stream comes from `GET /threads/{id}/stream`, not from a POST, but it has
   * to be reduced by the very same `PandoThread` — and the SDK's `reduce` is
   * private, reachable only by driving `client.run`. So the thread drives a
   * run as usual and this hook swaps the transport underneath it. Returning
   * `null` (the default) means "post normally".
   */
  streamOverride?: () => AsyncIterable<AguiEvent> | null;
  /**
   * Called when a `STATE_DELTA` is dropped. Defaults to a console warning.
   * Tests pass their own so the assertion is on a spy, not on stdout.
   */
  onDroppedPatch?: (ops: JsonPatchOperation[], reason: string) => void;
};

/**
 * Builds the wire body from the SDK's friendlier run options.
 *
 * `PandoAguiClient` has a private `buildInput` doing the same six lines; it
 * cannot be reused from outside the class, and the mapping is part of the
 * protocol's published shape (`RunAgentInput`), not a reducer we are
 * duplicating. Ids are always supplied by `PandoThread`, which owns thread and
 * run identity — the fallbacks exist only so a direct caller cannot post a
 * body the adapter would reject.
 */
export function buildRunInput(options: AguiRunOptions): RunAgentInput {
  const messages =
    options.messages ??
    (options.prompt === undefined
      ? []
      : [{ id: randomLocalId('msg'), role: 'user' as const, content: options.prompt }]);
  const input: RunAgentInput = {
    threadId: options.threadId ?? randomLocalId('thread'),
    runId: options.runId ?? randomLocalId('run'),
    messages,
  };
  if (options.tools?.length) input.tools = options.tools;
  if (options.context?.length) input.context = options.context;
  if (options.state !== undefined) input.state = options.state;
  if (options.parentRunId !== undefined) input.parentRunId = options.parentRunId;
  // Pando decodes `forwardedProps` and drops it; per-request context that must
  // actually reach the prompt travels in `context[]`. Passed through anyway so
  // a future adapter can read it.
  if (options.forwardedProps !== undefined) input.forwardedProps = options.forwardedProps;
  return input;
}

/** Same shape as the SDK's `randomId`, without importing a runtime symbol for it. */
function randomLocalId(prefix: string): string {
  const uuid = globalThis.crypto?.randomUUID?.();
  if (uuid) return `${prefix}-${uuid}`;
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

/**
 * Would these operations apply cleanly to `state`?
 *
 * `PandoThread.applyStateDelta` throws on a patch it cannot apply, and on any
 * `STATE_DELTA` that arrives before the first `STATE_SNAPSHOT`. A throw there
 * happens inside the `for await` that drives the reduction, which kills the
 * generator and takes the rest of the run with it — a malformed patch would
 * cost the user the reply. The story says such a patch is ignored, so the
 * event is filtered out *before* the thread ever sees it, using the SDK's own
 * `applyJsonPatch` against a throwaway copy to decide. The SDK stays the only
 * implementation of the patch semantics.
 */
export function patchApplies(state: PandoState | undefined, ops: JsonPatchOperation[]): boolean {
  if (state === undefined) return false;
  try {
    applyJsonPatch(structuredClone(state), ops);
    return true;
  } catch {
    return false;
  }
}

/**
 * A transport that runs the agent through the `DataProvider` seam.
 *
 * `PandoThread` composes it and does all the reducing; the only behaviour
 * added here is dropping a `STATE_DELTA` the thread would throw on.
 */
export function createAgentTransport(options: AgentTransportOptions): AgentTransport {
  const { provider, repo, getState } = options;
  const onDroppedPatch =
    options.onDroppedPatch ??
    ((ops: JsonPatchOperation[], reason: string) => {
      console.warn(`agent: dropped a STATE_DELTA (${reason})`, ops);
    });

  return {
    async *run(runOptions: AguiRunOptions): AsyncGenerator<AguiEvent, void, undefined> {
      const override = options.streamOverride?.() ?? null;
      const stream =
        override ??
        provider.runAgent(buildRunInput(runOptions), {
          ...(repo === undefined ? {} : { repo }),
          ...(runOptions.signal === undefined ? {} : { signal: runOptions.signal }),
        });
      for await (const event of stream) {
        if (event.type === 'STATE_DELTA' && getState !== undefined) {
          const state = getState();
          if (!patchApplies(state, event.delta)) {
            onDroppedPatch(
              event.delta,
              state === undefined ? 'no STATE_SNAPSHOT yet' : 'the patch does not apply',
            );
            continue;
          }
        }
        yield event;
      }
    },
  };
}

/**
 * The transport as `PandoThread` types its `client` option.
 *
 * See the file header: the cast is forced by a class type with private fields,
 * not by a gap in what the shim implements.
 */
export function asThreadClient(transport: AgentTransport): PandoAguiClient {
  return transport as unknown as PandoAguiClient;
}
