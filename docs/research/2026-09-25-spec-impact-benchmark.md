---
title: Spec impact benchmark — the impact report against reading the specs
created: 2026-09-25
updated: 2026-09-25
author: claude
status: measured
item: GIT-US-0137
---

# Spec impact benchmark — the impact report against reading the specs

This page measures the success criterion of the Phase 11 overview
([§6](./2026-09-24-spec-driven-development-overview.md#6-success-criteria)). It is epic E8's
benchmark (`GIT-US-0137`). The criterion: the impact report for a typical PR is ≤ 1.5k tokens,
10× cheaper than an agent reading the relevant spec folder, and tiers 1 and 2 are deterministic.
It uses the four dogfood specs of `GIT-US-0135` and `GIT-US-0136` (`GIT-SP-0001` … `GIT-SP-0004`,
32 requirements with markers in `internal/core`, `internal/vault` and `internal/mcp`).

**Result.** The criterion holds against the spec folder. The typical PR's report is **225 tokens**
as the MCP `spec_impact` JSON result (median of 12 PRs; largest 751). As the CLI text form it is
**136 tokens**. Reading the spec folder costs **8,174 tokens**: **36×** the MCP JSON (10.9× at
worst) and **60×** the text. It does not hold against a perfect-foresight reader who opens only the
one spec file that holds the affected requirements: 8.8× (MCP JSON) and 14.6× (text). Two
repeated runs of tiers 1–2 were byte-identical on every PR. Of the 29 hits, 52 % flag a
requirement whose behaviour the diff changes, and 83 % also count the hits where only a test
that verifies the requirement changed.

## 1. Setup

- **Code.** Everything ran in the jj workspace of `GIT-US-0137`. The base is the dogfood-specs
  change `7e60c1c4` (`docs(core): dogfood specs for link validation and mcp pagination`) on top
  of the full Phase 11 stack. The `gintrack` binary was built from that tree.
- **Isolation.** The binary ran against a throwaway `GINTRACK_CONFIG` that registers only this
  workspace (the approach of `scripts/spec-check.sh`). No Pando is configured there, so tiers 2
  and 3 answer `unavailable`. There were no semantic candidates. No Pando project was indexed,
  queried or changed.
- **Results ingested.** `go test -json ./internal/core/... ./internal/vault/... ./internal/mcp/...`
  ran at the base: 3,325 tests passed. `gintrack spec ingest` then recorded the results at the
  base commit, and all 32 requirements were `passing`. Each replay is uncommitted work on top of
  that base. So every hit on a passing requirement is `suspect` (R-IMP-5 rule 2): changed, and
  not re-verified.
- **Tokenizer.** Both sides use the product's estimator, `core.EstimateTokens`: `ceil(bytes / 3)`
  (docs/03 R-IMP-9). No model tokenizer was used.

## 2. The PRs

There are twelve PRs and two negative controls.

**Real replays.** Seven are real changes from this repository's history that touched the
dogfooded capabilities. Each one was replayed on the base as its **reverse diff**, applied with
`patch -R -F3` in a temporary jj change. The reverse touches the same files, hunks and symbols as
the original. Where later changes moved the code, some hunks were rejected, and the replay
measures the hunks that applied.

**Faithful edits.** Five PRs are edits written for this benchmark. They reproduce the kind of
change the old squash commits made (`49c9559`, `0cd06e3`, `6d37d00`, `c941511`), whose diffs no
longer apply. Where possible they target a real open gap, such as the list-cursor binding gap and
the `ItemPatch.conflictWith` observation in the `GIT-US-0135` comment.

**Negative controls.** Two PRs change code that no spec covers.

Each temporary change was abandoned after it was measured. Raw outputs are kept outside the
repository.

| # | PR (source) | Kind | Hunks applied | Files | Symbols | Changed lines |
|---|---|---|---|---:|---:|---:|
| P1 | `44f72685` fix(core): never report a refused requirement patch as already applied (`GIT-US-0151`) | real replay | 17/19 | 10 | 7 | 213 |
| P2 | `c0156e75` fix(core): make spec inverse links computed-only (`GIT-US-0106`) | real replay | 34/37 | 14 | 13 | 283 |
| P3 | `7ada705a` feat(core): add spec link kinds and requirement-ref link targets (`GIT-US-0106`) | real replay | 26/46 | 14 | 17 | 186 |
| P4 | `61ce11bf` fix(core): report requirement diagnostics on file lines (`GIT-US-0144`) | real replay | 38/38 | 25 | 19 | 577 |
| P5 | `337ce9d8` feat(mcp): address single spec requirements from mcp tools (`GIT-US-0122`) | real replay | 14/31 | 7 | 5 | 97 |
| P6 | `70e3fb96` feat(core): read and patch one requirement through the vault (`GIT-US-0107`) | real replay | 19/32 | 14 | 10 | 204 |
| P7 | `418ddda6` fix(core): clear impact suspect once re-verified at the diff's head (`GIT-US-0148`) | real replay | 29/29 | 14 | 12 | 610 |
| S1 | `ItemPatch.conflictWith` names the patch instead of answering "already applied" (rev protocol, after `49c9559`) | faithful edit | — | 1 | 1 | 7 |
| S2 | `Allocator.nextNumber` honours the counter hint only when it writes counters (allocator, after `0cd06e3`) | faithful edit | — | 1 | 1 | 4 |
| S3 | `listItems` passes `fields` into the cursor-bound query (pagination, after `6d37d00`) | faithful edit | — | 1 | 1 | 1 |
| S4 | `maxPageSize` 100 → 200 (pagination, after `6d37d00`) | faithful edit | — | 1 | 1 | 2 |
| S5 | `fromVault` gives a stale refusal with no conflicts its own retry line (rev protocol, after `c941511`) | faithful edit | — | 1 | 1 | 3 |
| C1 | `853cc9cb` feat(server): list branches and recent commits for impact pickers (`GIT-US-0149`) | control, real replay | 53/53 | 27 | 13 | 1019 |
| C2 | `includes` in `internal/mcp/page.go` trims commas: an untraced function in a traced file | control, edit | — | 1 | 1 | 2 |

## 3. The two ways

**A — without the tool: read the specs.** Three baselines, measured on the spec files' bytes.

- **Folder read** (the criterion's baseline): all of `docs/.pmngr/specs/`, 24,521 bytes = **8,174
  tokens**. `GIT-SP-0001` is 2,480, `GIT-SP-0002` 1,943, `GIT-SP-0003` 2,062 and `GIT-SP-0004`
  1,691.
- **Path grep**: the spec files that name the basename of a changed non-test Go file. The specs
  name code paths only in two `Scope` lines (`validate.go` and `requirements.go`, both in
  `GIT-SP-0003`), so the grep surfaces a spec for only 3 of the 12 PRs. A grep is not a usable
  way to find the specs: it is cheap because it misses them. The grep baseline is reported but
  is not the comparison.
- **Oracle**: only the spec file(s) that hold the requirements the diff *really* affects, judged
  by hand, including the ones tier 1 missed. This is the most conservative baseline. No agent
  without the tool can do this well, because it would first have to know which spec to open.

The cost of reading the diff itself is the same both ways and is left out.

**B — with the tool.** `gintrack spec impact --since 7e60c1c4` with the default budget (1,500)
and the default tiers:

- `--format text`: the CLI text form.
- `--format json`: the CLI JSON, printed indented.
- `spec_impact` over `gintrack mcp` (stdio, read-only), `format: json` and `format: text`: the
  tool result's text content. This is what an agent's context receives, and its size equals the
  `tokens` field the report states.

## 4. Results

| # | Hits | Behaviour | Evidence only | Unrelated | Candidates (t3) | B text | B MCP JSON | B MCP text | B CLI JSON | A oracle | A folder | Folder ÷ MCP JSON |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| P1 | 8 | 2 | 6 | 0 | — | 408 | 751 | 457 | 1,093 | 2,480 | 8,174 | 10.9× |
| P2 | 5 | 3 | 2 | 0 | — | 264 | 526 | 311 | 778 | 2,062 | 8,174 | 15.5× |
| P3 | 5 | 4 | 1 | 0 | — | 263 | 432 | 309 | 669 | 2,062 | 8,174 | 18.9× |
| P4 | 1 | 1 | 0 | 0 | — | 111 | 206 | 153 | 317 | 2,062 | 8,174 | 39.7× |
| P5 | 0 | — | — | — | — | 69 | 113 | 110 | 186 | 3,633 | 8,174 | 72.3× |
| P6 | 0 | — | — | — | — | 70 | 113 | 111 | 186 | — | 8,174 | 72.3× |
| P7 | 0 | — | — | — | — | 70 | 113 | 111 | 186 | — | 8,174 | 72.3× |
| S1 | 2 | 2 | 0 | 0 | — | 150 | 245 | 193 | 384 | 2,480 | 8,174 | 33.4× |
| S2 | 4 | 2 | 0 | 2 | — | 212 | 344 | 257 | 546 | 1,943 | 8,174 | 23.8× |
| S3 | 2 | 0 | 0 | 2 | — | 133 | 222 | 175 | 361 | 1,691 | 8,174 | 36.8× |
| S4 | 0 | — | — | — | — | 69 | 113 | 110 | 186 | 1,691 | 8,174 | 72.3× |
| S5 | 2 | 1 | 0 | 1 | — | 139 | 228 | 182 | 367 | 2,480 | 8,174 | 35.9× |
| C1 | 0 | — | — | — | — | 70 | 113 | 111 | 186 | — | 8,174 | — |
| C2 | 0 | — | — | — | — | 69 | 113 | 110 | 186 | — | 8,174 | — |

"Candidates (t3)" is `—` everywhere: tier 3 was `unavailable` (no Pando), and so was tier 2. No
report was truncated. The largest one (P1, 8 hits) used half the budget.

**Summary over the 12 PRs (controls excluded)**

| Measure | Median | Worst |
|---|---:|---:|
| B, MCP `spec_impact` JSON (the agent default) | **225** tokens | 751 |
| B, MCP `spec_impact` text | 179 | 457 |
| B, CLI text | **136** | 408 |
| B, CLI JSON as printed (indented) | 364 | 1,093 |
| A folder ÷ B MCP JSON | **36.3×** | 10.9× |
| A folder ÷ B MCP text | 45.8× | 17.9× |
| A folder ÷ B CLI text | **60.1×** | 20.0× |
| A oracle ÷ B MCP JSON (10 PRs with a spec really affected) | 8.8× | 3.3× |
| A oracle ÷ B MCP text | 11.3× | 5.4× |
| A oracle ÷ B CLI text | 14.6× | 6.1× |
| A path grep ÷ B CLI text (the 3 PRs it surfaced a spec for) | 7.8× | 7.8× |

Over only the 8 PRs with at least one hit, the median is 294 tokens (MCP JSON), 28.6× the
folder read and 6.6× the oracle.

## 5. Precision of tiers 1–2

Every hit was judged by hand against its spec text and the replayed diff:

- **behaviour** — the diff changes what the requirement states.
- **evidence only** — a test that verifies the requirement changed, but the requirement's
  behaviour did not.
- **unrelated** — neither.

Only tier 1 answered, so tier 1 is what was judged.

| Hit | Verdict | Why |
|---|---|---|
| P1 `SP-0001.R3`, `R4` | behaviour | `requirementConflicts` loses the named-fields report for a refused patch. That is exactly R3 and R4. |
| P1 `SP-0001.R2`, `R5`, `R7`, `R8`, `R9`, `R10` | evidence only | Reached only through `TestRequirementRevProtocol` or `TestRequirementUpdateConflictMatrix`. The removed subtests are about R3 and R4, but the test function verifies all six. |
| P2 `SP-0003.R2`, `R3` | behaviour | `validateItemLinks` stops refusing `implemented_by` and `modified_by`: inverse kinds can be stored again. |
| P2 `SP-0003.R5` | behaviour | The `!ComputedOnly()` guard goes, so the target-type check now runs for the inverse kinds. |
| P2 `SP-0003.R1`, `R4` | evidence only | `LinkKind.Valid` changed only its doc comment. The target-grammar test changed only its computed-only cases. |
| P3 `SP-0003.R1` | behaviour | The "unknown link kind" case moves from `replaces` to `supersedes`, which changes the list of known kinds. |
| P3 `SP-0003.R2` | behaviour | `Graph.Related` and requirement nodes leave the link graph that computes inverses. |
| P3 `SP-0003.R6` | behaviour | `validateRequirementEntry` rewrites the three requirement link kinds. |
| P3 `SP-0003.R7` | behaviour | `checkReferentialIntegrity` stops naming requirement targets in dangling warnings. |
| P3 `SP-0003.R4` | evidence only | Same test function as R1, but a kind case rather than a target case. |
| P4 `SP-0003.R7` | behaviour | Dangling warnings lose their file line. |
| S1 `SP-0001.R3`, `R4` | behaviour | The exact rule changed: a refused patch no longer yields an empty list. |
| S2 `SP-0002.R1`, `R4` | behaviour | How the maximum is computed and how the hint is used both change. |
| S2 `SP-0002.R2`, `R3` | unrelated | Gap filling and number permanence are untouched. `nextNumber` carries all four markers. |
| S3 `SP-0004.R1`, `R7` | unrelated | Limit clamping and body stripping are untouched. The change concerns cursor binding (R4 and R5), which is not traced to `listItems`. |
| S5 `SP-0001.R6` | behaviour | The retry line of the stale refusal changes. |
| S5 `SP-0001.R2` | unrelated | The refusal itself and its `currentRev` are unchanged. |

**Precision.** Of 29 hits, 15 are behaviour (**52 %**) and 24 are behaviour or evidence (**83 %**).
Evidence-only hits come from test-function granularity: one changed table case flags every
requirement its test function `Verifies`. Unrelated hits come from one function carrying several
`Implements:` markers.

**Misses (tier 1 recall).** The tool missed requirements that really changed in five places:

- **S4.** `maxPageSize` is a package-level constant. No traced symbol spans it, so
  `SP-0004.R1` (the cap of 100) was missed.
- **S3.** The binding it touches lives in the core query. That code was not changed, so the
  `listItems` hits are the wrong ones.
- **P5.** The replay removes `registerSpecTools(s)` from `registerTools`. That unregisters
  `create_requirement`, `update_requirement` and the requirement projection (`SP-0002.R7`,
  `SP-0004.R6`), but a removed call reaches no marker, and tier 2 walks callers, not callees.
- **P6.** The replay removes the vault's routing of requirement refs to a mount. In a
  single-project workspace no requirement's behaviour changes, so this is not counted as a miss.
- **Controls.** Both controls had 0 hits. The P7 replay had 0 hits too: the impact resolver has
  no spec. C2 shows that symbol granularity keeps an untraced function in a traced file
  (`page.go`) out of the report.

## 6. Determinism

Every PR and control ran twice, in six forms:

- `--tiers 1,2` text and JSON;
- the default tiers, text and JSON;
- `spec_impact` over MCP, JSON and text.

All 84 pairs were byte-identical (`cmp`). Tier 2 was `unavailable` in every run. Its
determinism against a live Pando answer is covered by the golden tests of `internal/impact`
(R-IMP-7), not by this benchmark.

## 7. Conclusions

1. **The criterion holds against the spec folder, with a wide margin.** The typical report is
   225 tokens over MCP (136 as CLI text), 15 % of the 1.5k budget. It is 36× cheaper than
   reading `docs/.pmngr/specs/`, and 10.9× in the worst case. A PR that affects no requirement
   costs about 110 tokens instead of 8,174.
2. **Against perfect foresight the saving is 8.8× (MCP JSON) and 11–15× (text).** The dogfood
   folder is small: four specs, 32 requirements. Folder reads grow with every spec, while the
   report grows with the number of hits, so the margin widens as specs accumulate. The oracle
   ratio stays about the same, since one spec file costs about 2k tokens.
3. **The report is cheap, but half of what it flags is noise**: 52 % behaviour, 83 % counting
   test evidence. An agent still has to read the hit requirements to decide, but it reads only
   those, by ref, instead of whole specs.
4. **Tier 1 misses constants and removed calls.** Tier 2, with Pando, was not measured here.
5. **Marker hygiene matters.** Put an `Implements:` marker on the narrowest function that carries
   the rule. `nextNumber` with four markers and `fromVault` with two produced every unrelated hit.

## 8. Proposed follow-ups

These are proposals. Criterion 3 is met, so none of them is required by `GIT-US-0137`.

1. **Separate evidence-only hits from behaviour hits** (`internal/impact`, `core.RenderImpactReport`).
   A hit whose only reasons are changed test symbols is evidence, not a behaviour change. Either
   rank it after the behaviour hits under its own reason prefix, or narrow a Go test's span to
   the changed `t.Run` subtest or table case. This would have removed 9 of 29 hits (31 %) from
   the head of the ranking.
2. **Reach traced symbols through a changed package-level declaration** (tier 1, deterministic).
   When a diff changes a top-level `const`, `var` or `type` that no traced symbol spans, add a
   hit for the traced symbols in the same package that reference it, with a reason such as
   `decl:<file>#<name>`. S4 would then have found `SP-0004.R1`.
3. **Document the tier 1–2 blind spots** in docs/03 §21.11 and docs/08 §4.21: package-level
   declarations, removed calls (tier 2 walks callers, not callees), and test-function
   granularity. Add a marker-placement guideline: one marker on the narrowest symbol.
4. **`spec impact --format json` prints indented JSON.** It prints about 45 % more bytes than the
   `tokens` it reports (1,093 against 751 for P1). Either print it compact, like the MCP result,
   or document that the budget measures the compact form.
5. **Re-run this benchmark with tiers 2–3** once a Pando instance indexes the repository. That run
   would check whether tier 2 closes the misses in follow-up 2 and the P5 miss, and measure what
   the candidates cost.
