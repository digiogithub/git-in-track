# Changelog

All notable changes to **git-in-track** are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/) as specified in
[docs/09-ci-cd-and-releases.md](docs/09-ci-cd-and-releases.md) §7.

Each release also carries a generated changelog on its GitHub Release page, grouped from
the Conventional Commit subjects that landed on `main`. This file is the hand-written
companion: migrations, deprecations, compatibility statements and known issues live here,
because a commit list cannot express them.

## [Unreleased]

### Added

- The companion serves the **git CORS proxy** browser-only mode needs at
  `http://127.0.0.1:7317/cors-proxy/`, and the web app adopts it automatically the moment
  it detects a companion, so the "browser UI plus local companion for networking" setup of
  docs/06 §6.3 needs no configuration (`GIT-US-0042`, ADR-025). It is deliberately narrow:
  the three git smart-HTTP endpoints only, an allow-list of hosts derived from the
  registered repositories' remotes plus `git.corsProxy.allowedHosts`, HTTPS only, public
  unicast addresses only — resolved once and dialed at the address that was checked, so DNS
  rebinding reaches nothing — 32 MiB request and 256 MiB response caps, a 120 s deadline,
  redirects re-validated at every hop, and headers copied through allow-lists in both
  directions. It requires this run's bearer token in `X-Gintrack-Token` and a trusted
  `Origin`; `Authorization` is reserved for the git host's credential and is the one header
  forwarded upstream. `GET /api/v1/git/cors-proxy` reports the mount point and the
  allow-list, and `git.corsProxy.enabled: false` removes the endpoint.
- The nginx and Caddy reverse-proxy recipe docs/06 §6.3 had claimed to ship now exists, in
  §6.3.2, with the same allow-list discipline.

- A workspace can hold several team repositories, and the web app chooses which one is
  active. The choice is remembered per workspace and sent on every call that reads a
  board, a sprint, a retro or a team knowledge base, so the companion and browser-only
  mode behave identically (`GIT-US-0036`, ADR-019, docs/04 §3.8).
- `GET /api/v1/teams` lists every mounted team, `GET /api/v1/teams/{key}` resolves the key
  it is given, and every team-scoped route accepts `?team=`.
- A team's project list is editable from the product. Settings holds a **Team projects**
  card that connects a registered repository to the active team and disconnects it again,
  offering the locally indexed projects as candidates and pre-filling key, name, docs
  folder, remote URL and branch (`GIT-US-0037`, docs/04 §3.9). The write path is
  `core.AddTeamProject`/`core.RemoveTeamProject`, the vault methods `team.project.add` and
  `team.project.remove`, and `POST`/`DELETE /api/v1/teams/{key}/projects`. A duplicate
  project key is refused with `team_project_exists`; a removal that would orphan a board,
  sprint or retro reference is refused with `team_project_referenced` unless it is forced.

- Repositories managed with **Jujutsu** are recognized as such, in both layouts —
  colocated (`.jj/` beside `.git/`) and with the git store inside `.jj/` — and reported as
  their own kind by `gintrack ls`, `gintrack add`, `gintrack doctor`, the repository and
  sync payloads and the web app (`GIT-US-0038`, ADR-021, docs/06 §14). A jj workspace with
  no colocated git working tree is registered and indexed instead of being refused.
- A **`jj` backend** reads a Jujutsu repository through `jj` itself, alongside `system` and
  `go-git` and selected for a jj working tree in either layout (`GIT-US-0040`, docs/06
  §14.4-14.5). It reports the real bookmark as the line of work with the bookmark as its
  push target, jj's own dirty set instead of git's index, the tracked remote bookmark with
  ahead/behind counted over a revset against it, `undo: operation_log` with nothing to
  resume, and the three sides of a conflict materialized out of the commit that records
  them with `markers: "jj"`. A **non-colocated repository is now readable at all** — status,
  conflicts and, for the first time, sprint metrics. Every read carries
  `--ignore-working-copy`, so looking at a repository never snapshots the user's working
  copy or adds an entry to the operation log.
