# ADR-015 — The official Go MCP SDK, and verb-noun tool names

- **Status:** Accepted
- **Date:** 2026-09-04
- **Phase:** 5 (MCP server and agent workflows)
- **Related:** [ADR-010](ADR-010-mcp-agent-surface.md), [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-005](ADR-005-companion-cli-go-embed.md)
- **Implements:** `GIT-US-0024`

## Context

[ADR-010](ADR-010-mcp-agent-surface.md) decided *that* MCP is the agent surface and
listed the design rules it has to follow. It deliberately left two things open,
and both had to be settled to write the first line of `internal/mcp`.

**Which SDK.** [`08-mcp-server.md`](../08-mcp-server.md) §1 named
`mark3labs/mcp-go` "or the official Go MCP SDK", isolated behind
`internal/mcp/transport.go`. When the package was written both existed:
`mark3labs/mcp-go` is the older community implementation with wide adoption, and
`github.com/modelcontextprotocol/go-sdk` is the specification authors' own,
published at v1 and maintained alongside the spec.

**How tools are named.** `08-mcp-server.md` §4 spelled the catalog
noun-first — `item_list`, `item_get`, `item_create` — which groups a large
surface tidily in a document. `GIT-US-0024` spelled the same tools verb-first —
`list_items`, `get_item`, `create_story`. A tool name is part of the public
contract: it appears in `--tools` allow-lists, in client configurations and in
the audit log, so it cannot be changed later without breaking them. One spelling
had to win before the first release.

Two smaller decisions rode along with the second one:

- The story's catalog has no `item_move`, but a status change must still be
  validated against the project's workflow rather than written as a plain field.
- The story's catalog has three `create_*` tools rather than one `item_create`
  with a `type` argument.

## Decision

**Use `github.com/modelcontextprotocol/go-sdk`, pinned to v1.4.0.**

- It is maintained by the authors of the protocol, so a spec revision reaches us
  as an SDK release rather than as a reverse-engineering exercise. ADR-010
  accepted spec churn as a cost; this is the cheapest way to pay it.
- It infers a JSON Schema from a Go struct, validates every incoming argument
  against it before the handler runs and every answer against the output schema
  before it leaves. That is exactly the "documented input and output schema"
  requirement of `GIT-US-0024`, obtained by construction instead of by a
  hand-written schema that can drift from the struct.
- It ships both transports the product needs — stdio and streamable HTTP — and a
  **client**, which is what lets the end-to-end tests drive a real
  `gintrack mcp` process and a real `POST /mcp` endpoint through real protocol
  sessions rather than by calling handlers.
- **v1.4.0, not the newest release.** From v1.4.1 onward the SDK's `go.mod`
  requires Go 1.25, and this module — and CI — are on Go 1.24. Pinning keeps
  `make build` and the release pipeline on the toolchain
  [`09-ci-cd-and-releases.md`](../09-ci-cd-and-releases.md) declares. The pin
  moves when the project's Go baseline does.

It is the only new direct dependency. It pulls in `google/jsonschema-go` and
`yosida95/uritemplate` transitively, and nothing else.

**Name tools verb first: `list_items`, `get_item`, `create_story`.**

- It is the convention agent runtimes and their users already read, and a model
  picks a tool by reading its name before its description.
- `GIT-US-0024` is the acceptance-tested contract; the document's spelling was a
  drafting convenience, and the document is updated to match.
- The three `create_*` tools stay three tools. An agent that picks a tool by name
  cannot file a task as an epic by mistyping a field, and the model's choice is
  visible in the audit trail without inspecting arguments.
- `update_item` accepts `status` and routes it through the core's `item.move`,
  which validates the transition against the project's workflow. The agent sees
  one obvious tool for "change this item"; the validation is the same code the
  web UI runs. A transition the project does not declare is refused with the
  transitions it does declare.

## Consequences

**Positive**

- Schemas cannot drift from the handlers: the Go type is the schema.
- The MCP layer stays thin. Everything above `transport.go` is transport- and
  SDK-agnostic, so the stdio server and the HTTP endpoint are provably the same
  surface — the same registry, the same workspace, one set of tests.
- Invalid arguments are rejected by the SDK before a handler runs, so a handler
  only ever validates *domain* rules, which live in `internal/core`.
- The end-to-end tests are honest: a spawned binary and an HTTP listener, driven
  by the SDK's client.

**Negative**

- The version pin is a maintenance item. It has to be revisited when this module
  raises its Go baseline, and the SDK is young enough that v1 releases arrive
  often.
- Tool names now differ from a document that was written first. The document is
  corrected in the same change, but anyone holding the older draft sees a
  mismatch.
- `update_item` doing double duty means one tool can produce two commits — a
  patch and a move — when a call changes fields and status at once. The result
  reports the final `rev`, and the move quotes the rev the patch produced rather
  than the caller's, so no write is lost.

## Alternatives considered

- **`mark3labs/mcp-go`.** Mature and widely used, and its schema handling is
  hand-rolled rather than inferred, which is precisely the drift `GIT-US-0024`
  asks us to avoid. Rejected; the isolation in `transport.go` means switching
  later costs one file if that judgment turns out wrong.
- **Hand-written JSON-RPC.** No dependency at all, and it puts protocol
  revisions, session management and SSE framing on us forever. Rejected: the
  protocol is not our product.
- **Keeping the document's `item_list` names.** Consistent with the draft and
  inconsistent with the story that was accepted and with what agent runtimes
  read. Rejected; the document is the thing that changes.
- **One `item_create` with a `type` argument.** Fewer tools, and a mistyped
  field silently files the wrong kind of item. Rejected for a surface whose
  caller is a language model.
- **A separate `move_item` tool.** Closer to the REST API's shape, and a fourth
  write tool for what an agent thinks of as "update this item". Rejected;
  `update_item` routes a status change through the same validated path.
