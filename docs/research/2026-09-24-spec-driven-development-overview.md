---
title: Spec-driven development in git-in-track — overview
created: 2026-09-24
updated: 2026-09-24
author: claude
status: validated
reviewed_by: GIT-T-0238
---

# Spec-driven development in git-in-track — overview

> **Status: validated on 2026-09-24** through GIT-T-0238. The reviewer's
> decisions are in §7 and override anything earlier drafts said. The plan below
> is the Phase 11 milestone in `docs/.pmngr/`.

## 1. Problem

Agents working from Markdown specs have the same three blind spots, whatever
tool they use (Spec Kit, Kiro, OpenSpec, BMAD, Tessl, Agent OS):

1. **Impact.** When a feature is added or changed, which existing specs and
   requirements does the change touch? Today the agent either re-reads the whole
   spec folder or does not check at all.
2. **Coverage.** Is each requirement tested, and did that test pass last time?
   None of the agent SDD tools ingest test results.
3. **Validity.** Does the requirement still hold after the change? Drift is
   detected by asking an LLM to re-read code and spec (Spec Kit `converge`,
   OpenSpec `verify`, Traycer). That is expensive, non-deterministic and not
   incremental.

The shared root cause: **no index, no stable requirement identity, no link from
requirement to code and tests.** git-in-track already has the first two halves:
permanent IDs, typed links, `rev` content hashes, an MCP server with field
projection and pagination, and Pando for semantic search plus code-symbol
indexing and impact analysis. It is missing the spec layer on top.

## 2. What we borrow and what we add

| Idea | Taken from | In git-in-track |
|---|---|---|
| Living specs separate from change proposals, ADDED / MODIFIED / REMOVED deltas | OpenSpec | Story carries a `## Spec Delta`; applied when the story is done |
| Constrained grammar (EARS, SHALL + WHEN/THEN scenarios) | Kiro, OpenSpec | Linted in `internal/core`, live in the web editor via WASM |
| Stable requirement IDs | Spec Kit `FR-001`, Doorstop | Scoped block IDs `GIT-SP-NNNN.R<n>`, never by title |
| Suspect links after upstream change | Doorstop fingerprints | Verification stamp per requirement plus git and Pando change detection |
| Bidirectional coverage, test result import | StrictDoc | `// Verifies: GIT-SP-0003.R2` markers plus `go test -json`, JUnit and Vitest ingest |
| Only the relevant slice reaches the agent | Kiro steering `fileMatch`, BMAD shards | `spec_context` generated on demand, never copied |
| Cheap-first verify loop | Spec Kit `converge`, Traycer | Deterministic checks first; the LLM sees only flagged requirements |

**The differentiator is the impact query.** Given a diff, return the affected
requirements in a few hundred tokens with the reason for each hit, its test
status and whether it is suspect. It resolves in three tiers, cheapest first:

1. **Direct trace.** Changed files and symbols that carry a requirement marker,
   or that a requirement's `trace:` declares.
2. **Transitive.** Pando `code_impact_analysis` over the changed symbols reaches
   callers that carry a marker.
3. **Semantic.** Pando semantic search over requirement blocks, using the
   changed symbol names and story title. These hits are flagged `candidate`
   with a score, never presented as certain.

## 3. Model (ADR-037, human-supervised)

- **One new item type, `spec`** (`GIT-SP-NNNN`, type code `SP`, folder
  `docs/.pmngr/specs/`). A spec is a capability: purpose, scope and glossary,
  followed by its requirements as blocks in the body.
- **Requirements are blocks, not files.** Each one is a heading
  `### GIT-SP-0003.R2 — <title>` followed by an EARS or SHALL statement and
  `#### Scenario:` blocks (WHEN/THEN).
  - Requirement IDs are scoped `<SPEC-ID>.R<n>`: permanent, never renumbered or
    reused; the next one is max+1 within the file. Moving a requirement to
    another spec allocates a new ID and records `supersedes`.
  - Every requirement behaves as a separate unit everywhere: its own status,
    its own block-level `rev` (hash of the block), trace, verification stamp,
    row in lists, search and coverage, MCP addressing (get or update one
    requirement quoting its block `rev`), and Pando entry whose hits resolve to
    the block anchor.
