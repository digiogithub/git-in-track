# git-in-track

> Git-native, Markdown-first project management for teams — and for their AI agents.

[![CI](https://github.com/digiogithub/git-in-track/actions/workflows/ci.yml/badge.svg)](https://github.com/digiogithub/git-in-track/actions/workflows/ci.yml)
[![Release](https://github.com/digiogithub/git-in-track/actions/workflows/release.yml/badge.svg)](https://github.com/digiogithub/git-in-track/releases)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

## What is git-in-track

git-in-track is a project management tool with no central server and no database.
Every artifact — epics, user stories, tasks, milestones, comments, boards,
sprints, retrospectives and knowledge base pages — is a plain Markdown file with
YAML front matter stored inside a git repository. If you can clone the repo, you
have the whole backlog, its full history, and the ability to work offline.

Humans work through a web UI that feels like an Obsidian or Logseq vault crossed
with a Kanban board: browse the knowledge base, edit stories in a rich Markdown
editor, drag cards across columns. AI agents work on exactly the same files,
either directly on disk or through a local MCP server, so an agent that picks up
a story and a developer that reviews it are looking at the same bytes.

Sync is git and nothing else. Any host works — GitHub, GitLab, Gitea, Bitbucket
or a bare repository on your own machine — because git-in-track only ever speaks
standard git. There is no SaaS to sign up for, no export format to worry about,
and no vendor that can take your backlog away.

## Key features

- **Plain files, no lock-in.** Markdown + YAML front matter, readable and
  editable with any text editor.
- **Git is the only sync mechanism.** No server, no database, no accounts.
  Multi-user collaboration is just multiple people pushing to the same repos.
- **Browser-only mode.** Open local folders straight from a Chromium-based
  browser; parsing and indexing run in WebAssembly compiled from the shared Go
  core.
- **Companion CLI.** `gintrack serve` runs a local server that embeds the same
  web app, watches the file system for instant updates, and indexes natively.
- **Team boards across projects.** Kanban and Scrum boards live in a team
  repository and *reference* items in project repositories — never copies.
- **Knowledge base included.** Any docs folder becomes a rendered KB with GFM
  tables, task lists, footnotes, callouts, wikilinks and Mermaid diagrams.
- **Agent-native.** An MCP server exposes the backlog as compact, paginated
  tools designed for LLM agents.
- **One core, two runtimes.** The same Go code powers the CLI and the browser
  WASM module, so behavior never drifts between them.

## How it works

There are two kinds of repositories.

A **project repository** is any git repository you already have. You mark one
folder as its documentation folder (for example `docs/`); that folder becomes the
project knowledge base. Inside it, a `.pmngr/` directory holds the project
backlog: `project.yaml`, `epics/`, `stories/`, `tasks/`, `milestones/` and
`comments/`. A project's full backlog lives only in the project repository, next
to the code it describes.

A **team repository** — one per team — holds `team.yaml` (members plus the list
of project repositories), a team-wide `knowledge/` folder, and a `.pmngr/`
directory with `boards/`, `sprints/` and `retros/`. Boards aggregate work from
every project by reference (`ref: <projectKey>/<itemId>`). If a project repo is
not cloned locally, its cards are rendered as read-only remote references from
the index snapshot committed under `.pmngr/index/<projectKey>.json`.

You can use git-in-track in **browser-only mode** (pick folders with the File
System Access API; the shared Go core runs as WebAssembly in a Web Worker; git
operations via isomorphic-git) or in **companion mode**, where the `gintrack`
CLI serves the same web app locally, watches files with fsnotify and performs git
operations natively. The web app detects the companion and upgrades itself
automatically. Agents connect through `gintrack mcp` over stdio, or over
streamable HTTP on the local server.

```mermaid
flowchart LR
    subgraph Local["Your machine"]
        WEB["Web app<br/>React 18 + Vite"]
        CLI["gintrack CLI<br/>serve / mcp"]
        WASM["Go core<br/>as WASM"]
        FS[("Working trees<br/>on disk")]
    end
    subgraph Repos["Git remotes (any host)"]
        TEAM["Team repo<br/>team.yaml, boards,<br/>sprints, retros"]
        P1["Project repo A<br/>docs/.pmngr/"]
        P2["Project repo B<br/>docs/.pmngr/"]
    end
    AGENT["AI agent"]

    WEB -->|File System Access| FS
    WEB --> WASM
    WEB <-->|REST + WebSocket| CLI
    CLI --> FS
    AGENT -->|MCP| CLI
    FS <-->|git fetch / push| TEAM
    FS <-->|git fetch / push| P1
    FS <-->|git fetch / push| P2
    TEAM -.->|ref: KEY/ITEM-ID| P1
    TEAM -.->|ref: KEY/ITEM-ID| P2
```

## What an item looks like

`docs/.pmngr/stories/ACME-US-0042-login-with-sso.md`:

```markdown
---
id: ACME-US-0042
type: story
title: Login with SSO
status: in_progress
created: 2026-02-10
updated: 2026-02-18
author: jane.doe
assignees: [john.roe]
labels: [auth, security]
priority: high
parent: ACME-EP-0003
milestone: ACME-M-0001
estimate: 5
due: 2026-03-01
links:
  - kind: blocked_by
    target: ACME-US-0031
---

## Description

As a user, I want to sign in with my corporate identity provider so that I do
not need a separate password for this application.

## Acceptance Criteria

- [x] SAML metadata is configurable per tenant
- [ ] Failed assertions show an actionable error
- [ ] Session is created with the same claims as password login

## Notes

See [[Identity Provider Setup]] in the knowledge base.
```

Tasks live in `.pmngr/tasks/` with `parent` pointing at a story; comments live in
`.pmngr/comments/<ITEM-ID>/<timestamp>-<author>.md`. See
[docs/03-data-model.md](docs/03-data-model.md) for the full schema.

## Installation

> **1.0 is prepared but not yet tagged.** The release pipeline is complete and verified
> end to end against a snapshot build, but no `v*` tag has been pushed, so these channels
> hold nothing yet. Until a maintainer tags `v1.0.0`, build from source with `make build`.
> What is left to do, and by whom, is in
> [docs/12-release-readiness-1-0.md](docs/12-release-readiness-1-0.md) §6.

Every channel is published from the same tag by the release workflow
([docs/09-ci-cd-and-releases.md](docs/09-ci-cd-and-releases.md) §10). Releases are
unsigned archives with `checksums.txt`, by design ([ADR-011](docs/adr/ADR-011-goreleaser-unsigned-artifacts.md)).

```bash
# macOS — Homebrew, the recommended route: it clears the quarantine attribute
brew install digiogithub/tap/gintrack

# Windows — Scoop
scoop bucket add digiogithub https://github.com/digiogithub/scoop-bucket
scoop install gintrack

# Linux, or any platform — the release archive
#   https://github.com/digiogithub/git-in-track/releases/latest
tar -xzf gintrack_*_linux_amd64.tar.gz && sudo install gintrack /usr/local/bin/

# Docker — serve a working tree without installing anything
docker run --rm -p 127.0.0.1:7317:7317 -v "$PWD:/work" \
  --user "$(id -u):$(id -g)" ghcr.io/digiogithub/git-in-track:latest

# Developers — no embedded web UI unless you build the frontend first
go install github.com/digiogithub/git-in-track/cmd/gintrack@latest
```

The container prints its bearer token on start; open the URL it logs. It binds
`0.0.0.0` inside the container because a published port cannot reach a loopback
bind — the `-p 127.0.0.1:…` mapping is what keeps it off your network. `brew` is
macOS-only here: the tap ships a cask, not a formula
([ADR-016](docs/adr/ADR-016-homebrew-cask-instead-of-formula.md)). `go install`
builds from source without `web/dist`, so `gintrack serve` reports no embedded UI;
`gintrack mcp` and the file commands work normally.

Verify a downloaded archive before running it:

```bash
sha256sum -c checksums.txt --ignore-missing        # Linux
shasum -a 256 -c checksums.txt --ignore-missing    # macOS
```

## Quick start

> **All seven phases are implemented.** The shared Go core, the `gintrack` binary
> with the embedded web app, browser-only mode, the companion CLI (`gintrack
> serve` with the REST API, live file watching and the event stream), team boards
> and sprints, git sync (`gintrack sync`, commit on save, conflict resolution),
> the MCP server (`gintrack mcp`), retrospectives, sprint metrics and the
> distribution channels all ship. The 1.0 release itself is **prepared but not
> tagged**: read [CHANGELOG.md](CHANGELOG.md) for what the release does and does
> not do, and build from source with `make build` in the meantime.

```bash
# 1. Install gintrack (see Installation above) and put it on PATH

# 2. Register a project repository you already have cloned
gintrack add ./my-repo

# 3. Start the local companion and open the web app
gintrack serve   # http://127.0.0.1:7317
```

Browser-only alternative: open the hosted web app in a Chromium-based browser,
click *Open folder* and pick your repository. Indexing runs in WebAssembly and the
index is cached in IndexedDB; no binary and no installation are required. Other
browsers get a read-only fallback.

## Project status

**Feature-complete for 1.0; the release is prepared and awaiting its tag.** All
seven phases (Phase 0 Foundations → Phase 6 Retrospectives, metrics and 1.0) are
implemented; see [docs/11-roadmap.md](docs/11-roadmap.md) for the plan and
[docs/12-release-readiness-1-0.md](docs/12-release-readiness-1-0.md) for the
evidence behind each claim below, including the criteria that are **not** met.
The live status of every epic and story is in [docs/.pmngr/](docs/.pmngr/), this
repository's own backlog.

| Phase | Milestone | Status |
|-------|-----------|--------|
| 0 | Foundations (core model, parser, validation, IDs, CI, WASM build) | done |
| 1 | Browser-only MVP (folder access, WASM index, KB viewer, backlog, editor) | done, one story in review |
| 2 | Companion CLI (`gintrack serve`, watcher, native index, REST/WS) | done |
| 3 | Team repository and boards (kanban, scrum, sprints, remote references) | done, in review |
| 4 | Git sync (commit on save, fetch/rebase/push, conflicts, credentials) | done, in review |
| 5 | MCP server and agent workflows (13 tools, stdio + HTTP, `rev` locking) | done, in review |
| 6 | Retrospectives, sprint metrics, distribution channels, 1.0 release prep | done, in review — the `v1.0.0` tag is the maintainer's remaining step |

Known limitations are not hidden: the full list, with a file or a test behind
every line, is [CHANGELOG.md](CHANGELOG.md) "Known limitations" and
[docs/12-release-readiness-1-0.md](docs/12-release-readiness-1-0.md) §5. The
short version: commit on save does not work in browser-only mode, browser git
needs a CORS proxy, metrics are rewritten by a rebase or a squash, and `brew
install` is macOS-only.

## Repository layout

```
cmd/gintrack/            # CLI entry point
internal/core/           # shared core (model, frontmatter, index, query, ids) -> also WASM
internal/server/         # HTTP/WS API, embeds web/dist
internal/watcher/        # fsnotify file watching
internal/gitops/         # go-git wrapper
internal/mcp/            # MCP server
wasm/                    # WASM entry point (main_js.go) + JS glue
web/                     # React + Vite + TypeScript app
docs/                    # planning docs, ADRs, and this project's own .pmngr backlog
.github/workflows/       # ci.yml, release.yml
Makefile, go.mod, .goreleaser.yaml, Dockerfile
```

## Documentation

| Document | What it covers |
| --- | --- |
| [docs/01-vision-and-scope.md](docs/01-vision-and-scope.md) | Problem, principles, target users, and what is explicitly out of scope. |
| [docs/02-architecture.md](docs/02-architecture.md) | Components, shared Go core, WASM and companion modes, data flow. |
| [docs/03-data-model.md](docs/03-data-model.md) | File naming, IDs, front matter fields, statuses, relations, comments. |
| [docs/04-team-repository.md](docs/04-team-repository.md) | `team.yaml`, boards, sprints, retros, cross-project references, index snapshots. |
| [docs/05-web-app.md](docs/05-web-app.md) | React app structure, routing, editor, KB rendering, browser-only mode. |
| [docs/06-git-sync.md](docs/06-git-sync.md) | Commit-on-save, fetch/rebase/push, conflicts, credentials, CORS proxy. |
| [docs/07-cli-and-api.md](docs/07-cli-and-api.md) | `gintrack` commands, REST + WebSocket API, `rev` optimistic locking. |
| [docs/08-mcp-server.md](docs/08-mcp-server.md) | MCP tools for agents, transports, payload shapes, pagination. |
| [docs/09-ci-cd-and-releases.md](docs/09-ci-cd-and-releases.md) | CI pipelines, GoReleaser, build matrix, release artifacts. |
| [docs/10-development-guidelines.md](docs/10-development-guidelines.md) | Coding standards, testing, commit conventions, review process. |
| [docs/11-roadmap.md](docs/11-roadmap.md) | Phases 0–6 with scope and exit criteria for each. |
| [docs/12-release-readiness-1-0.md](docs/12-release-readiness-1-0.md) | Evidence for every 1.0 criterion: what is proven, what is partial, what is not done. |
| [CHANGELOG.md](CHANGELOG.md) | Release notes, the compatibility promise, known limitations and operational notes. |
| [docs/adr/](docs/adr/) | Architecture decision records. |
| [docs/.pmngr/](docs/.pmngr/) | This project's own backlog, dogfooding the format. |
| [AGENTS.md](AGENTS.md) | Working instructions for AI coding agents on this repository. |

## Contributing

Contributions are welcome. Start with
[docs/10-development-guidelines.md](docs/10-development-guidelines.md) for
coding standards, commit conventions (Conventional Commits) and the definition
of done, then pick an item from [docs/.pmngr/](docs/.pmngr/). All code, comments,
documentation and commit messages are written in English.

## License

MIT (placeholder — to be confirmed by the repository owner). See [LICENSE](LICENSE).
