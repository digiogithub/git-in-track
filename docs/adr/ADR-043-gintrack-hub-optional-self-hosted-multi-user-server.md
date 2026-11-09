# ADR-043 — gintrack hub: an optional self-hosted multi-user server built from the same core

- **Status:** Proposed
- **Date:** 2026-11-08
- **Phase:** Post-1.0 (gintrack hub proof of concept; no epic yet)
- **Related:** [ADR-002](ADR-002-git-as-only-sync.md), [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-005](ADR-005-companion-cli-go-embed.md), [ADR-010](ADR-010-mcp-agent-surface.md), [ADR-015](ADR-015-official-go-mcp-sdk-and-verb-noun-tools.md), [ADR-031](ADR-031-external-references.md), [ADR-033](ADR-033-inbox-is-a-reserved-triage-status-category.md), [ADR-036](ADR-036-pando-indexes-the-repository-directly.md), [ADR-039](ADR-039-gintrack-launches-and-manages-pando.md), [ADR-040](ADR-040-gintrack-update-verified-self-update.md), [ADR-044](ADR-044-memory-items-and-knowledge-only-projects.md)
- **Supersedes:** nothing. It narrows the non-goal "No central server, no hosted service" of docs/01-vision-and-scope.md §6 for one optional product; it does not withdraw it for `gintrack`.

## Context

Teams want a *shared project state* that chat products do not give them. A
claude.ai Project keeps memory per user and opaque, its knowledge is small, its
artifacts are not kept, and nothing in it is reachable by other agents (Pando,
Codex, Gemini, project agents). The wanted service holds:

- an explicit, reviewable memory of facts with scopes (project, organisation),
  an author (a person or an authenticated agent), a date and a proposal queue
  (an agent proposes, a person accepts);
- a versioned knowledge base of project documents with hybrid search;
- remote MCP (Streamable HTTP + OAuth) and REST for several users and agents,
  each with its own identity and per-project grants, plus the web UI for people
  who do not use git.

gintrack already has almost every piece: Markdown items with permanent ids,
`rev` and `stale_revision`, three-way merge of front matter (`internal/core`),
the `CoreApi` contract (`internal/vault`), commit batching and sync
(`internal/gitops`), a job engine (`internal/syncengine`), the MCP server with a
Streamable HTTP transport (`internal/mcp`) and the web app (`web/`). What it
lacks is everything that makes it multi-user:

- the companion is one person on loopback, authenticated by one bearer token
  (`internal/server/server.go:598`);
- people are strings and the commit author is the machine's git identity:
  `CommitRequest.Author` exists (`internal/gitops/gitops.go:188`) but the
  Committer never sets it (`internal/gitops/committer.go:335`), and its
  coalescing key is repository + item (`committer.go:201`), so two people
  editing one item would share a commit;
- the MCP `Agent:` name is fixed per process (`internal/mcp/server.go:85`);
- an unscoped query searches every project of every mount
  (`internal/vault/semantic.go:31`), which is correct for one person and a
  leak between tenants;
- semantic search needs one managed Pando process per repository (ADR-039).

AlchemAI Mem is a larger corporate memory backend moving at its own pace. Its
client, `memsync` in alchemai-agent, is what Pando already uses to push and pull
memory. A proof of concept that speaks the same protocol
(`alchemai.memory.v2`) lets Pando connect to the hub with no new extension.

## Decision

1. **A separate binary, `cmd/gintrack-hub`, in the same Go module.** `gintrack`
   stays local and serverless, so the non-goal of docs/01 §6 is narrowed for
   the hub alone. The hub imports `internal/*` and duplicates no domain logic.
   It ships as its own artifact (container image), and the asset names
   `gintrack update` depends on (ADR-040) do not change.
2. **Git stays the only source of truth.**
   - Memory and knowledge are Markdown files in the project and team
     repositories, which the hub clones from the team's existing forge with a
     deploy key and pushes back to (ADR-002). In the PoC the hub serves no git
     itself.
   - Users, agent-token hashes, grants and views live in YAML in an
     administration repository.
   - Every SQLite file and in-memory index is derived data that can be rebuilt
     from the files.
3. **Every write carries an authenticated principal.**
   - People sign in with OIDC (Google), agents with per-identity opaque tokens,
     and MCP clients with OAuth 2.1 as the MCP specification describes. OAuth
     is in the PoC, so a claude.ai Project can connect.
   - The principal travels in `context.Context` down to `gitops`. It becomes
     the commit author, the `Agent:` trailer and an `On-Behalf-Of:` trailer,
     and it is part of the Committer's coalescing key.
4. **Authorisation lives in one wrapper around the vault Dispatcher**, shared by
   REST, MCP and the memory protocol.
   - A *view* restricts the paths, memory scopes and tools a token or OAuth
     client sees. Tools a view denies are absent from `tools/list`, as write
     tools are without `AllowWrite`.
   - Views do not apply to git: anyone who can clone gets the whole project.
5. **Semantic search uses Pando's RAG engine as a library** (`pando/rag`: SQLite
   FTS5 + embeddings + RRF, pure-Go driver), installed behind
   `vault.SemanticSearcher` (`internal/vault/semantic.go:55`). The hub starts
   no Pando process.
   - It reindexes per commit: the job `rag.apply{project, from, to}` runs
     with a commit watermark stored in the same transaction as the index
     update.
   - Every hit is resolved back to the live document by `rev`, and stale hits
     are dropped.