- **Per-requirement metadata** lives in the spec front matter as a
  `requirements:` map keyed by `R<n>`; prose stays in the body:

  ```yaml
  requirements:
    R2:
      status: in_progress
      trace: {code: [internal/core/ids.go#NextID], tests: [internal/core/ids_test.go#TestNextID]}
      verified: {rev: sha256:..., commit: <sha>, at: 2026-10-01, by: claude}
  ```

- **Lifecycle** reuses the project workflow statuses; no per-type workflow.
- **New link kinds:** `implements` / `implemented_by` (story or task to
  requirement), `modifies` / `modified_by` (story that changes an existing
  requirement — the delta), `supersedes` / `superseded_by`. Link targets accept
  requirement refs `GIT-SP-NNNN.R<n>`; this target-grammar extension is part of
  ADR-037.
- **Code and test anchors**, both deterministic:
  - in-code markers `// Implements: GIT-SP-0003.R2` and
    `// Verifies: GIT-SP-0003.R2`, which travel with the code;
  - the optional `trace:` entry for code that cannot carry a comment.
- **Verification stamp** is the only state written back:
  `gintrack spec verify` writes `verified` when the linked tests pass.
  **Suspect** is computed, never stored: the block `rev` changed since the
  stamp, or traced files or symbols changed between `verified.commit` and HEAD
  (directly, or transitively through Pando).
- **Grammar lint** (EARS / SHALL / scenarios / vague words) is a warning by
  default, configurable in `project.yaml` and raisable to error.
- **Derived cache only.** Test results and the marker scan are rebuildable
  caches. The repository stays the source of truth; Pando never writes specs.
- **Boundaries.** Block parsing, IDs, lint and the delta live in
  `internal/core` (WASM-safe). Diff, marker scan, test ingest and Pando calls
  stay out of `internal/core` and `internal/vault`, behind host-installed seams
  like `vault.SemanticSearcher`.

## 4. Milestone — Phase 11: Spec-driven development

| # | Epic | Content | Areas |
|---|---|---|---|
| E1 | Spec data model | ADR-037 + docs/03 first (human approval, no code), then the code: `spec` type, `SP` IDs, folder, requirement blocks with block `rev`, `requirements:` map, link kinds and requirement-ref targets. **Agent under human supervision, own PR.** | core, docs |
| E2 | Authoring, lint and Spec Delta | Vault requirement addressing, grammar linter (warn, configurable), `## Spec Delta` parsing and application on done, templates, duplicate detection on create | core, wasm, server |
| E3 | Trace engine | `gitops.ChangedFiles(from, to)` for git, system-git and jj; marker scanner; trace graph; test-result ingest; verification stamp and suspect computation | git, server, cli |
| E4 | Impact over Pando | Wrappers for `code_impact_analysis`, `code_find_symbol`, `code_related_files`; requirement blocks in the Pando index; three-tier resolver with token budget; `unavailable` without Pando | server, mcp |
| E5 | Agent surface (MCP and CLI) | Stdio semantic-search fix; per-requirement MCP get/update/list; `spec_context`, `spec_coverage`, `spec_impact`, `verify_requirement`, `trace_requirement`; `gintrack spec lint\|impact\|coverage\|verify`; SDD loop in AGENTS.md and docs/08 §10 | mcp, cli, docs |
| E6 | Web | Spec HTTP API; `/p/$project/specs` tree with one row per requirement; requirement detail and trace panel; **coverage matrix (mandatory)**; impact view for a ref range; live lint | web, server, wasm |
| E7 | CI gate | `gintrack spec impact --since <ref> --fail-on failing,suspect` as a PR check and a git hook | ci, cli |
| E8 | Dogfood and benchmark | Specs for rev protocol, ID allocation, link validation and MCP pagination; token benchmark for "what does this PR affect?" | docs |

