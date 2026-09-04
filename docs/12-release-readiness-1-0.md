---
title: 1.0 release readiness
type: page
tags: [release, status]
---

# 12 — 1.0 release readiness

This document is the evidence record for the 1.0 release. It answers one question per
row: **is this criterion satisfied, and what proves it?** Nothing here is marked satisfied
on the strength of a plan or an intention; every "yes" names a test, a file or a command
whose output was observed.

It is deliberately unflattering where it has to be. `docs/11-roadmap.md` states what we
set out to build; this document states what we actually built, and
[§5](#5-known-gaps-shipped-in-10) lists what we did not.

> **Status of the tag.** At the time of writing, `v1.0.0` has **not** been pushed. The
> repository carries no tags at all, so no 0.x release was ever published. Everything in
> this document is pre-tag verification; [§6](#6-what-a-maintainer-still-has-to-do) is the
> remaining work, and it belongs to a human.

---

## 1. Verification run

All figures below come from one run on 2026-09-04, Linux/amd64, on branch
`feat/phase-6-retros-metrics`, at commit `c524914`.

| Gate | Command | Result |
| --- | --- | --- |
| Lint | `make lint` | **pass** — golangci-lint v2.5.0 (run via `go run`, the version CI pins): `0 issues.`; `go vet` clean over 12 packages; ESLint `--max-warnings 0` clean; `tsc -b` clean; both workflow files parse as YAML |
| Go tests | `make test` | **pass** — 515 Go test functions over 10 packages, `-race`, all `ok` |
| Web tests | `make test` | **pass** — Vitest: 54 test files, 476 tests, 0 failures |
| WASM build | `make wasm` | **pass** — `web/public/core.wasm` + `wasm_exec.js` produced |
| WASM smoke | `make wasm-smoke` | **pass** — `scripts/wasm-smoke.mjs`: 32/32 checks |
| Binary build | `make build` | **pass** — `bin/gintrack` with `web/dist` embedded |
| Release config | `make release-check` | **pass** — `goreleaser check`: 1 configuration file validated (2 deprecation warnings for `dockers:`/`docker_manifests:`, see §5) |
| Snapshot release | `goreleaser release --snapshot --clean --skip=publish,docker` | **pass** — 6 archives, `checksums.txt`, `dist/homebrew/Casks/gintrack.rb`, `dist/scoop/bucket/gintrack.json` |
| Checksums | `sha256sum -c checksums.txt` | **pass** — all 6 archives match |
| Snapshot binary | `./dist/gintrack_linux_amd64_v1/gintrack version` | **pass** — reports version, commit, date, `by: goreleaser`, `ui: embedded`, `core: schema v1` |
| Container image | `goreleaser release --snapshot --skip=publish` | **partial** — the amd64 image builds and `docker run … version` works; the **arm64 image fails on this host** (`exec /bin/sh: exec format error`) because binfmt emulation is not installed. Install it with `docker run --privileged --rm tonistiigi/binfmt --install arm64`; the release workflow does the equivalent with `docker/setup-qemu-action@v3` |

### Go coverage by package

| Package | Coverage |
| --- | --- |
| `cmd/gintrack` | 75.5 % |
| `cmd/gintrack/output` | 93.1 % |
| `internal/config` | 84.1 % |
| `internal/core` | **83.6 %** |
| `internal/core/osfs` | 73.6 % |
| `internal/gitops` | 77.4 % |
| `internal/mcp` | 85.6 % |
| `internal/server` | 77.6 % |
| `internal/vault` | 78.1 % |
| `internal/watcher` | 83.7 % |

`internal/core` is at 83.6 %, below the ≥ 85 % that milestone 1 set as an exit criterion.
That criterion is therefore **not met** and is recorded as such in §2.

---

## 2. Milestone exit criteria

Legend: **yes** — verified, with the proof named. **partial** — the behaviour exists but
the criterion as written is not fully demonstrated. **no** — not done.

### Milestone 1 — Foundations (`GIT-M-0001`, Phase 0)

| Exit criterion | Status | Evidence |
| --- | --- | --- |
| `make build` produces a `gintrack` binary on Linux, macOS and Windows | **yes** | `make build` here; the GoReleaser snapshot cross-compiles all six `GOOS`/`GOARCH` targets and the archives were produced and checksummed |
| `make test` is green; `internal/core` coverage ≥ 85 % | **partial** | `make test` is green; `internal/core` measures **83.6 %**, 1.4 points short |
| Parser round-trips every fixture byte-for-byte except intentional normalisation, verified by golden tests | **yes** | `internal/core/frontmatter_test.go`: `TestSerializeItemGolden`, `TestSerializeCommentGolden`, `TestRoundTripFixtures`, `TestRoundTripDogfoodBacklog`, `TestUnknownKeysSurviveARoundTrip`, `TestSerializeIsDeterministic`; goldens under `internal/core/testdata/golden/` |
| The same core compiles to WASM and answers a smoke query | **yes** | `make wasm-smoke` → 32/32; `scripts/wasm-smoke.mjs` calls `gintrackCore.version()`, `vault.load` and `item.list` against the real module |
| A tagged `v0.1.0` produces all six archives plus `checksums.txt` | **partial** | The artifact production is proven by the snapshot run (6 archives + `checksums.txt`, all verified); **no tag was ever pushed**, so the tagged path itself is unexercised |

### Milestone 2 — Browser-only MVP (`GIT-M-0002`, Phase 1)

| Exit criterion | Status | Evidence |
| --- | --- | --- |
| A new user opens `testdata/fixtures/project-basic` and creates, edits and reads a story without a terminal, in under two minutes | **partial** | The flow is implemented and covered by Vitest component/hook tests; the *timed*, human-in-the-loop measurement was never recorded |
| A 5,000-file vault indexes in under 3 s; a single changed file re-indexes in under 100 ms | **partial** | `BenchmarkBuild2000Items` and `BenchmarkApplyOneFileEvent` (`internal/core/index_test.go`) measure the native core at 2,000 items; there is **no 5,000-file browser measurement** on record |
| The UI thread is never blocked for more than 50 ms during indexing (measured) | **no** | The core runs in a Web Worker by construction, but no main-thread blocking measurement exists |
| Every write produces a file the Go native parser reads back identically | **yes** | The same `internal/core` serializer runs in both modes (ADR-003); `TestRoundTripFixtures` and `TestRoundTripDogfoodBacklog` prove the round trip, and `wasm-smoke` proves the WASM build runs that code |
| Firefox and Safari load the vault read-only and say so | **yes** | `web/src/fs/support.ts` detects the absence of `showDirectoryPicker`; the fallback and its banner are documented in `docs/05-web-app.md` §"read-only fallback" and covered by Vitest |

### Milestone 3 — Companion CLI (`GIT-M-0003`, Phase 2)

| Exit criterion | Status | Evidence |
| --- | --- | --- |
| Editing a file in an external editor updates the open UI in under 300 ms | **partial** | `internal/watcher` debounces and streams events, and `internal/server/events_test.go` covers the WebSocket path; the 300 ms figure was never measured end to end |
| A 50,000-file vault indexes natively in under 5 s | **no** | No benchmark of that size exists |
| The app behaves identically in both modes for every Phase 1 flow (one shared E2E suite, run twice) | **no** | **There is no Playwright suite.** `web/` has no `e2e/` directory and no Playwright dependency; parity rests on the shared core and on unit tests |
| The server refuses a non-loopback bind without `--allow-remote`, and rejects a foreign `Origin` | **yes** | `internal/server/server_test.go:TestNewRefusesTokenlessNonLoopbackBind`; Origin checks covered in `internal/server/api_test.go` and `events_test.go` |

### Milestone 4 — Team repository and boards (`GIT-M-0004`, Phase 3)

| Exit criterion | Status | Evidence |
| --- | --- | --- |
| A board in `testdata/fixtures/team-basic` shows cards from two projects, one cloned and one remote-only | **yes** | `internal/core/boardview_test.go` builds board views over the fixture including a snapshot-backed remote project (`.pmngr/index/WEB.json`); `internal/core/remoteref_test.go` covers the snapshot reader |
| Dragging a card rewrites exactly one item file's `status` and the board's `order:`, and nothing else | **yes** | `internal/core/boardview_test.go` and the `move_on_board` tests in `internal/mcp` assert the write set |
| Two people moving different cards produce a mergeable YAML diff (scripted concurrent-edit test) | **yes** | `internal/core/boardview_test.go:TestConcurrentMovesMerge` |
| WIP limits are enforced visually and cannot be silently exceeded | **yes** | `wip_limit_exceeded` is returned by `move_on_board` unless `force` is set (`internal/mcp`), and the columns render their limits |

### Milestone 5 — Git sync (`GIT-M-0005`, Phase 4)

| Exit criterion | Status | Evidence |
| --- | --- | --- |
| Two clones edited concurrently are reconciled through the UI without a terminal, including one real conflict | **partial** | `internal/gitops` and `internal/server/conflicts_test.go` cover detection and resolution, and the conflict UI exists; **applying** a resolution requires the system-git backend — go-git refuses with `git_unsupported` (`internal/gitops/conflicts_gogit.go`). The two-clone scenario was never run end to end through the UI |
| No credential is ever written to disk or to `localStorage` (verified by test) | **yes** | `internal/gitops/credentials_test.go:TestNoCredentialReachesDisk`; browser tokens are held in a module closure for the session only |
| Commit on save produces one commit per logical edit, not one per keystroke | **partial** | Implemented and tested for companion mode (`internal/gitops/committer.go`); **commit on save does not work in browser-only mode** — the settings render but nothing is committed |
| A push failure leaves a recoverable working tree with an actionable message | **yes** | `internal/gitops/sync_*` return typed errors surfaced by the sync UI; covered in `internal/gitops` tests |

### Milestone 6 — MCP server and agent workflows (`GIT-M-0006`, Phase 5)

| Exit criterion | Status | Evidence |
| --- | --- | --- |
| An agent over MCP picks a `todo` story, moves it to `in_progress`, opens a PR and comments — no human file edits | **partial** | Every tool the loop needs ships and is tested (`internal/mcp`), and this repository's own backlog was moved by agents through these files; a **single agent completing a story end to end from `AGENTS.md` alone has never been demonstrated and recorded** |
| Two agents writing the same item concurrently produce one success and one `rev` conflict, never a lost update | **yes** | `internal/mcp/locking_test.go`, including `TestStaleRevisionTeachesTheRetry`; `rev` is a read-time content hash, never stored |
| The tool schemas are stable and documented; a schema change bumps MINOR | **partial** | The twelve tools are documented in `docs/08-mcp-server.md` and the policy is written in `docs/09` §7, but **no golden snapshot pins the tool schemas**, so a schema change cannot fail a test |

### Milestone 7 — Retrospectives, metrics and 1.0 (`GIT-M-0007`, Phase 6)

| Exit criterion | Status | Evidence |
| --- | --- | --- |
| A full sprint planned, run, closed and retrospected inside the tool by this project's own team | **partial** | `docs/.pmngr/` is a real backlog that drove every phase, and sprints, boards and retros are implemented and covered by tests; a **complete plan → run → close → retro cycle over this backlog is not recorded** |
| Burndown and cumulative flow match a hand-computed reference for a fixture sprint | **yes** | `internal/core/metrics_test.go`: `TestBuildSprintMetricsBurndownMatchesTheReference`, `TestBuildSprintMetricsCumulativeFlowMatchesTheReference`, `TestBuildSprintMetricsFlowStatsMatchTheReference`, plus `TestBuildSprintMetricsDegradesHonestly` |
| All accessibility checks pass at WCAG 2.1 AA for the primary flows | **no** | `eslint-plugin-jsx-a11y` runs in `make lint`, which is a linter, not an audit. **No WCAG 2.1 AA audit was performed** |
| `brew install`, `scoop install`, `docker run` and `go install` each produce a working `gintrack`, verified on a clean machine | **partial** | The cask and the Scoop manifest are generated by the snapshot run and the amd64 container image runs `gintrack version` correctly here; **nothing has been verified on a clean machine, because nothing is published until the tag** |
| The data model is frozen at `schemaVersion: 1` with a documented 0.x migration path | **partial** | The on-disk layout is frozen at `schema: 1` (`internal/core.SupportedSchema`, `project.yaml:schema`) and the evolution rules are `docs/03-data-model.md` §19. **`gintrack migrate` does not exist** — see §5, and note that no 0.x release was ever published, so there is nothing in the field to migrate from |

> **Naming.** The roadmap and the story say `schemaVersion`; the field on disk is
> `schema`, in `project.yaml` and `team.yaml`, and the constant is
> `core.SupportedSchema`. `docs/09` §7 has been corrected to use the real name.

---

## 3. Vision goals

Against `docs/01-vision-and-scope.md` §5.

| Goal | Status | Evidence / shortfall |
| --- | --- | --- |
| **G1** Full backlog as Markdown in the project repo | **yes** | `internal/core` model, parser and validator; this repository's own `docs/.pmngr/` is the proof by use. The "team runs a two-week sprint without leaving the repo" test has not been run as a timed exercise |
| **G2** Documentation folder as a first-class knowledge base | **yes** | GFM tables, task lists, footnotes, callouts, wikilinks and Mermaid all render; covered by Vitest in `web/src/features/kb` |
| **G3** Work in a browser with zero installation | **partial** | Full CRUD in Chromium via the File System Access API; Firefox and Safari are read-only, and browser-mode git needs a CORS proxy |
| **G4** Strictly better with the CLI installed | **yes** | `gintrack serve`, fsnotify watching, native indexing, native git, and automatic upgrade of the web app when the companion is detected |
| **G5** Aggregate multiple projects onto one team board | **yes** | `team.yaml`, Kanban and Scrum boards, `ref: <projectKey>/<itemId>`, and read-only remote references from `.pmngr/index/<KEY>.json` |
| **G6** Git as the only sync mechanism | **partial** | `gintrack sync` (fetch → integrate → push), conflict UI and safe credentials in companion mode. In the browser: no commit on save, no rebase, no SSH, and a CORS proxy is required. The go-git backend fast-forwards only and cannot apply a conflict resolution |
| **G7** Agents as first-class participants | **yes** | `gintrack mcp` on stdio and `POST /mcp`; twelve tools; `rev` optimistic locking; `AGENTS.md`. Retro, metrics and sync tools remain planned |
| **G8** The full agile loop | **partial** | Sprints, retrospectives, improvement actions, burndown and cumulative flow all ship. Board-level cumulative flow and cross-sprint velocity do not |
| **G9** Never lock in data | **yes** | Every artifact is a Markdown or YAML file; the schema is `docs/03-data-model.md`; unknown keys survive a round trip (`TestUnknownKeysSurviveARoundTrip`) |
| **G10** Installable in one step | **yes, pending the tag** | GoReleaser produces six archives plus `checksums.txt`, a Homebrew cask, a Scoop manifest and multi-arch images from one tag. None of it exists until a maintainer pushes `v1.0.0` |

---

## 4. Release checklist (docs/09 §9), pre-tag portion

| Step | State |
| --- | --- |
| `main` green in CI | Not applicable here: the work is on `feat/phase-6-retros-metrics`, which has not been merged |
| `make lint test` passes locally | **done** on Linux. **Not run on macOS**, which the checklist also requires |
| `make wasm-smoke` passes | **done** — 32/32 |
| `make release-check` | **done** |
| `make release-snapshot` produces 6 archives, checksums, cask, Scoop manifest, both images | **partial** — everything except the arm64 image, which needs binfmt on this host |
| Tap and bucket repositories exist; `HOMEBREW_TAP_TOKEN` and `SCOOP_BUCKET_TOKEN` set | **not done** — maintainer action, see §6 |
| Snapshot binary runs; `gintrack serve` loads the embedded app | **partial** — `gintrack version` verified from the archive and from the container; the served UI was not opened in a browser in this run |
| Data model changes accompanied by a migration and a `schema` bump | **n/a** — the layout stays at `schema: 1` |
| `CHANGELOG.md` reviewed | **done** — [`CHANGELOG.md`](../CHANGELOG.md) was written for this release |
| Documentation reflects the release | **done** — see the "Documentation" heading of the 1.0 entry in `CHANGELOG.md` |
| `README.md` installation section mentions the version | **done** |
| The version milestone is closed and its stories are `done` | **not done** — deliberately. Stories are left `in_review`; closing them is the maintainer's call |

---

## 5. Known gaps shipped in 1.0

These are real, user-visible limitations of the 1.0 build. They are repeated in
`CHANGELOG.md` so that nobody has to find this file to learn them.

### Browser-only mode

- **Commit on save does not work in the browser.** The setting renders and previews the
  message template, but nothing is committed. Use the companion CLI.
- **Firefox and Safari are read-only.** No `showDirectoryPicker` means no writes and no
  git. The fallback loads the folder into memory and indexes files above 5 MB by metadata
  only.
- **Browser git needs a CORS proxy**, because git hosts send no permissive CORS headers.
  Until one is configured, git in the tab is disabled.
- **No SSH remotes** from a tab; **no rebase** (the integration strategy is forced to
  `merge`); no signing, hooks, submodules or LFS.
- Clones are shallow (`depth: 50`, single branch); history beyond that boundary is not
  available, and deepening defers to the companion.
- Git credentials are session-only, in memory. Encrypted token storage in IndexedDB is not
  implemented — which is a limitation, not a defect.
- **The dedicated git Web Worker was not split out.** Git work shares the existing worker
  and credentials live in a module closure.
- Browser metrics degrade to `provenance.source: "updated"`: state before an item's
  `updated` timestamp is reported as `unknown` (hatched band), not guessed.
- Saved views and column layouts live in `localStorage`, not in the repository.

### Git sync

- **`git.dirtyPolicy` is not implemented.** The `stash` and `ask` values are documented but
  the key is not read.
- **Branch policy is not implemented**: no `user-branch` mode, no `autoPr`, no host URL
  templates. Every repository syncs its checked-out branch against its own upstream.
- **Per-repository `git:` overrides** (`workspaces[].repos[].git`) are not implemented;
  settings are per workspace.
- The go-git backend can only fast-forward — a diverged branch fails with
  `git_unsupported` — cannot sign commits, cannot abort or continue a rebase or merge, and
  **cannot apply a conflict resolution** (reading conflicts works on both backends).
  Install system git and select that backend for the full range.
- There is no keychain integration, no terminal credential prompt and no plaintext
  credentials file. That was a deliberate cut, not an oversight.

### Data model and CLI

- **`gintrack migrate` does not exist.** `docs/03-data-model.md` R-EVO-4 specifies it. It
  is unnecessary today because no 0.x release was ever published — 1.0 is the first
  published `schema: 1` — but it must exist before `schema: 2`.
- **ID collisions across concurrent branches are possible and there is no repair tool.**
  Two branches can allocate the same number; the clash appears at merge. The index reports
  duplicates (`TestBuildRecordsDuplicateIDs`); renumbering is manual.
- `gintrack board`, `gintrack sprint` and `gintrack retro` are specified in `docs/07` §4.6
  but not implemented. Boards, sprints and retros are managed in the UI or by editing files.

### Server API

The following endpoints answer `501 Not Implemented` by design — configuration is CLI-only
and editing is scoped to what the UI needs:

- `POST /api/v1/workspaces`, `POST /api/v1/repos`, `DELETE /api/v1/repos/{id}`
- `PATCH /api/v1/projects/{key}` (editing `project.yaml` over the API)
- `GET /api/v1/kb/asset`
- `PUT /api/v1/items/{id}` (full replace; use `PATCH`)
- `/api/v1/items/{id}/links` (edit typed links through `PATCH`)
- `GET /api/v1/git/log` answers `not_implemented`

`POST /api/v1/sync/run` is **synchronous**: it runs the whole sync and answers `200` with
the finished result rather than `202 Accepted` plus a polling endpoint
(`internal/server/sync.go:handleSyncRun`). A long sync therefore holds the request open,
and there is no way to poll or cancel it.

Observability documented in `docs/07` §8 is **not** implemented: no `GET /api/v1/metrics`
in Prometheus format, no `/debug/pprof`, no `gintrack doctor --bundle`, no rotating file
log.

### MCP server

- Twelve tools ship. `list_workspaces`, `list_projects`, `get_kb_tree`, `link_items`,
  `list_comments`, `list_boards`, `get_board`, `get_sprint`, `list_retros`,
  `get_sync_status` and `run_sync` are **planned, not built**.
- **There are no retrospective tools and no metrics tools over MCP.** Agents read those
  files directly.
- Resources and prompts are not advertised; the `--tools` allowlist, dry-run, the local
  audit log, the `Agent*` commit trailers and rate limiting are all planned.
- **No golden snapshot pins the tool schemas**, so a breaking schema change would not fail
  a test. The MINOR-bump policy is written down but unenforced.
- A long-lived MCP session does not watch for external edits and cannot attach to an
  already-running companion.
- `delete_item` is deliberately absent: an agent may move an item to `cancelled`, never
  delete it.

### Metrics

- **A rebase or a squash rewrites history, and therefore rewrites the charts.** Metrics are
  reconstructed from the git history of the item files (ADR-017); nothing is stored. If you
  squash a branch, every burndown point derived from those commits moves with it.
- The history walk is **bounded at 2,000 commits per path**. Past that the result is
  flagged `truncated` and is approximate.
- Cards resolved from an index snapshot (a project you have not cloned) are `unknown` on
  every day.
- **Board-level cumulative flow and cross-sprint velocity are not built.** Metrics are
  per-sprint.

### Distribution and build

- **`brew install` is macOS-only.** GoReleaser deprecated `brews:`, so the tap ships a
  cask, and Homebrew on Linux cannot install a cask (ADR-016). Linux users take the
  tarball, `go install` or the image.
- **`go install` produces a binary with no embedded web UI.** `gintrack mcp` and every file
  command work; `gintrack serve` says there is no UI. Clone and `make build` for the full
  product.
- **Docker file watching needs inotify to cross the bind mount.** That works on Linux; on
  Docker Desktop for macOS and Windows it does not — pass `--watch=false` there.
- **The container binds `0.0.0.0`** because a loopback bind inside a container is
  unreachable. What keeps it private is the port mapping: `-p 127.0.0.1:7317:7317`. Writing
  `-p 7317:7317` publishes your repository on every host interface, behind nothing but the
  bearer token.
- Binaries are **unsigned and not notarized** (ADR-011). macOS Gatekeeper and Windows
  SmartScreen will warn on first run; `docs/09` §4 documents the bypass for each. **Those
  bypass instructions have not been re-verified on macOS or Windows for this release.**
- `.deb`, `.rpm`, AUR, nixpkgs, winget, Snap and Flatpak are deliberately out of scope.
- GoReleaser's `dockers:`/`docker_manifests:` blocks are deprecated in favour of
  `dockers_v2:`. They still validate and still build; migration is mechanical and pending.
- `.golangci.yaml` disables `contextcheck` for `internal/gitops/.*_test\.go` — test fixture
  helpers bottom out in context-free constructors. Production `internal/gitops` code is
  fully linted.

### Process

- **No Playwright end-to-end suite exists**, so the "one shared E2E suite run twice"
  parity guarantee is not mechanised.
- **An agent completing a story end to end from `AGENTS.md` alone has never been
  demonstrated and recorded.**
- **No WCAG 2.1 AA audit has been performed.** `eslint-plugin-jsx-a11y` is a linter.
- The licence is MIT as a placeholder and must be confirmed by the repository owner
  (`docs/01` §12, open question 1).

---

## 6. What a maintainer still has to do

None of the following can be done by an agent, and none of it was done here.

**Create the publishing targets** (`docs/09` §10):

1. `digiogithub/homebrew-tap` — public, default branch `main`.
2. `digiogithub/scoop-bucket` — public, default branch `main`.
3. Actions secret `HOMEBREW_TAP_TOKEN` on `git-in-track`: a fine-grained PAT scoped to
   `digiogithub/homebrew-tap` with `Contents: read and write`.
4. Actions secret `SCOOP_BUCKET_TOKEN`: the same, scoped to `digiogithub/scoop-bucket`.

GHCR needs no secret — `secrets.GITHUB_TOKEN` plus the job's `packages: write` is enough.
The release workflow checks both PATs before it builds anything and fails with the fix in
the message when either is missing.

**Merge and tag:**

```bash
git switch main && git pull --ff-only
# merge feat/phase-6-retros-metrics through a reviewed PR, then:
git tag -a v1.0.0 -m "v1.0.0"
git push origin v1.0.0
```

**Verify after the workflow finishes:**

- Six archives plus `checksums.txt` on the GitHub Release, and the generated changelog.
- `Casks/gintrack.rb` landed in the tap; `bucket/gintrack.json` landed in the bucket.
- `ghcr.io/digiogithub/git-in-track:1.0.0` exists as a manifest list over `-amd64` and
  `-arm64`, and `:latest` moved.
- Download one archive per OS family and verify it against `checksums.txt`.
- `brew install digiogithub/tap/gintrack` on macOS, `scoop install gintrack` on Windows,
  and the documented `docker run` each give a working `gintrack version`.
- Walk the Gatekeeper bypass on macOS and the SmartScreen bypass on Windows and confirm
  `docs/09` §4 is still accurate.

**Decide, separately from the tag:**

- Whether the milestones and stories still `in_review` become `done`.
- The licence (MIT placeholder).
- Whether `internal/core` coverage below 85 % blocks 1.0 or becomes a follow-up.