- **Jujutsu repositories are writable.** Commit on save, explicit commits, fetch,
  integrate, push, undo and conflict resolution all go through `jj`, so a jj repository is
  a first-class repository rather than a read-only special case (`GIT-US-0041`, ADR-024,
  docs/06 §14.6-14.7). A commit is `jj commit -m <message> -- <paths>`, which records
  exactly those paths and leaves every other edit in the new working-copy commit, followed
  by a fast-forward `jj bookmark move` — without it the work would be reachable from `@`
  alone and `jj git push` would never publish it. A sync runs `jj git fetch`,
  `jj rebase -b @ -d <upstream>` (or a `jj new` merge commit) and
  `jj git push -b <bookmark>` with its dry run; `jj undo` takes an operation back over the
  operation log; a conflict is resolved by writing the merged file and squashing it into
  the commit that records it. Each row of `GET /api/v1/sync/status` now carries `writes`,
  which is what the sync panel disables its buttons from.

### Changed

- A second team repository is no longer reported as an error and ignored. What is reported
  now is two mounted repositories declaring the same team `key:`.
- The version-control backend interface (`internal/gitops.Backend`) is expressed only in
  concepts git and Jujutsu both have, so the coming jj backend can implement it without
  faking an index, a `MERGE_HEAD` or a branch (`GIT-US-0039`, ADR-022, docs/06 §14.6): a
  commit covers *exactly* a set of paths, an unfinished *integration* reports how it is
  undone (`abort` or `operation_log`) and resumed (`continue` or nothing), a conflict is
  three sides the backend produces however it can plus the marker dialect of the working
  file, and a *line of work* (a branch or a bookmark) replaces the branch-plus-detached
  pair. `Backend.Abort`/`Continue` are now `Undo`/`Resume`. **No API and no behavior
  changed:** every JSON field and every `git_*`/`vcs_*` code is where it was, and the new
  fields (`lineKind`, `pushTarget`, `unfinished`, `undo`, `resume`, `markers`) are
  additive.

### Fixed

- **git no longer writes behind Jujutsu.** In a jj repository git's `HEAD` sits at the
  parent of the working-copy commit, so a `git commit` there landed on `@-`, moved no
  bookmark and was abandoned as an orphan by the next `jj` command — unreachable from any
  bookmark and unpublishable by `jj git push`. Every such write was first refused with
  `vcs_jujutsu_write_refused` (HTTP 409) and a message naming the `jj` command to run
  instead (`GIT-US-0038`), and now goes through `jj` itself (`GIT-US-0041`). The refusal
  remains for the one case that has no safe answer: a jj repository with **no jj binary
  installed**.
- A jj repository is no longer shown with a destructive "Detached HEAD" badge or as a
  permanently dirty tree. Its branch is reported as `@` — as its bookmark, once the `jj`
  backend of `GIT-US-0040` is driving it — and the sync panel says "Managed by Jujutsu —
  reads and writes go through jj" instead of advising a branch checkout that is
  impossible there.
- A jj repository's sync state is the truthful one (`up_to_date`, `ahead`, `behind`,
  `diverged`, `dirty`, `conflicted`) instead of the `jujutsu` placeholder, which now means
  "no jj binary is installed, so the read-only git guard is driving this repository"
  (`GIT-US-0040`). The `jujutsu` flag on the status is unchanged, and is what the UI reads.
- A jj older than 0.41 is refused with `vcs_jujutsu_too_old` when a repository is opened,
  instead of being only a `gintrack doctor` warning: the backend does not parse output it
  has not been verified against (`GIT-US-0040`).
- Ahead and behind no longer fail in a jj repository whose bookmark jj marks as conflicted
  after a fetch — the state the sync preflight meets when both sides moved. The counters
  are computed against the bookmark's local position, so the repository reads as
  `diverged` (`GIT-US-0041`).
