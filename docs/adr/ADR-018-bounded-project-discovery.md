# ADR-018 — Project discovery is bounded to the root and one level, plus what the registration declares

- **Status:** Accepted
- **Date:** 2026-09-05
- **Phase:** 7 (Authoring a workspace from the web UI)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-004](ADR-004-browser-only-file-system-access.md)
- **Implements:** `GIT-US-0031` — Create a project when a repository has no backlog

## Context

`project.yaml` is the discovery marker of a project backlog (doc 03, R-LOC-2).
Until now every host looked for it by walking the whole working tree, pruning
only `.git`, `node_modules`, `dist`, `vendor` and dot folders, down to 32 levels.

That is wrong in a way a user notices on their first minute: registering the
git-in-track repository itself reported three projects — the real `GIT` backlog
under `docs/`, plus `DEMO` and `ACME` from the test fixtures under `testdata/`
and `internal/core/testdata/`. Those fixtures are inputs to a test suite, not a
team's work. The same happens with a vendored sample, a documentation snapshot,
a cloned example repository, or a second checkout left inside the tree.

The unbounded walk is also unpredictable: what counts as a project depends on
whatever else happens to be on disk under the folder, and it changes when
somebody adds a fixture.

But a bound cannot be depth alone. Doc 03 §3.5 documents the monorepo layout
`apps/api/docs/.pmngr/` with `key: API` next to `apps/web/docs/.pmngr/` with
`key: WEB` — three levels down. A rule that simply stopped at one level would
silently drop a documented, supported layout.

Three options were considered:

1. **Depth cap only.** Simple, and it breaks the documented monorepo.
2. **A marker file at the repository root** listing the project folders. A new
   file format, a new thing to merge, and nothing to write it in a repository
   nobody has registered yet.
3. **A shallow default plus an explicit declaration** carried by the
   registration, which every host already has: the gintrack configuration in
   companion mode, the persisted folder handle record in browser-only mode.

## Decision

**Discovery probes the repository root and each of its first-level directories,
plus every documentation folder the caller declares. Nothing else is walked.**

- `core.DiscoverProjectsWith(fs, DiscoveryOptions{Roots, DocsFolders})` is the
  one implementation, in `internal/core/index.go`. `Roots` are folders the
  shallow rule applies to — one per repository when several are mounted side by
  side — and `DocsFolders` are declared folders, probed directly at any depth
  and never walked into. `core.DiscoverProjects(fs, root)` is the shallow rule
  alone. `core.DiscoveryDepth` is 1.
- A declared folder that does not exist is not an error, and a declared folder
  that escapes the vault (`..`, an absolute path) is ignored.
- **Detection stays deep, and it is separate from discovery.** `DocsCandidates`
  (`internal/config/repo.go`, depth 4) and `detectDocsFolders`
  (`web/src/fs/detect-project.ts`, depth 4) still find nested backlogs when a
  repository is registered, and offer them. Declaring one is a deliberate act:
  `gintrack add --docs apps/api/docs --docs apps/web/docs`, or choosing it in
  the add-repository wizard. Auto-declaring every candidate would reintroduce
  exactly the defect this ADR removes.
- The declaration travels to the core with the mount: `Repo.docsFolders` in the
  configuration file, `docsFolders` on `workspace.mount` and `vault.load`, and
  `docsFolders` on the persisted `RepoHandleRecord` in the browser.
- Creating a project declares its folder, in the vault and in the registration,
  so the folder a user just chose is never lost to the bound.

## Consequences

**Good.**

- What counts as a project is a property of the registration, not of whatever
  else is on disk. `gintrack ls` against this repository reports `GIT` and
  nothing else.
- Discovery is a handful of `stat` calls instead of a full tree walk, on every
  mount, every reload and every watcher pass that touches a `project.yaml`.
- Both runtimes agree: the browser scan feeds the same core, with the same
  declaration.

**Bad, and accepted.**

- A monorepo has to declare its projects once. The wizard and `gintrack add`
  both show the folders they found, so the declaration is a click or a flag,
  never a hand-edited configuration file.
- A repository registered before this change carries no `docsFolders`, so its
  primary `docsFolder` is the only declared one. A nested second project stops
  being indexed until it is declared. That is the intended behaviour change and
  it is the one the release notes have to call out.
- Two projects in first-level siblings (`api/.pmngr`, `web/.pmngr`) still work
  with no declaration at all, because they are within the rule.

## Alternatives rejected

- **Keep the deep walk and filter by a deny list** (`testdata`, `fixtures`,
  `examples`). It guesses at intent from folder names, and it is wrong for a
  repository whose real backlog lives in a folder that happens to be on the
  list.
- **Ask the user at index time.** A prompt cannot exist in a watcher pass or in
  a headless `gintrack ls`.
