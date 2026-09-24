---
title: Spec-driven development in git-in-track — overview for validation
created: 2026-09-24
author: claude
status: draft
reviewed_by: GIT-T-0238
---

# Spec-driven development in git-in-track — overview

> **Status: draft for human validation.** Nothing here is in the backlog yet.
> Once the review task GIT-T-0238 is resolved, this becomes the Phase 11
> milestone with its epics and stories. The open questions in §7 are the
> decisions that change the plan.

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
| Stable requirement IDs | Spec Kit `FR-001`, Doorstop | Permanent item IDs `GIT-RQ-NNNN`, never by title |
| Suspect links after upstream change | Doorstop fingerprints | Verification stamp in front matter plus git and Pando change detection |
| Bidirectional coverage, test result import | StrictDoc | `// Verifies: GIT-RQ-0042` markers plus `go test -json`, JUnit and Vitest ingest |
| Only the relevant slice reaches the agent | Kiro steering `fileMatch`, BMAD shards | `spec_context` generated on demand, never copied |
| Cheap-first verify loop | Spec Kit `converge`, Traycer | Deterministic checks first; the LLM sees only flagged requirements |

**The differentiator is the impact query.** Given a diff, return the affected
requirements in a few hundred tokens with the reason for each hit, its test
status and whether it is suspect. It resolves in three tiers, cheapest first:

1. **Direct trace.** Changed files and symbols that carry a requirement marker,
   or that a requirement declares.
2. **Transitive.** Pando `code_impact_analysis` over the changed symbols reaches
   callers that carry a marker.
3. **Semantic.** Pando `search_semantic` over requirements, using the changed
   symbol names and story title. These hits are flagged `candidate` with a
   score, never presented as certain.

## 3. Proposed model (needs an ADR — human-only area)

- **Two new item types:**
  - `spec`, a capability (`GIT-SP-NNNN`, folder `specs/`). It holds purpose,
    scope and glossary, and is the parent of its requirements.
  - `requirement` (`GIT-RQ-NNNN`, folder `requirements/`). It is one small file
    holding an EARS or SHALL statement plus `#### Scenario:` blocks
    (WHEN/THEN). Small files give per-requirement `rev`, cheap `get_item`,
    clean git blame and clean merges.
- **New link kinds:**
  - `implements` / `implemented_by`, from a story or task to a requirement.
  - `modifies` / `modified_by`, from a story that changes an existing
    requirement. This is the delta.
  - `supersedes` / `superseded_by`, from requirement to requirement.
- **Code and test anchors.** This part cannot use `links[]` today, because
  targets must be item IDs. Two sources, both deterministic:
  - **In-code markers**, which travel with the code through refactors:
    `// Implements: GIT-RQ-0042` and `// Verifies: GIT-RQ-0042`. These can be
    scanned natively and are cheap.
  - **An optional `trace:` field** on the requirement for code that cannot carry
    a comment: `trace: {code: [path#Symbol], tests: [path#TestName]}`.
- **Verification stamp**, the only state written back:
  `verified: {rev: <requirement rev>, commit: <sha>, at: <date>, by: <who>}`.
  `gintrack spec verify` writes it when the linked tests pass.
  - **Suspect** is computed and never stored. A requirement is suspect when its
    `rev` changed since the stamp, or when its traced files or symbols changed
    between `verified.commit` and HEAD (directly, or transitively through
    Pando).
- **Derived cache only.** Test results and the marker scan are rebuildable
  caches, like the index today. The repository stays the source of truth.

## 4. Milestone sketch — Phase 11: Spec-driven development

| # | Epic | Content | Areas |
|---|---|---|---|
| E1 | Spec data model | ADR-037; `spec` and `requirement` types, IDs, folders, link kinds, `trace`, `verified`; docs/03, JSON Schema, scaffold, validation. **Human-supervised.** | core, docs |
| E2 | Authoring and lint | EARS/SHALL/scenario linter in core (WASM), templates, `## Spec Delta` parsing, applying the delta when a story is done, duplicate detection on create via semantic search | core, wasm, web |
| E3 | Trace engine | Marker scanner (native), `gitops.ChangedFiles(from, to)` for git, system-git and jj, test-result ingest (go test -json, JUnit, Vitest), verification stamp, suspect computation | gitops, server, cli |
| E4 | Impact analysis over Pando | Wrappers for `code_impact_analysis`, `code_find_symbol` and `code_related_files`; the three-tier resolver with a token budget; semantic fallback marked `candidate`; `unavailable` degradation without Pando | server, mcp |
| E5 | Agent surface (MCP and CLI) | MCP: `spec_impact`, `spec_coverage`, `spec_context`, `verify_requirement`, `trace_requirement`. CLI: `gintrack spec lint\|impact\|coverage\|verify`. Fix stdio `gintrack mcp` so it gets the semantic searcher. SDD loop in AGENTS.md and docs/08 §10 | mcp, cli, docs |
| E6 | Web | `/p/$project/specs` capability tree, requirement detail with a trace panel, coverage matrix, impact view for a branch or ref range (companion mode), live lint in the editor | web |
| E7 | CI gate and interop | `gintrack spec impact --since origin/main --fail-on failing,suspect` as a PR check and git hook; importers for Spec Kit, OpenSpec and Kiro | ci, cli |
| E8 | Dogfood and benchmark | Write specs for 3–4 of our own capabilities (rev protocol, ID allocation, link validation, MCP pagination); measure agent tokens for "what does this PR affect?" against reading a spec folder | docs |

Rough size: 8 epics, about 30–35 stories. E1 blocks everything. E3 and E4 can
run in parallel after E1. E5 and E6 depend on E3 and E4.

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

## 7. Open questions for the reviewer

1. **Granularity.** Should each requirement be its own file (recommended:
   cheap reads, per-requirement `rev`, Doorstop-style), or should requirements
   be blocks inside one capability file (OpenSpec-style: fewer files, but
   needing block-level IDs and `rev`)?
2. **Code anchors.** In-code markers plus an optional `trace:` field
   (recommended), markers only, or front matter only?
3. **Requirement lifecycle.** Should it reuse the project workflow statuses, or
   use a per-type workflow (`draft → approved → implemented → deprecated`)?
   The per-type option means extending `project.yaml`, which is another
   data-model change.
4. **Grammar strictness.** Should EARS be enforced as an error, or only warned
   about (recommended: warn, configurable)?
5. **Scope of the first cut.** Is E7 interop in, or deferred to a later phase?
   Is the E6 web coverage matrix required for the milestone?
6. **Human-only areas.** E1 changes on-disk formats. Should it be done by a
   human, or by an agent under supervision in its own PR?
7. **Naming.** Is it `spec` + `requirement`, or `capability` + `requirement`,
   and which ID codes: `SP`/`RQ`, or `CAP`/`REQ`?

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
