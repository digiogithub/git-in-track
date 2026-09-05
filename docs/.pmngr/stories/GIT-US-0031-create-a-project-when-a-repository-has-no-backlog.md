---
id: GIT-US-0031
type: story
title: Create a project when a repository has no backlog
status: in_review
priority: high
parent: GIT-EP-0008
milestone: GIT-M-0008
author: team
labels: [core, cli, web]
estimate: 8
created: 2026-09-05T00:00:00Z
updated: 2026-09-05T00:00:00Z
---

## Description

As someone opening git-in-track on a repository that has never used it, I want the tool to
ask me where the backlog should live and create it, so that I can start working instead of
hand-writing `project.yaml` and a folder tree.

Two defects of the shipped product block that first minute, and both are about where a
project is:

1. **Discovery is unbounded.** `core.DiscoverProjects` walks the entire working tree looking
   for `.pmngr/project.yaml`, so registering the git-in-track repository itself surfaces the
   `DEMO` and `ACME` fixtures under `testdata/` as if they were real projects of the user.
2. **There is no way to create a project.** `gintrack add` on a repository with no `.pmngr/`
   prints a warning and mounts an empty workspace; the web wizard offers "mount it anyway".
   Nothing anywhere writes a `project.yaml`.

The fix for (1) must not break the monorepo layout documented in docs/03 §3.5
(`apps/api/docs/.pmngr/`, three levels deep). The rule adopted here is: **discovery probes
the repository root and each of its first-level directories, plus every documentation folder
the registration explicitly declares** — deep detection happens once, when the repository is
registered, and is recorded in the configuration (or in the browser's mount record) so that
every later scan is bounded and predictable. See ADR-018.

The fix for (2) is a project scaffolder in `internal/core` (WASM-safe, `core.FS` only)
reachable from every surface: the CLI (`gintrack init`, and `gintrack add --key`), the vault
contract (`project.create`), the REST API (`POST /api/v1/repos/{id}/projects`) and the add
repository wizard of the web app.

## Acceptance Criteria

- [x] `core.DiscoverProjects` probes the repository root and its first-level directories only,
      and no longer walks the whole tree.
- [x] A documentation folder declared by the caller is probed at any depth, so the documented
      monorepo layout `apps/api/docs/.pmngr/` keeps working once it is registered.
- [x] The registration path (`config.DocsCandidates`, the browser wizard) still detects deep
      candidates and records them, so declaring them is a one-time act, not a manual edit.
- [x] `gintrack ls` against the git-in-track repository reports the `GIT` project only; the
      `testdata/` and `internal/core/testdata/` fixtures are gone from the listing.
- [x] The browser scan applies the same bound, so both runtimes agree on what a project is.
- [x] `internal/core` gains a project scaffolder that writes `<docsFolder>/.pmngr/project.yaml`
      plus the layout of docs/03 §2, validates the key against `[A-Z][A-Z0-9]{1,9}` and
      refuses to overwrite an existing project. It compiles to WASM: `core.FS` only.
- [x] The CLI can create a project without a terminal: `gintrack init <path> --key --name
      --docs`, and `gintrack add --key` creates one while registering.
- [x] The vault exposes `project.create`, the companion serves
      `POST /api/v1/repos/{id}/projects`, and all three web providers implement
      `createProject`.
- [x] The add repository wizard, when no backlog is found, asks for the folder, the key and
      the name and creates the project; "mount it anyway" stays available as an explicit
      choice.
- [x] docs/02, docs/03, docs/05 and docs/07 describe the discovery rule and the create flow,
      and ADR-018 records the decision.
- [x] `make lint`, `make test`, `make wasm`, `make wasm-smoke` and `make build` pass.

## Notes

The discovery bound and the scaffolder ship together on purpose: the wizard cannot ask "where
should the backlog live?" without a rule saying where the answer is allowed to be, and the
bound cannot be tightened without a way to declare the folders it stops finding by accident.