Deferred, no milestone: **Spec importers** (Spec Kit, OpenSpec, Kiro), status
`backlog`.

E1 blocks almost everything. E3 and E4 run in parallel after E1. E5 and E6
depend on E3 and E4. E7 and E8 close the milestone.

## 5. Agent loop once built

1. Pick a story, then call `spec_context(story)`. It returns the linked
   requirements with one-line statements and scenarios, their test status and
   related KB pages, within a token budget.
2. Implement, then add `// Implements:` and `// Verifies:` markers.
3. Call `spec_impact(base: main, head: worktree)`. It returns the affected
   requirements, why each was hit, the test status and suspect ones, typically
   under 1.5k tokens.
4. For each suspect or failing requirement, do one of three things: fix the
   code, add a `## Spec Delta` (MODIFIED), or add a comment asking a human.
5. Tests pass, so `verify_requirement` stamps the requirement. The PR goes up,
   and the CI gate reruns step 3 deterministically.

## 6. Success criteria

- The impact report for a typical PR is ≤ 1.5k tokens, 10× cheaper than an
  agent reading the relevant spec folder (measured in E8).
- Tiers 1 and 2 are deterministic: the same diff gives the same result, with
  or without an LLM.
- Every requirement shows one of `untested`, `passing`, `failing` or `suspect`
  in the CLI, MCP and web.
- The WASM build still passes. Browser-only mode keeps authoring and lint, and
  impact and ingest answer `unavailable`.

## 7. Decisions (2026-09-24)

1. **Granularity.** Requirements are blocks inside one `spec` file
   (`### GIT-SP-0003.R2 — <title>`), not files. IDs `<SPEC-ID>.R<n>` are
   permanent, next = max+1 in the file; moving one means a new ID plus
   `supersedes`. Each requirement still behaves as a separate unit (status,
   block `rev`, trace, stamp, list/search/coverage row, MCP addressing, Pando
   anchor). Metadata goes in a front-matter `requirements:` map keyed by `R<n>`.
2. **Code anchors.** In-code `// Implements:` and `// Verifies:` markers plus
   the optional `trace:` entry.
3. **Lifecycle.** Reuse the project workflow statuses; no per-type workflow.
4. **Grammar.** Lint warns by default; `project.yaml` can raise it to error.
5. **Web.** The coverage matrix (requirement × tests; `untested`, `passing`,
   `failing`, `suspect`) is mandatory for the milestone.
6. **Scope.** The E7 CI gate and git hook are in. Importers for Spec Kit,
   OpenSpec and Kiro are a separate epic with no milestone, status `backlog`.
7. **E1 ownership.** An agent under human supervision, in its own PR. First
   story: ADR-037 and docs/03 for approval, no code. Second: the code, blocked
   by the first.
8. **Naming.** `spec` / `SP`. Link kinds `implements`/`implemented_by`,
   `modifies`/`modified_by`, `supersedes`/`superseded_by`; targets accept
   `GIT-SP-NNNN.R<n>` (part of ADR-037).

## Sources

A summary of the landscape review, done on 2026-09-24:

- Spec Kit: https://github.com/github/spec-kit
- OpenSpec: https://github.com/Fission-AI/OpenSpec
- Kiro: https://kiro.dev/docs/specs/ and https://kiro.dev/docs/specs/correctness/
- Fowler on SDD tools: https://martinfowler.com/articles/exploring-gen-ai/sdd-3-tools.html
- Doorstop: https://github.com/doorstop-dev/doorstop
- StrictDoc: https://strictdoc-project.github.io/features/
- BMAD: https://www.augmentcode.com/guides/bmad-method-ai-development
- Traycer: https://docs.traycer.ai/tasks/phases