6. **The hub implements the server side of `alchemai.memory.v2` over git.**
   - Endpoints: `POST /v1/memory/batch`, `GET /v1/memory/search`,
     `GET /v1/memory/changes` and `POST /v1/memory/enrich`, using the public
     `alchemai-sdk/wire` module.
   - Facts are `memory` items (ADR-044), one file per key, with one
     synchronous commit per project per batch.
   - `version` is a monotonic per-project counter **stored in front matter**,
     so cursors survive an index rebuild, and `Record.id` is the stable item
     id.
   - Only the `project/` and `org/` scopes exist; `user/*` and `private/*` are
     rejected.
   - Additive extensions: `/capabilities`, proposals (the `propose` grant
     writes into the triage category of ADR-033; `write` auto-accepts) and
     `base_version` → `stale_revision`.
   - Redaction stays with the client.

## Consequences

### Easier

- One implementation of the model, the `rev` protocol and the merge serves the
  companion, the browser and the hub. A fix in `internal/core` reaches all
  three.
- Commits name the real author, so `git log` is the write audit. Grants are
  YAML in git, so their history is the permission audit.
- Pando, Claude Code, Codex and claude.ai Projects share one memory and one
  knowledge base, reviewed by people in the existing inbox UI.

### Harder — the price we are paying

- **A product boundary to defend.** docs/01 §6 is no longer absolute. The hub
  must stay optional, and nothing in `gintrack` may come to depend on it.
- **A security surface we did not have.** OAuth 2.1 with dynamic client
  registration, token storage and a multi-tenant authorisation wrapper. A
  single Dispatcher method that escapes the wrapper is a tenant leak, so the
  wrapper needs a test that walks every method with a principal holding no
  grant.
- **`spec_impact` tiers 2 and 3 answer `unavailable` in the hub.** They need
  Pando's code graph, which the RAG library does not carry. Tier 1 and
  `trace_requirement` still answer.
- **Views do not apply to git.** A view is a convenience for MCP, REST and the
  UI, never a confidentiality boundary for someone with clone access.
- **Memory throughput is bounded by git**: one commit per batch per project.
  That is fine for a PoC; heavy use may need batching across requests.
- **The hub is a long-running service with N indexes in memory.** It needs
  lazy mounting and limits per organisation.
- **`internal/core` must stay WASM-clean** while hub-only needs grow. Anything
  hub-specific (auth, the memory protocol, SQLite) lives under `internal/hub/`
  or `cmd/gintrack-hub`, never in `core` or `vault`.

### Documents to change when this is accepted (not changed by this ADR)

- **docs/01-vision-and-scope.md §6**: reword "No central server, no hosted
  service" to keep it true for `gintrack` and point to this ADR for the
  optional hub.
- **docs/02-architecture.md**: a hub section covering the components, the
  principal flow and the Dispatcher wrapper.
- **docs/03-data-model.md**: see ADR-044.
- **docs/07-cli-and-api.md**: the authentication modes, and `/v1/memory/*`
  beside `/api`.
- **docs/08-mcp-server.md**: the hub-only tools (`write_kb_page`,
  `add|propose|update|forget|search_memory`, `accept|reject_memory`,
  `project_context`), OAuth, and views. The local tool counts in AGENTS.md and
  `cmd/gintrack/mcp_test.go` do not change, because hub tools are registered
  only by the hub binary.
- **docs/09-ci-cd-and-releases.md**: the hub image. The release pipeline is a
  human-only area, so that change is a separate story.
- **docs/11-roadmap.md**: the hub phases (human-only area).
- **AGENTS.md**: the hub entry in the layout, and the "no central server" rule
  rephrased for the hub.
- Rename `internal/server/hub.go` (the WebSocket event hub) so that "hub" means
  one thing.

## Alternatives considered

### `gintrack hub` as a subcommand of the existing binary

Rejected. It would ship OIDC, OAuth, SQLite and the memory protocol inside the
local CLI that every user installs, and it would make "gintrack has no server"
false for the product itself rather than for an optional one. The single
binary was the only gain, and a separate artifact costs nothing against
ADR-040.

### The hub in its own repository or module

Rejected. The hub needs `internal/core`, `internal/vault`, `internal/gitops`,
`internal/mcp` and `internal/syncengine`. A separate module could only import
them if they became a public, versioned API. That compatibility cost is far
larger than keeping one more `cmd/` in this module. It would also invite the
duplicated parsing and validation that AGENTS.md forbids.

### Extend Pando into the hub ("Pando mode")

Rejected. Pando is per-machine and per-repository (ADR-039), and its
remembrances live in its own database. A Pando-based hub would make that
database the source of truth for shared memory, which is what ADR-001 and
ADR-036 rule out, and it would leave the web UI, items, `rev` and the git
history outside. Pando contributes its RAG engine as a library instead, and
stays a client through memsync.

### One managed Pando process per project, as the companion does

Rejected for the hub. N processes, N ports and N configurations per
organisation is acceptable on one person's machine and not on a shared
server. The library behind the same `SemanticSearcher` seam gives the same
answers with none of the supervision.

### A database as the memory store with a periodic export to git

Rejected. Git would stop being the source of truth, `rev` and the three-way
merge would not apply, and a lost export would be lost memory. One commit per
batch is the price of keeping ADR-002.

### Own git smart-HTTP server in the PoC

Deferred, not rejected. Teams already have a forge. Serving git means
`git http-backend`, push/receive coordination with the hub's checkout and a
binary store. It returns as its own ADR if a team without a forge needs it.