- The two sides of a jj conflict are no longer swapped. jj materializes the incoming work
  as the `+++++++` snapshot and the user's own commit as the `%%%%%%%` diff — the opposite
  of git's index during a rebase — so "keep mine" kept the wrong side in a jj repository
  (`GIT-US-0041`).
- A dirty working copy no longer blocks a sync in a jj repository. There the working copy
  *is* a commit, so a rebase carries it along instead of overwriting a checkout
  (`GIT-US-0041`).

## [1.0.0] — unreleased, prepared

> **This entry is prepared, not published.** No `v1.0.0` tag has been pushed; the
> repository carries no tags at all. The maintainer cuts the tag, and the release
> workflow does everything else. The remaining steps are listed in
> [docs/12-release-readiness-1-0.md](docs/12-release-readiness-1-0.md) §6.

First stable release. git-in-track is a project management tool with **no server and no
database**: epics, stories, tasks, milestones, comments, boards, sprints, retrospectives
and knowledge-base pages are Markdown files with YAML front matter, stored in your git
repositories. If you can clone the repo, you have the backlog, its whole history, and the
ability to work offline. Sync is `git fetch` / `git rebase` / `git push`, and nothing else.

### What 1.0 does

**Your backlog is files.** Epics, stories, tasks, milestones and comments live under
`.pmngr/` inside your documentation folder. Every field is documented in
[docs/03-data-model.md](docs/03-data-model.md); unknown keys survive a round trip, so a
tool someone else writes cannot lose your data. `rev`, the optimistic-locking hash, is
computed when a file is read and is never stored.

**Two ways to run it.**

- *Browser-only.* Open a local folder from a Chromium-based browser through the File
  System Access API. The same Go core runs as WebAssembly in a Web Worker, and the index is
  cached in IndexedDB. No installation.
- *Companion CLI.* `gintrack serve` binds `127.0.0.1:7317` and serves the same web app from
  the binary, with fsnotify file watching, a native indexer, a REST + WebSocket API and
  native git. The web app detects the companion and upgrades itself without a reload.

**A knowledge base.** Any documentation folder renders as a first-class KB: GFM tables,
task lists, footnotes, callouts, wikilinks `[[Page]]`, Mermaid diagrams and optional math,
with an outline and a backlinks panel built from the link graph.

**Teams.** A team repository (`team.yaml`) aggregates several project repositories onto
shared Kanban and Scrum boards. Boards hold *references* — `ref: <projectKey>/<itemId>` —
never copies. A project you have not cloned still renders, read-only, from the index
snapshot committed at `.pmngr/index/<projectKey>.json`.

**Sprints, retrospectives and metrics.** Plan a sprint, run it on the board, close it, and
record the retrospective — went well, to improve, actions — with improvement actions
promotable into real tasks in a project repository. Burndown, cumulative flow, cycle time,
lead time and throughput are **reconstructed from the git history of the item files**
(see [ADR-017](docs/adr/ADR-017-metrics-history-from-git-not-a-stored-time-series.md)).
Nothing is stored redundantly, and where the history cannot answer, the chart says
`unknown` instead of guessing.

**Git from inside the product.** `gintrack sync` fetches, integrates and pushes with a
dry-run preview and a clear status. Commit on save is off by default and takes a message
template. Conflicts get a three-way text UI for Markdown bodies and a field-level helper
for YAML front matter. Credentials go through your credential helper and SSH agent
natively; in the browser a token stays in memory for the session and is never written
anywhere.

**Agents are first-class.** `gintrack mcp` speaks MCP on stdio, and `gintrack serve
--mcp-http` serves the same tools at `POST /mcp`. Twelve tools — `list_items`,
`search_items`, `get_item`, `create_epic`, `create_story`, `create_task`, `update_item`,
`add_comment`, `move_on_board`, `list_kb_pages`, `get_kb_page`, `search_kb` — with compact
JSON, cursor pagination and `rev`-based optimistic locking, so two agents writing the same
item produce one success and one structured conflict, never a lost update.
[AGENTS.md](AGENTS.md) is the convention an agent reads before it picks up work.

