---
title: Spec impact benchmark — replay kit
type: page
tags: [research, benchmark]
---

# Spec impact benchmark — replay kit

This folder holds what the [benchmark](../2026-09-25-spec-impact-benchmark.md) needs to be run again
without private files (`GIT-US-0170`): the diffs of the 14 replayed PRs, the hand verdicts, a driver
script and a summary script. Every path below is relative to this folder.

| File | What it is |
|---|---|
| `patches/P1.patch` … `P7.patch`, `C1.patch` | The **forward** diff (`git diff <sha>^ <sha>`) of the eight real commits of §2. The benchmark applies them **reversed**: `patch -R -F3 -p1`. |
| `patches/S1.patch` … `S5.patch`, `C2.patch` | The six hand edits of §2, as forward diffs (`patch -p1`). |
| `verdicts.json` | The hand verdicts: for each PR, the requirements whose behaviour the replayed diff really changes (21 in all: the recall truth set) and those that are test evidence only. Every other requirement a report names for that PR is unrelated. |
| `replay.sh` | Applies one PR, waits for Pando to settle, runs `spec impact` for tiers 1, 1+2, 1+2+3 and 3 (JSON twice and text once each), restores the tree. |
| `summarise.py` | Reads the output of `replay.sh` and prints the per-tier verdict counts, cumulative recall, tokens, determinism and latency. |

## What was reconstructed

The original replay patches and hand edits of `GIT-US-0137` and `GIT-US-0161` were kept outside the
repository. Nothing here is a copy of them.

- **P1–P7 and C1 come from git history.** Each patch is the diff between a commit and its parent,
  for the commits named in §2 of the benchmark (`44f72685`, `c0156e75`, `7ada705a`, `61ce11bf`,
  `337ce9d8`, `70e3fb96`, `418ddda6`, `853cc9cb`). Applied reversed on the base with `patch -R -F3`
  they touch the same files as the first run (10, 14, 14, 25, 7, 14, 14 and 27) and the same number
  of changed lines to within 2 (P7 612 against 610, C1 1,017 against 1,019). The count of applied
  hunks differs from §2 for four PRs: P1 18 of 19 (was 17), P3 31 of 46 (was 26), P5 16 of 31 (was
  14) and P6 26 of 32 (was 19). The first run's exact `patch` invocation is not known, so the hunk
  counts of the original are not reproducible; the symbols and files it changed are.
- **S1–S5 and C2 are rewritten from their one-line descriptions in §2.** The originals were never
  saved. They are the smallest edits that match each description and target the same function:
  - S1 makes `ItemPatch.conflictWith` return a field named `patch` instead of `nil` when the patch
    is refused (6 changed lines; §2 counted 7).
  - S2 makes `Allocator.nextNumber` honour the counter hint only when `WriteCounters` is set (2
    changed lines; §2 counted 4).
  - S3 adds `"fields": in.Fields` to the `item.list` parameters of `listItems` (1 line).
  - S4 raises `maxPageSize` from 100 to 200 (2 lines).
  - S5 gives `fromVault` a retry line for a stale refusal without conflicts (3 lines).
  - C2 makes `includes` in `internal/mcp/page.go` trim commas (2 lines).
- **`verdicts.json`** carries the hand verdicts of §5 and §9.3 of the benchmark and the ones added
  in §10. It is judgement, not measurement: a verdict is `behaviour` only when the replayed diff
  changes what the requirement states.

The patches apply to the base commit `7e60c1c4` (`docs(core): dogfood specs for link validation and
mcp pagination`). They do not apply to `main`.

## How to replay

1. **Build the binary under test** from the commit to measure: `make build` gives `bin/gintrack`.
2. **Clone the base** into a scratch directory and keep every replay out of your working repository:

   ```bash
   git clone <this repository> "$SCRATCH/repo" && git -C "$SCRATCH/repo" checkout 7e60c1c4
   ```

3. **Write a throwaway configuration** (the real one is never touched). `GINTRACK_CONFIG` names it,
   and its directory is also the cache directory, so Pando's per-repository directory lands there too:

   ```bash
   export GINTRACK_CONFIG=$SCRATCH/cfg/config.yaml
   gintrack add "$SCRATCH/repo"
   ```

   Then edit the file: under the repository set `semanticSearch: true`, under `search.pando` set
   `mode: managed` and `managed.binary` to the `pando` binary, and give `server` a port that is not
   7317 with `openBrowser: false`.
4. **Start the companion** with `gintrack serve --no-open --port <port>` in the background. In managed
   mode it starts one Pando for the repository (ADR-039), registers it as a code project and indexes
   it. Check `gintrack pando status --json` for `"state": "ready"`, then poll `code_index_status` on the
   instance's `mcpUrl` (the bearer token is in its `tokenFile`) until the job is `completed`. It took
   about 100 s for 1,124 files.
5. **Ingest the test results at the base**, so every hit is `suspect` rather than `untested`:

   ```bash
   cd "$SCRATCH/repo"
   go test -json ./internal/core/... ./internal/vault/... ./internal/mcp/... > "$SCRATCH/go.json"
   gintrack spec ingest "$SCRATCH/go.json"
   ```

6. **Replay and measure**:

   ```bash
   BENCH_REPO=$SCRATCH/repo GINTRACK=$PWD/bin/gintrack OUT=$SCRATCH/out \
     docs/research/spec-impact-benchmark/replay.sh P1 P2 P3 P4 P5 P6 P7 S1 S2 S3 S4 S5 C1 C2
   python3 docs/research/spec-impact-benchmark/summarise.py "$SCRATCH/out"
   ```

7. **Stop the companion** (`Ctrl+C` or `kill` the `serve` process). The managed Pando dies with it;
   check that no `pando` process points into `$SCRATCH`.

Pando indexes the repository once, at the base. The replayed diff is the working tree, so tiers 2 and
3 answer from the base index while tier 1 reads the diff. With the generated `.pando.toml`
(`KBWatch = true`) Pando also re-imports the documents a replay changes; `replay.sh` waits for that.

The Pando version matters: results are only comparable for one Pando build and one gintrack build.
`§10` of the benchmark states the ones it used.
