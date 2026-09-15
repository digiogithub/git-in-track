# ADR-035 — The agent panel consumes AG-UI directly, with `@pando-ai/sdk/agui` and our own chat UI

- **Status:** Accepted
- **Date:** 2026-09-15
- **Phase:** 9 (Agent interface and semantic search)
- **Related:** [ADR-009](ADR-009-react-vite-typescript.md), [ADR-010](ADR-010-mcp-agent-surface.md), [ADR-025](ADR-025-the-cors-proxy-security-model.md), [ADR-032](ADR-032-local-integration-credential-storage.md)
- **Implements:** `GIT-US-0069` — Pando-side configuration, persona and agent interface documentation
- **Research:** [Pando TypeScript SDK and AG-UI client from a browser](../research/2026-09-13-pando-gap-sdk-client.md), [Pando AG-UI server gaps](../research/2026-09-13-pando-gap-agui-server.md)

## Context

Phase 9 puts a conversation next to the backlog: ask a question in the web app, get an
answer with real item ids in it. The agent is Pando, running locally; the question is how
its stream reaches a React component.

Pando speaks [AG-UI](https://docs.ag-ui.com) over SSE. Its adapter
(`internal/agui`) mounts `GET /info`, `GET /healthz`, `POST /{agent}` for a run, and the
thread routes around them. The event vocabulary is close to the AG-UI spec but not equal to
it in two places that matter to us:

- `RUN_FINISHED` carries an `outcome` field, and `outcome: "interrupt"` is the entire
  human-in-the-loop design — a run parks, the browser answers a permission or a question,
  and the run resumes. It is not in the AG-UI `RunFinishedEventSchema`.
- Pando emits `REASONING_START` / `REASONING_MESSAGE_*` / `REASONING_END`, where the
  published AG-UI core has `THINKING_TEXT_MESSAGE_*` instead.

Whatever we build has to reduce a stream into a transcript, apply RFC-6902 `STATE_DELTA`
patches, and handle interrupts, permission cards, agent questions and cancellation. That
reducer is roughly 150 lines and **nobody ships it for us**: not Pando's SDK, not the
generic AG-UI client. The choice is therefore not "reducer or no reducer"; it is which
transport and which types we build it on, and how much machinery comes along.

Two constraints from the rest of phase 9 also apply. The deployment is one
`pando agui-serve` per repository, because `--cwd` chdirs once and the working directory
owns the configuration, the session database and an `ipc.lock`. And the token must never
reach the browser, which means the companion proxies and strips `Origin` (docs/20 section
6, ADR-025's posture applied to a second upstream).

## Decision

**The web app consumes AG-UI directly.** It depends on `@pando-ai/sdk@0.2.0`, imports only
the browser-safe deep entry `@pando-ai/sdk/agui/client` for `PandoAguiClient`,
`PandoThread`, `parseSSE`, `applyJsonPatch` and the HITL helpers (`approve`, `deny`,
`answerQuestion`, `isPermissionRequest`, `isQuestionRequest`), and takes its types from
`@pando-ai/sdk/agui` as type-only imports. The chat surface itself is ours, built from the
existing shadcn components and the design system of docs/13, reached through `DataProvider`
like every other feature.

**The companion is the only client of Pando.** The browser posts to
`/api/v1/agent/...` on the companion; the companion adds the AG-UI bearer token, strips
`Origin`, applies its own per-user and global in-flight caps, and proxies to the
per-repository `agui-serve` instance it has in its routing table. Pando's own
`AllowedOrigins` stays empty, because an absent `Origin` skips its CORS check entirely.

**The Pando side is generated, not hand-written.** `gintrack agent init` writes one
`.pando.toml` per repository with `[AGUI]` (including the `Tools` glob allow-list and
`Mesnada = false`), `[MCPServers.gintrack]`, `[Remembrances]` and `[MCPServer] HttpEnabled
= false`, plus the persona and the routing skill. The generated file, not this ADR, is the
place a reader looks for the current values; docs/20 explains each one.

## Consequences

### What becomes easier

- The interrupt/resume cycle, reasoning output and permission cards are all reachable,
  because we read Pando's own event shapes rather than a schema that drops them.
- The bundle stays small: the agui deep entry is `fetch` + `ReadableStream` +
  `AbortController` and pulls in nothing else.
- The chat UI obeys our design system and our provider seam, so browser-only mode reports
  the capability as absent instead of rendering a panel that cannot work.
- Adding a second repository is a second process and a second row in the companion's
  routing table, not a protocol change.

### What we now have to live with

- **A frontend dependency published by another project.** `@pando-ai/sdk` is versioned and
  released by Pando, on Pando's schedule. A breaking change in the agui subpath is our
  problem to absorb, and the pin is exact (`0.2.0`) for that reason. The fallback, if the
  package ever becomes unconsumable, is to vendor `client.ts` + `types.ts` (~470 lines, no
  dependencies) into `web/src/lib/agui/` — cheap, but then we own them.
- **The protocol is young.** AG-UI is pre-1.0 and Pando's dialect of it is ahead of the
  published schemas in exactly the places we depend on. Events we reduce today may be
  renamed; the reducer is the blast radius.
- **Operational coupling to a per-project Pando instance.** The panel works only while a
  `pando agui-serve` process is running for that repository, holding `.pando/ipc.lock` in
  its working directory. That is a second process the user must start, a port to keep free,
  a token file to keep in sync, and a lock that makes two instances on one repository an
  error rather than a scaling option.
- **A feature that exists only in companion mode.** Browser-only mode has no process that
  can hold a token or reach a local port, so this is the first substantial capability that
  is simply absent there, and the capability flag has to carry that honestly.
- **The persona and the skill are conventions, not enforcement.** `[AGUI] Tools` and
  `Mesnada = false` are real — Pando applies them after building the tool set — but the
  AG-UI agent is still the coder agent underneath. What keeps it from writing files is the
  allow-list plus `AutoApprove = false` and human-in-the-loop approval, not the persona
  text. The allow-list must be written as though the persona did not exist.
- **Loopback binding is not a boundary against the user's own browser.** A page the user
  visits runs on the user's machine. This bites hardest on Pando's own MCP transport on
  `:9777`, whose CORS policy is `*` with global auto-approval: binding it to 127.0.0.1
  protects nothing there, which is why the generated configuration leaves `HttpEnabled =
  false` and docs/20 says so in the security section rather than in a footnote.
- **A secret lands in the repository's working tree.** Pando does not expand environment
  variables inside an MCP server's headers, so `[MCPServers.gintrack.Headers]` holds the
  companion's literal bearer token. The generator writes the file with mode 0600 and warns,
  but the file must be git-ignored by the user; we cannot enforce that.
- **Two tabs on one thread fight.** A second POST on a live thread abandons the running
  one, server-side. We document it; we cannot fix it from here.

### The security boundary, stated plainly

`AutoApprove = false` plus the approval dialog is the whole boundary until Pando's `[AGUI] Tools`
allow-list (PANDO-EP-0002, shipped 2026-09-14 and written by `gintrack agent init`) is in force
on the instance; the allow-list supersedes it as the primary control and demotes human-in-the-loop
approval to defence in depth, it does not replace it. Pando has no per-thread permission policy:
the browser's "always allow for this thread" is an in-memory, per-thread, non-persisted client
memory that auto-answers the next prompt for the same tool name and is forgotten on reload.

The allow-list only sees a tool by name, and Pando's MCP gateway takes the names away: with
`ToolDiscovery` on (its default once an `[MCPServers]` entry exists) every MCP tool sits behind
`tool_search` or the generic `mcp_call_tool` proxy, which the list strips and which asks no
per-tool approval. `gintrack agent init` therefore writes `[ToolDiscovery] Enabled = false` and
`[MCPGateway] Enabled = false`; the boundary above assumes both.

## Alternatives considered

**CopilotKit with a Node sidecar.** Pando ships CopilotKit glue and an example that uses
it. Rejected: it requires a Node runtime process next to the Go companion, which
contradicts the single-binary distribution of ADR-005 and doubles what a user must install
and keep running. It also brings a GraphQL runtime and a component library with its own
opinions about layout and theming, which would sit beside — not inside — the design system
of docs/13.

**Re-implementing CopilotKit's GraphQL runtime in Go, inside the companion.** This would
keep the single binary and let the companion speak CopilotKit's protocol to a CopilotKit
frontend. Rejected: it is a substantial amount of protocol code we would own and have to
track against an upstream that is not ours, to obtain a frontend library we did not want in
the first place. The cost is paid to reach the alternative we just rejected.

**The generic `@ag-ui/client` `HttpAgent`.** It has the pieces the Pando SDK lacks: an
agent abstraction that owns `threadId`/`messages`/`state`, `applyPatch` for `STATE_DELTA`,
`verifyEvents`, `abortRun()`. Rejected on fidelity and weight. `RunFinishedEventSchema` has
no `outcome`, so `outcome: "interrupt"` — the signal the whole HITL design rests on — is
invisible to the abstraction; `EventType` has no `REASONING_*`, so thinking output passes
validation and is then never reduced into `messages`, i.e. it silently disappears. It also
adds rxjs, zod, uuid, `@ag-ui/proto` and `@ag-ui/encoder` to the browser bundle, reads
`process.env` in a subscriber-error path (a `define` shim under Vite), and **still** needs
the same custom reducer for permissions, questions and `pando.*` custom events.

**Vendoring `src/agui/client.ts` + `types.ts` into `web/src/lib/agui/`.** Identical code,
no registry dependency. Not chosen as the default because a published, versioned package is
easier to upgrade and to audit than a copy that silently ages; kept explicitly as the
fallback if the package becomes unconsumable, and the reason the import surface is kept
narrow enough for that swap to be mechanical.

**A REST-only panel with no streaming.** Ask a question, wait, render an answer. Rejected:
it forfeits token streaming, reasoning output and interrupts, and interrupts are not a
nicety here — with `AutoApprove = false` a run that cannot ask for approval cannot use a
tool at all.