### Compatibility promise

- **The on-disk layout is frozen at `schema: 1`.** The field is `schema` in `project.yaml`
  and `team.yaml`; `internal/core.SupportedSchema` is the constant. Items do not carry
  their own version.
- Within the `1.x` line, git-in-track will **not** rename or remove a front-matter field,
  change a file path, or change an ID format. Adding a new *optional* field with a default
  is a MINOR release and does not bump `schema`.
- The same promise covers the REST/WebSocket API, the MCP tool schemas, and the CLI
  commands and flags. A breaking change to any of them is a MAJOR release.
- A client that meets a **higher** `schema` opens the project read-only and says why,
  rather than corrupting it.
- Unknown keys are preserved on rewrite. Add your own — prefix them `x-` — and the tool
  will not eat them.

**Upgrading.** There is nothing to upgrade from: no 0.x release was ever published, and
1.0 is the first `schema: 1` in the field. `gintrack migrate` is specified
([docs/03-data-model.md](docs/03-data-model.md) §19, R-EVO-4) but **not implemented**; it
must exist before any `schema: 2` ships.

### Known limitations

The complete, evidence-backed list is
[docs/12-release-readiness-1-0.md](docs/12-release-readiness-1-0.md) §5. The ones most
likely to affect you:

**Browser-only mode is not the full product.**

- **Commit on save does not work in the browser.** The setting renders; nothing is
  committed. Use the companion CLI.
- **Firefox and Safari are read-only** — no File System Access API means no writes and no
  git. Files above 5 MB are indexed by metadata only in that fallback.
- **Browser git requires a CORS proxy**, because git hosts send no permissive CORS headers.
  Until one is configured, git in the tab is disabled. No SSH remotes. No rebase — the
  integration strategy is forced to `merge`. No signing, hooks, submodules or LFS. Clones
  are shallow (`depth: 50`, single branch).
- Browser metrics can only see back as far as each item's `updated` timestamp and report
  everything before it as `unknown`.

**Git sync.**

- `git.dirtyPolicy` (`stash`, `ask`) is documented but **not implemented** — the key is not
  read.
- **Branch policy is not implemented**: no `user-branch` mode, no `autoPr`, no host URL
  templates. Every repository syncs its checked-out branch against its own upstream.
- **Per-repository `git:` overrides are not implemented**; settings are per workspace.
- The pure-Go backend fast-forwards only, cannot sign, cannot abort or continue a rebase,
  and **cannot apply a conflict resolution**. Install system git and select that backend if
  you need any of that. Reading conflicts works on both.

**Metrics — read this before you trust a chart.**

- **A rebase or a squash rewrites history, and therefore rewrites the charts.** Metrics are
  derived from the commits that touched each item file. Squash a branch and every point
  derived from those commits moves with it. This is the price of storing no time series,
  and it is the deliberate trade of ADR-017.
- The history walk is **bounded at 2,000 commits per path**; beyond that the result is
  flagged `truncated` and is approximate.
- Cards from a project you have not cloned are `unknown` on every day.
- Metrics are **per sprint**: board-level cumulative flow and cross-sprint velocity are not
  built.

**MCP.**

- Twelve tools ship. `list_workspaces`, `list_projects`, `get_kb_tree`, `link_items`,
  `list_comments`, `list_boards`, `get_board`, `get_sprint`, `list_retros`,
  `get_sync_status` and `run_sync` are **planned, not built**.
- **There are no retrospective or metrics tools over MCP.** Agents read those files
  directly.
- Resources, prompts, dry-run, the `--tools` allowlist, the local audit log and rate
  limiting are all planned. `delete_item` is deliberately absent: an agent may move an item
  to `cancelled`, never delete it.
- **No golden snapshot pins the tool schemas.** The MINOR-bump policy is written down but
  not enforced by a test.

