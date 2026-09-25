---
title: Spec impact benchmark — the impact report against reading the specs
created: 2026-09-25
updated: 2026-09-25
author: claude
status: measured
item: GIT-US-0137
rerun: GIT-US-0161
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

**Re-run with Pando (`GIT-US-0161`, [§9](#9-re-run-with-pando-tiers-2-and-3)).** The same 14 diffs
ran again with a Pando instance that had indexed the repository. Tier 2 is deterministic (224
repeated pairs byte-identical), but it hurts both budget and precision. It adds 22 hits to the
29 of tier 1, and only 1 of them is a behaviour hit (9 more are test evidence). The median report
grows from 190 to 261 tokens, and the worst from 716 to 1,393. That worst case is 5.9× the folder
read, below the 10× target. It closes none of the tier 1 misses of §5. Tier 3 gave no candidates:
in the shipped build it deadlocks (the CLI crashes after 30–90 s and the MCP tool hangs). With that
patched out, Pando's response cache made it `unavailable` on 14 of 14 PRs. If the cache is
followed by hand, it would have returned 3 candidates, none of them relevant.

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
   [§9](#9-re-run-with-pando-tiers-2-and-3) measured it later. It closes neither miss.
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
5. **Re-run this benchmark with tiers 2–3** (done by `GIT-US-0161`; see
   [§9](#9-re-run-with-pando-tiers-2-and-3)) once a Pando instance indexes the repository. That run
   would check whether tier 2 closes the misses in follow-up 2 and the P5 miss, and measure what
   the candidates cost.

## 9. Re-run with Pando tiers 2 and 3

`GIT-US-0161` ran the same PR set again, this time with Pando indexing the repository. The
question: what do tier 2 (transitive callers) and tier 3 (semantic candidates) add in hits,
precision and tokens?

### 9.1 Setup

- **Same diffs.** A new jj workspace sat on the same base, `7e60c1c4`. Every replay was applied
  exactly as in §2, from the saved patches. All 14 resulting diffs were byte-identical to the ones
  of the first run. The test results of the first run (`go test -json` at the base) were
  ingested again, so every requirement starts `passing` and every hit is `suspect`.
- **Pando.** Pando v1.0.1, a private `pando mcp-server --no-stdio` on a loopback port with a bearer
  token. It had its own data directory and a local `.pando.toml` with these settings:
  - `KBPath` is the workspace's `docs/`, with `KBWatch = false`.
  - `[TokenOptimization] BuildCodeGraph = true`.
  - Mesnada and the API server are off.

  The maintainer's Pando and gintrack configurations were not changed. The gintrack configuration
  was a copy of the maintainer's. It registers only the benchmark workspace and sets
  `search.pando.mcpUrl` and `mcpToken`.
- **Indexed before the run.** The code project is `www_git-in-track-us-0161-bench`, the id the
  impact seam derives from the repository root. It indexed the base in 56 s: 582 Go files, 14,720
  symbols, 51,447 call edges and 3,225 import edges. The knowledge base holds 756 documents in
  5,092 chunks. Nothing was re-indexed or polled during the run, so Pando answers from the base
  while the diff is the working tree.
- **Three binaries.**
  - *The first-run binary*: the `GIT-US-0137` build, which has the same tier 1 as §4–§5. This
    isolates what Pando adds.
  - *The stack head*: the build this change sits on, with `GIT-US-0157`, `GIT-US-0158` and
    `GIT-US-0159` on top of the first run. It shows the current product.
  - *Patched*: the stack head with two benchmark-only patches, which were not committed. The
    first answers `impact.query` and `impact.report` without the vault mutex (see §9.5). The
    second prints the tier-3 query text on stderr.
- **Runs.** Every PR ran twice per binary: `--tiers 1` and `--tiers 1,2`, as text and JSON. The
  MCP `spec_impact` ran over stdio with `tiers: [1, 2]`, as JSON and text. The patched binary also
  ran `--tiers 1,2,3`, `--tiers 3` and the MCP tool with the default tiers. The shipped stack
  head ran once more with the default tiers (all three), under a 200 s timeout.

### 9.2 Results per PR

This table uses the first-run binary for tiers 1 and 2. "T2 hits" counts requirements that only
tier 2 reached. "Corroborated" counts tier-1 hits that tier 2 gave a `call:` reason too. "B/E/U"
is the verdict of each tier-2 hit, with the categories of §5. Tokens are the report's `tokens`
field, which is the size of the MCP JSON result.

T3 always answered `unavailable` (§9.5). "T3 if cache followed" lists the candidates tier 3
would have added with the Pando cache paged by hand. "T3 cost" is what the `unavailable` tier-3
line adds to the report (patched binary, `1,2,3` against `1,2`).

| # | T1 hits | T2 hits | T2 B/E/U | Corroborated | T3 if cache followed | Tokens T1 | Tokens T1+2 | Text T1 | Text T1+2 | T3 cost |
|---|---:|---:|---|---:|---|---:|---:|---:|---:|---:|
| P1 | 8 | 0 | — | 3 | 0 | 716 | 874 | 380 | 381 | +99 |
| P2 | 5 | 2 | 0/1/1 | 3 | 0 | 491 | 755 | 236 | 329 | +99 |
| P3 | 5 | 5 | 1/0/4 | 3 | 0 | 397 | 929 | 234 | 464 | +100 |
| P4 | 1 | 12 | 0/8/4 | 0 | 0 | 171 | **1,393** | 82 | 616 | +3 ¹ |
| P5 | 0 | 0 | — | 0 | 0 | 77 | 81 | 41 | 43 | +99 |
| P6 | 0 | 0 | — | 0 | 0 | 78 | 82 | 42 | 44 | +99 |
| P7 | 0 | 0 | — | 0 | 0 | 78 | 82 | 42 | 44 | +99 |
| S1 | 2 | 0 | — | 0 | 0 | 210 | 208 | 122 | 121 | +99 |
| S2 | 4 | 1 | 0/0/1 | 1 | 2 (`SP-0002.R7`, `R8`: unrelated) | 309 | 400 | 184 | 230 | +99 |
| S3 | 2 | 0 | — | 0 | 0 | 187 | 185 | 104 | 103 | +99 |
| S4 | 0 | 0 | — | 0 | 1 (`SP-0004.R2`: unrelated) | 77 | 76 | 41 | 40 | +99 |
| S5 | 2 | 2 | 0/0/2 | 0 | 0 | 193 | 314 | 111 | 192 | +99 |
| C1 | 0 | 0 | — | 0 | 0 | 78 | 82 | 42 | 44 | +99 |
| C2 | 0 | 0 | — | 0 | 0 | 77 | 76 | 41 | 40 | +99 |

¹ On the stack head, P4 fills the 1,500 budget. With tiers 1–2 the page holds 10 of 16 hits, and
with tier 3 it holds 9, so the tier-3 line displaces a hit instead of adding tokens.

The first-run tier 1 is 190 tokens at the median here, not the 225 of §4. That is because §4 ran
the default tiers, and each report then carried two long `unavailable` tier lines.

### 9.3 Precision per tier

Every tier-2 hit was judged by hand against the requirement text and the replayed diff, as in §5.
"Evidence" is widened here to a test that verifies the requirement and now runs changed code.

| Hit | Verdict | Why |
|---|---|---|
| P2 `SP-0001.R2` | unrelated | `FileStore.AddComment calls Valid`. That is `CommentKind.Valid`, not the changed `LinkKind.Valid`. Pando resolves callees by name. |
| P2 `SP-0003.R6` | evidence | `TestValidateSpecRules` verifies R6 and calls the changed `validateItemLinks`. The rule does not change. |
| P3 `SP-0003.R3` | **behaviour** | `validateItemLinks` names `Kind.Inverse()` in the computed-only refusal. The replay removes the inverses of the spec kinds, so the refusal no longer names what to record instead. Tier 1 missed this, and §5 did not list it as a miss. |
| P3 `SP-0001.R2`, `R9`, `R10`; `SP-0002.R7` | unrelated | `FileStore.UpdateRequirement` and `CreateRequirement` call `linksHaveSpecConstruct`. That function gates the schema feature, not the rev protocol or number allocation. |
| P4 `SP-0001.R2`, `R3`, `R4` | evidence | Their verifying tests call the changed `writeItem`. Diagnostics gain file lines, but the rev rules do not change. |
| P4 `SP-0003.R1`, `R3`, `R4`, `R5`, `R6` | evidence | Their verifying tests call the changed `orderDiagnostics`. |
| P4 `SP-0001.R9`, `R10`; `SP-0002.R7` | unrelated | These are reached through implementation callers of `writeItem` whose behaviour does not change. |
| P4 `SP-0002.R8` | unrelated | `Index.requirementRefsTo calls add`. That is a different `add`. Here too, name resolution collides. |
| S2 `SP-0002.R5` | unrelated | `allocate` calls `nextNumber`, but the counter-hint change cannot repeat a number. |
| S5 `SP-0004.R1`, `R7` | unrelated | `listItems` calls `fromVault`, but a list never produces the stale refusal whose retry line changed. |

| Tier | Hits | Behaviour | Behaviour + evidence | Precision (strict) | Precision (lenient) |
|---|---:|---:|---:|---:|---:|
| 1 (§5) | 29 | 15 | 24 | 52 % | 83 % |
| 2, hits of its own | 22 | 1 | 10 | **5 %** | 45 % |
| 1 + 2 | 51 | 16 | 34 | 31 % | 67 % |
| 3, as shipped | 0 | — | — | — | — |
| 3, cache followed by hand | 3 | 0 | 0 | 0 % | 0 % |

- **Recall.** Tier 2 closes none of the §5 misses. The S4 constant has no call edge. The S3
  binding lives in unchanged code. The P5 removed call is a callee, and tier 2 walks callers. Its
  one behaviour hit (P3 `SP-0003.R3`) is new, and it was also missed by the manual review of §5.
- **Corroboration.** Tier 2 adds `call:` reasons to 10 tier-1 hits. On the stack head, those
  reasons change a hit's `kind`, which ranks it:
  - P1 `SP-0001.R2`, `R9` and `R10` go from `test-only` to `behaviour`. That is wrong: §5 judged
    all three evidence only. The `call:` reason comes from a production caller
    (`FileStore.UpdateRequirement`) that carries their markers.
  - P3 `SP-0003.R1` goes from `test-only` to `behaviour`. That is right.

### 9.4 Tokens and budget

Over the 12 PRs (controls excluded), with the report's `tokens`:

| Measure | T1 (first-run binary) | T1+2 (first-run binary) | T1 (stack head) | T1+2 (stack head) |
|---|---:|---:|---:|---:|
| Median | 190 | 261 | 353 | 401 |
| Worst | 716 | **1,393** (P4) | 1,421 (P4) | 1,493 (P4, 10 of 16 hits on the page) |
| Folder read ÷ median | 43× | 33× | 23× | 20× |
| Folder read ÷ worst | 11.4× | **5.9×** | 5.8× | 5.5× |
| Oracle ÷ median (10 PRs) | 10.4× | 6.4× | 4.5× | 4.0× |
| Oracle ÷ worst | 3.5× | **1.5×** | 1.5× | 1.4× |

- Tier 2 costs 48 tokens at the median and up to 1,222 (P4: 171 → 1,393). Its cost is the
  unrelated hits.
- With tier 2, the worst case breaks the 10× target against the folder read, and it comes close
  to the reading of the oracle spec (1.5×).
- The stack head's tier 1 already reaches that worst case without Pando. Through `decl:… uses
  Item`, `GIT-US-0158` adds 14 hits to P4, and through `uses ItemResult` it adds 12 `test-only`
  hits to P5. These were not judged here. The follow-up in §9.7 flags them.
- Tier 3 as shipped costs +99 tokens a report, for an `unavailable` line that quotes Pando's
  cache header. It adds no candidate.
- **Latency.** Pando is local, so latency is not a cost. Tiers 1–2 take a median of 0.69–0.72 s, the
  same as tier 1 (0.71 s), and 0.89 s at worst. Tiers 1–3 (patched) take a median of 0.86 s.

### 9.5 Why tier 3 gives nothing

Three independent faults, each enough on its own:

1. **Deadlock (shipped binary).** `Vault.Dispatch` holds the vault mutex for `impact.query` and
   `impact.report`. Tier 3 calls the Pando searcher, which re-enters the same vault to resolve its
   candidates: `Vault.Page` in the code leg, and `Vault.Item` and `Vault.Requirement` in the
   knowledge-base leg. `sync.Mutex` is not re-entrant.
   - **CLI.** `spec impact` with the default tiers crashed on all 14 PRs with `fatal error: all
     goroutines are asleep - deadlock!` after 30–90 s (exit 2).
   - **MCP.** Over stdio, `spec_impact` with the default tiers did not answer within 150 s. It
     holds the vault lock the whole time.
   - **Companion.** `gintrack serve` takes the same `Dispatch` path. That is read from the code,
     not run.

   Tier 2's Pando calls also run under that lock, for up to the 10 s call budget. Any session
   with `search.pando` configured is affected, because all three tiers are the default.
2. **Pando's response cache.** Pando's MCP server replaces any tool result over 15,000 bytes or
   300 lines with a `[Response cached: … cache_id …]` header and a preview, to be paged with
   `cache_read` (Pando `internal/llm/tools/cache_interceptor.go`). No setting turns this off.
   Tier 3 asks for 16 chunks (limit 8, over-fetched 2×). On all 14 PRs the result was
   16,433–23,478 bytes, which `internal/pando` cannot decode. So tier 3 reports `unavailable`.
   Any `search_semantic` page that large fails the same way.
3. **The query does not reach spec blocks.** `kb_search_documents` has no path filter here, and
   the requirement kind filter runs after it. Only 9 of the 224 top-16 chunks came from spec files.
   Most came from docs pages, stories and comments that quote the same symbol names. After the
   chunks are mapped onto blocks (the rule of `core.LocateRequirement`) and the refs that tiers
   1–2 already hold are removed, 3 candidates remain, all unrelated. The query is symbol names
   (`nextNumber`, `maxPageSize`), and these match prose about the code better than the EARS
   statements of a requirement do.

The ranking of the knowledge base is not stable either. 12 of 14 queries returned the same top 16
on a second call.

### 9.6 Determinism

- **Tier 2.** Every pair was byte-identical (`cmp`): 224 pairs in all, 6 per PR for each of the
  two unpatched binaries (tiers 1 and 1–2, text and JSON, and the MCP JSON and text with tiers
  1–2), plus 4 per PR for the patched binary.
- **Against one index.** This holds against one Pando index. Re-indexing with
  `BuildCodeGraph = false` turns every tier-2 answer into `2 ok 0` (see §9.8).
- **Tier 3.** Tier 3 is not meant to be deterministic, and none of its runs repeated
  byte-for-byte. The cause is the `unavailable` message: it embeds Pando's random `cache_id`.

### 9.7 Follow-ups

Tier 2 hurts both precision (5 % strict) and the budget (worst case 5.9× the folder). Tier 3 does
not work. The acceptance criteria ask for a follow-up in that case. These are proposed as stories
under `GIT-EP-0029`:

1. **Tier 3 deadlocks under the vault mutex** (bug, high). Run the impact backend outside the vault
   lock, over an index snapshot, or give the searcher a lock-free resolver. Tier 2's Pando calls
   should not hold the lock either. Add a test that runs `impact.query` with a searcher that
   resolves through the vault.
2. **Follow Pando's response cache in `internal/pando`** (bug). Page a `[Response cached …]` result
   with `cache_read`, or ask for fewer chunks. Remove the random `cache_id` from the tier message,
   which is also shorter that way. `search_semantic` is affected too.
3. **Aim tier 3 at requirement blocks.** Restrict the knowledge-base search to the specs folder,
   or index requirement blocks as their own documents. Build the query from the story title and
   Spec Delta rather than from bare symbol names. Re-measure with this PR set.
4. **Tier 2 precision.**
   - Drop callers reached through a name that several definitions share (`Valid`, `add`), or pin
     the callee with `code_find_symbol`.
   - Count a `call:` reason from a test caller as test evidence.
   - Do not let a production caller that carries several markers flip a `test-only` hit to
     `behaviour`.
5. **Report a missing code graph as `unavailable`.** Pando answers "No callers found … or the
   project lacks call edges" when `BuildCodeGraph` is off, and tier 2 reports that as `ok 0`.
   Document in docs/21 §6.1 that tier 2 needs `[TokenOptimization] BuildCodeGraph = true`.
6. **Bound the `decl:` reach of `GIT-US-0158`.** A changed type used everywhere, such as `Item` or
   `ItemResult`, floods tier 1 (P4 +14, P5 +12). Judge those hits, and cap or rank them.

### 9.8 The maintainer's environment, as found

This explains why a run with the existing configuration would show nothing:

- The gintrack configuration sets `search.pando.projectId` but no `mcpUrl`. So tiers 2 and 3
  answer `unavailable`.
- Every running `pando mcp-server` was started with `--no-http`. So there is no endpoint to point
  `mcpUrl` at.
- `~/.pando.toml` sets `[TokenOptimization] BuildCodeGraph = false`. A project indexed under it
  has no call edges. This benchmark's first index had 0 edges, and tier 2 answered `ok 0` on P1.
- The repository's existing code project is registered as `figma-linux` (indexed 2026-09-04),
  not under the id derived from the repository path that the impact seam asks for.
