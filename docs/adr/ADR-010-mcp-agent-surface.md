# ADR-010 — MCP is the integration surface for AI agents

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 5 (MCP server + agent workflows)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-002](ADR-002-git-as-only-sync.md), [ADR-005](ADR-005-companion-cli-go-embed.md)

## Context

AI-agent-assisted teams are an explicit target audience. An agent working in a
checkout can already read and write `.pmngr/*.md` files directly — that is a
deliberate property of the format ([ADR-001](ADR-001-markdown-yaml-storage.md)) and it
must keep working with no server involved.

But direct file access alone is inefficient and error-prone at scale:

- Finding "the next ready task" among 2,000 files means reading 2,000 files, which
  burns context and money.
- Writing means the agent must reproduce our YAML conventions exactly, or produce a
  file our parser rejects or our writer reformats.
- Concurrent edits by a human and an agent silently clobber each other without a
  concurrency primitive.
- There is no natural place to put agent-specific affordances such as pagination or
  field projection.

Meanwhile the Model Context Protocol has become the common way for agents to
discover and call tools, with client support across major agent runtimes and stdio
and HTTP transports.

## Decision

**Ship `gintrack mcp` as the supported, optimised agent integration surface, while
keeping direct file access fully valid.**

- Transports: **stdio** (`gintrack mcp`, for agent runtimes that spawn a process) and
  **streamable HTTP** mounted on the local server
  ([ADR-005](ADR-005-companion-cli-go-embed.md)), behind the same token and origin
  checks.
- Tools (initial set): `list_projects`, `search_items`, `get_item`, `create_epic`,
  `create_story`, `create_task`, `update_item`, `add_comment`, `move_on_board`,
  `read_kb_page`, `search_kb`.
- Tools delegate to `internal/core` — the same code the UI and REST API use. No
  agent-only semantics, ever.
- Agent-oriented design rules, all enforced in the tool schemas:
  - **Front matter only by default.** `get_item` and `search_items` return front
    matter plus computed metadata; bodies require `body: true`. Field projection is
    supported.
  - **Compact JSON.** Short, stable key names; no decorative wrapper objects; no
    prose in payloads.
  - **Pagination everywhere.** Cursor-based, with a default and a maximum page size.
  - **`rev` on every read, optional on every write.** A write supplying a stale `rev`
    fails with `CONFLICT` and returns the current one. This is the agent's optimistic
    lock ([ADR-002](ADR-002-git-as-only-sync.md)).
  - **Errors are actionable**: machine-readable `code`, the offending field, and what
    a correct value looks like.
- Repository conventions for agents live in `AGENTS.md` at the repository root,
  versioned with the code: which fields an agent may set, when to comment rather than
  edit, commit message conventions, and when to stop and ask.
- Every MCP write lands in the working tree as an ordinary file change, visible in
  `git status` and reviewable in a pull request. The MCP server has no privileged
  path and grants no capability that a human with a checkout lacks.

## Consequences

**Positive**

- An agent can triage a large backlog for a few thousand tokens instead of a few
  hundred thousand.
- Agents write valid files by construction: our writer serialises, so conventions and
  formatting stay consistent and diffs stay clean.
- `rev`-based optimistic locking gives agents a safe concurrent-write story without a
  lock server.
- Agent activity is reviewable exactly like human activity, because it is commits.
- One implementation: MCP tools, REST handlers and the UI share operation names and
  payload shapes, so behaviour is specified and tested once.

**Negative**

- **MCP is young and moving.** Spec revisions and SDK churn are a maintenance cost.
  Mitigated by keeping the MCP layer thin — framing and schemas only — so a spec
  change touches one package.
- **A second API surface to document, version and test**, in addition to REST.
- **Security.** stdio inherits the trust of the spawning process; the HTTP transport
  needs the same token, origin and path-confinement protections as the rest of the
  local API, and must never become a general file-read primitive.
- **Prompt-injection exposure.** Repository content is untrusted and flows into agent
  context. We cannot solve this; we can avoid amplifying it (no automatic execution
  of instructions found in items) and document it.
- **Tool-surface discipline.** Tools are easy to add and hard to remove. The initial
  set is deliberately small, and additions need justification.
- **Requires the companion binary.** Browser-only users get no MCP server — though
  agents in that situation still have direct file access.

## Alternatives considered

- **Direct file access only, no server.** Zero new surface and it always works — and
  it makes backlog-scale reads prohibitively expensive and gives agents no
  concurrency primitive. Kept as a supported baseline, rejected as the whole story.
- **A REST API for agents.** We have one for the UI, and agents would need bespoke
  documentation, an HTTP client and an auth flow per runtime, with no tool discovery.
  MCP gives discovery and schemas for free. Rejected as the *primary* agent surface;
  the REST API remains available.
- **A dedicated CLI for agents (`gintrack query --json`).** Composable and easy to
  ship, but every agent runtime then needs its own shell integration, and there is no
  schema for the agent to introspect. Retained as a convenience, not the main surface.
- **OpenAPI plus a generated client.** Good for conventional integrations, poor for
  agent tool discovery in current runtimes. Rejected for this purpose.
- **Waiting for MCP to stabilise before shipping.** Would delay a core differentiator
  past 1.0 for a target audience that exists now. Rejected; we accept the churn.