**API and CLI.**

- `POST /api/v1/sync/run` is synchronous: it answers `200` with the finished result, so a
  long sync holds the request open and cannot be polled or cancelled.
- These answer `501 Not Implemented` by design (configuration is CLI-only):
  `POST /api/v1/workspaces`, `POST /api/v1/repos`, `DELETE /api/v1/repos/{id}`,
  `PATCH /api/v1/projects/{key}`, `GET /api/v1/kb/asset`, `PUT /api/v1/items/{id}` (use
  `PATCH`), and `/api/v1/items/{id}/links`. `GET /api/v1/git/log` answers
  `not_implemented`.
- Observability documented in [docs/07](docs/07-cli-and-api.md) §8 is not implemented: no
  Prometheus endpoint, no `/debug/pprof`, no `gintrack doctor --bundle`, no rotating file
  log.
- `gintrack board`, `gintrack sprint` and `gintrack retro` are specified but not
  implemented; use the UI or edit the files.
- **ID collisions across concurrent branches are possible and there is no repair tool.**
  Two branches can allocate the same number and the clash surfaces at merge. The index
  reports duplicates; renumbering is manual.

**Process.** There is no Playwright end-to-end suite, no recorded WCAG 2.1 AA audit, and
no recorded demonstration of an agent completing a story end to end from `AGENTS.md`
alone.

### Operational notes

**Docker.** The image serves the working tree you mount:

```bash
docker run --rm -p 127.0.0.1:7317:7317 -v "$PWD:/work" \
  --user "$(id -u):$(id -g)" ghcr.io/digiogithub/git-in-track:1.0.0
```

- The mount **is** the point — there is no database, and the container keeps no state.
- The process binds `0.0.0.0` inside the container, because a loopback bind in a container
  is reachable by nothing. **What keeps it private is `-p 127.0.0.1:7317:7317`.** Writing
  `-p 7317:7317` publishes your repository on every host interface, behind nothing but the
  bearer token — which is printed on start, or supplied with `-e GINTRACK_TOKEN=…`.
- `--user "$(id -u):$(id -g)"` lets the container write to a tree you own.
- **File watching needs inotify to cross the bind mount.** That works on Linux; on Docker
  Desktop for macOS and Windows it does not, and the UI updates only on reload — pass
  `--watch=false` there.

**Unsigned artifacts.** Releases are unsigned and not notarized, by design
([ADR-011](docs/adr/ADR-011-goreleaser-unsigned-artifacts.md)). macOS Gatekeeper and
Windows SmartScreen will warn on first run; the bypass for each is in
[docs/09](docs/09-ci-cd-and-releases.md) §4. Verify every download against
`checksums.txt`. Homebrew is the recommended macOS route because the cask clears the
quarantine attribute for you.

**Installing.** `brew install digiogithub/tap/gintrack` is **macOS-only** — the tap ships a
cask, not a formula ([ADR-016](docs/adr/ADR-016-homebrew-cask-instead-of-formula.md)), and
Homebrew on Linux cannot install a cask. Linux users take the tarball, the image, or
`go install`. `go install` builds **without** the embedded web UI: `gintrack mcp` and every
file command work, `gintrack serve` reports there is no UI. Clone and `make build` for the
full product.

**For the maintainer cutting this release.** Before the tag exists, create
`digiogithub/homebrew-tap` and `digiogithub/scoop-bucket`, and set the Actions secrets
`HOMEBREW_TAP_TOKEN` and `SCOOP_BUCKET_TOKEN` — fine-grained PATs scoped to one repository
each, `Contents: read and write`. GHCR needs no secret. The release workflow verifies both
tokens before it builds anything and fails with the fix in the message when either is
missing. Full procedure: [docs/09](docs/09-ci-cd-and-releases.md) §9 and §10.

[Unreleased]: https://github.com/digiogithub/git-in-track/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/digiogithub/git-in-track/releases/tag/v1.0.0
