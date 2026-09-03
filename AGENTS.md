# AGENTS.md — instructions for AI coding agents

These instructions apply to every AI coding agent (Claude Code, Codex, Cursor,
Copilot, and others) working in this repository. Follow them literally. If an
instruction here conflicts with a document under `docs/`, the document wins for
product decisions; this file wins for process.

## Project summary

git-in-track is a git-native, Markdown-first project management tool. Epics,
stories, tasks, milestones, comments, boards, sprints, retrospectives and
knowledge base pages are Markdown files with YAML front matter stored in git
repositories. There is no central server and no database. Humans use a web UI;
agents use the same files or the local MCP server. The CLI binary is `gintrack`;
the Go module path is `github.com/digiogithub/git-in-track`.

The repository is currently in the **planning phase**: it contains documentation
only. The code scaffold is Phase 0 work.

## Read the docs in this order

1. `README.md` — what the product is and how the pieces fit.
2. `docs/11-roadmap.md` — phases 0–6; know which phase your task belongs to.
3. `docs/02-architecture.md` — components and boundaries.
4. `docs/03-data-model.md` — file layout, IDs, front matter. Authoritative.
5. `docs/10-development-guidelines.md` — coding standards and process.

Then read only what your task needs: `docs/01-vision-and-scope.md`,
`docs/04-team-repository.md`, `docs/05-web-app.md`, `docs/06-git-sync.md`,
`docs/07-cli-and-api.md`, `docs/08-mcp-server.md`,
`docs/09-ci-cd-and-releases.md`, and the ADRs in `docs/adr/`.

## Tech stack

- **Backend and CLI**: Go 1.23+, cobra (CLI), chi (HTTP), fsnotify, go-git,
  goldmark, yaml.v3, an MCP Go SDK, and `go:embed` to embed the built frontend
  (`web/dist`) into the binary.
- **Frontend**: React 18 + Vite + TypeScript in `web/`, with TanStack Router and
  Query, Zustand, Tailwind CSS, shadcn/ui (Radix), CodeMirror 6, a
  unified/remark/rehype Markdown pipeline, dnd-kit for boards.
- **Shared core**: `internal/core` is compiled twice — natively for the CLI and
  to WebAssembly (`GOOS=js GOARCH=wasm`) for browser-only mode. One
  implementation, no drift.

## Repository layout — what goes where

```
cmd/gintrack/            # CLI entry point (cobra commands only, thin)
internal/core/           # model, front-matter parser, index, query, ID allocation
internal/server/         # HTTP/WS API; embeds web/dist
internal/watcher/        # fsnotify file watching
internal/gitops/         # go-git wrapper (and optional system-git shell-out)
internal/mcp/            # MCP server and tool definitions
wasm/                    # WASM entry point (main_js.go) + JS glue
web/                     # React + Vite + TypeScript app
docs/                    # planning docs, docs/adr/, docs/.pmngr/ backlog
.github/workflows/       # ci.yml, release.yml
Makefile, go.mod, .goreleaser.yaml
```

Put domain logic in `internal/core`. Do not duplicate parsing, validation or
query logic in `internal/server`, `internal/mcp`, `wasm/` or `web/`.

## Mandatory rules

- Write **everything in English**: code comments, identifiers, documentation,
  commit messages, PR titles and descriptions, test names.
- Use **Conventional Commits** with one of these scopes: `core`, `cli`,
  `server`, `web`, `wasm`, `mcp`, `docs`, `ci`.
  Example: `feat(core): parse wikilinks in story bodies`.
- **Never commit secrets**, tokens, credentials or personal data. No `.env`
  files with real values, no hard-coded hosts belonging to a customer.
- **Never change data-model paths, ID formats or front-matter field names**
  without updating `docs/03-data-model.md` in the same change and adding an ADR
  under `docs/adr/`.
- **Keep `internal/core` free of OS-specific and browser-specific code** so it
  compiles to WASM: no `os/exec`, no `syscall`, no direct filesystem walking, no
  `syscall/js`. Access files through an interface the caller supplies. Put
  native-only code in `internal/gitops`, `internal/watcher` or `internal/server`,
  and browser glue in `wasm/`.
- **The frontend must not import Node-only modules** (`fs`, `path`, `child_process`,
  and similar). It runs in a browser tab in both operating modes.
- **Update the docs when behavior changes.** A behavior change with stale docs is
  an incomplete change.
- Do not add dependencies casually. Prefer the standard library and the stack
  already listed above; justify any new dependency in the PR description.

## Build and test commands

These are the target commands defined by the Makefile. **The scaffold does not
exist yet**: an agent implementing Phase 0 must create it so that these commands
work exactly as written, matching `docs/09-ci-cd-and-releases.md` and
`docs/10-development-guidelines.md`.

```bash
make deps     # go mod download + npm ci in web/
make web      # vite build -> web/dist
make wasm     # GOOS=js GOARCH=wasm build -> web/public/core.wasm
make build    # go build with go:embed of web/dist -> gintrack binary
make test     # go test ./... + web unit tests
make lint     # go vet, golangci-lint, eslint, tsc --noEmit
```

Useful during development:

```bash
cd web && npm run dev    # Vite dev server with hot reload
go test ./...            # Go tests only
go test ./internal/core/... -run TestParse -v
```

Run `make lint` and `make test` before proposing any change that touches code.

## How to pick work

The backlog for this project lives in `docs/.pmngr/`, dogfooding the format the
product implements:

```
docs/.pmngr/project.yaml     # project key, statuses, labels, members
docs/.pmngr/epics/           # <KEY>-EP-<NNNN>-<slug>.md
docs/.pmngr/stories/         # <KEY>-US-<NNNN>-<slug>.md
docs/.pmngr/tasks/           # <KEY>-TK-<NNNN>-<slug>.md
docs/.pmngr/milestones/      # <KEY>-MS-<NNNN>-<slug>.md
docs/.pmngr/comments/<ITEM-ID>/<timestamp>-<author>.md
```

Read `project.yaml` first: it defines the project key (the ID prefix) and the
status workflow. Then pick a story whose `status` is `todo` and whose
dependencies (`links` of type `blocked_by`) are `done`.

### Front matter fields

`id` (immutable), `type` (`epic|story|task|milestone|comment|board|sprint|retro`),
`title`, `status`, `created`, `updated`, `author`, `assignees[]`, `labels[]`,
`priority` (`critical|high|medium|low`), `parent` (epic for a story, story for a
task), `milestone`, `estimate` (story points) or `effort` (hours), `due`, and
`links[]` (typed relations: `blocks`, `blocked_by`, `relates_to`, `duplicates`).
`rev` is never stored in a file — it is a content hash computed at read time.
The body is free Markdown with the conventional sections `## Description`,
`## Acceptance Criteria` (task-list checkboxes) and `## Notes`.

### Move a story's status

Edit two lines of front matter and nothing else:

```yaml
status: in_progress
updated: 2026-09-03
```

Use the statuses configured in `project.yaml` (default: `backlog`, `todo`,
`in_progress`, `in_review`, `done`, `cancelled`). Set `status: in_progress` when
you start, `in_review` when you open the PR. Do not mark `done` yourself unless
the task explicitly tells you to.

### Add a comment

Create `docs/.pmngr/comments/<ITEM-ID>/<UTC-timestamp>-<author>.md`, for example
`docs/.pmngr/comments/GIT-US-0007/20260903T142500Z-claude.md`:

```markdown
---
item: GIT-US-0007
author: claude
created: 2026-09-03T14:25:00Z
type: comment
---

Split the parser work: front matter first, then the link graph.
```

Never edit or delete someone else's comment.

### Create a task under a story

Add a file in `docs/.pmngr/tasks/` named `<ID>-<slug>.md`. The ID rule:
`<PROJECT-KEY>-TK-<NNNN>`, where `NNNN` is the next unused number for that type,
zero-padded to four digits, scanning the existing files in the folder. Set
`parent` to the story ID and `type: task`.

**IDs are permanent. Never renumber, reuse or reassign an existing ID**, even if
the item is cancelled or deleted. Gaps in the numbering are normal and expected.

## Definition of done

A change is done when all of these hold:

1. The acceptance criteria in the story are satisfied and their checkboxes are
   ticked.
2. Tests cover the new behavior and `make test` passes.
3. `make lint` passes with no new warnings.
4. `make wasm` still builds — the WASM target is part of done, not an extra.
5. The affected documents under `docs/` are updated, including an ADR if a
   decision was made.
6. Commits follow Conventional Commits with a valid scope.
7. The story's `status` and `updated` fields are set correctly.

## Testing expectations

- **Go**: table-driven tests with `t.Run` subtests, in the package under test.
  Use `testing` from the standard library; no assertion framework required.
- **Parser**: golden files. Keep inputs and expected output under
  `internal/core/testdata/`, and support a `-update` flag to regenerate them.
  Review golden diffs manually — never regenerate blindly to make a test pass.
- **Web**: Vitest plus Testing Library for components and hooks. Test behavior,
  not implementation details.
- **End to end**: Playwright, added later in the roadmap; do not block earlier
  phases on it.
- Every bug fix starts with a failing test that reproduces the bug.

## Pull request conventions

- One PR per story or task. Keep it focused and reviewable.
- Title in Conventional Commits form, same scopes as commits.
- The description states: which `docs/.pmngr/` item it implements, what changed,
  how it was tested, and which docs were updated.
- Never force-push a branch that is under review.
- Do not commit build output (`web/dist`, `web/public/core.wasm`, binaries).
- Do not create a commit or push unless the task explicitly asks for it.

## Things to avoid

- **Do not introduce a central server or a database.** Git repositories and
  files on disk are the only storage. A local companion process is allowed; a
  hosted backend is not.
- **Do not store state outside Markdown and YAML in the repositories.** Caches
  (IndexedDB, an on-disk index) are fine as derived data that can be rebuilt from
  the files at any time, and must never be the source of truth.
- **Do not break the WASM build.** Anything you add to `internal/core` must
  compile under `GOOS=js GOARCH=wasm`.
- **Do not add code signing or notarization to the release pipeline.** Releases
  are unsigned archives with checksums, by design.
- Do not invent new front-matter fields, statuses or directory names on the fly.
- Do not translate identifiers, file names or documentation into another
  language.
