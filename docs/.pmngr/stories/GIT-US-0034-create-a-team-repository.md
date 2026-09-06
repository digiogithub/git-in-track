---
id: GIT-US-0034
type: story
title: Create a team repository
status: done
priority: critical
parent: GIT-EP-0009
milestone: GIT-M-0009
author: team
labels: [core, cli, web]
estimate: 8
created: 2026-09-05
updated: 2026-09-06T00:00:00Z
closed: 2026-09-06T00:00:00Z
---

## Description

As a team lead setting git-in-track up for my squad, I want the product to create the team
repository for me, so that boards, sprints, retrospectives and the team knowledge base have a
home without me hand-writing `team.yaml` and guessing the folder layout.

Nothing in the shipped product writes a `team.yaml`. There is `core.CreateProject` and no
`core.CreateTeam`; `internal/core/team.go` and `internal/core/teamdiscover.go` read and
validate a file somebody else wrote. The vault contract has `team.get` and no team write
method, there is no REST route, no CLI verb and no UI. `gintrack init --team` only registers
the repository as a team repository — it writes nothing — and because every team surface keys
off a parsed root `team.yaml` (`internal/vault/workspace.go`, `internal/server/mount.go`,
`internal/vault/team.go`), the role recorded in the configuration is inert: a folder with no
`team.yaml` registers happily and every board, sprint and retro call then fails.

The fix mirrors GIT-US-0031: a scaffolder in `internal/core` (WASM-safe, `core.FS` only)
reachable from every surface — the CLI, the vault contract (`team.create`), the REST API and
the web app.

A freshly created team repository declares neither projects nor members: connecting projects
is a separate story, and the first member is optional information the browser does not have.
The validation rules of docs/04 §3.5 made both of those errors, so a file the product itself
wrote would have been reported as broken. Those two "none is declared" cases are warnings
now; a malformed entry stays an error. ADR-020 records the decision.

## Acceptance Criteria

- [x] `internal/core` gains `CreateTeam`/`NewTeamConfig`, writing `team.yaml` per docs/04 §3
      plus `.pmngr/boards/`, `.pmngr/sprints/`, `.pmngr/retros/`, `.pmngr/index/` and
      `knowledge/index.md`. It uses `core.FS` only and compiles to WASM.
- [x] The team key is validated against `[A-Z][A-Z0-9-]{1,15}` and an existing `team.yaml` is
      never overwritten.
- [x] The file the scaffolder writes round-trips through `LoadTeamConfig` and re-emits
      byte-identically.
- [x] A freshly created team repository loads with no error diagnostic: "no project declared"
      and "no member declared" are warnings, malformed entries stay errors, and docs/04 §3.5
      plus ADR-020 say so.
- [x] The vault exposes `team.create`, the companion serves
      `POST /api/v1/repos/{id}/team`, and all three web providers implement `createTeam`.
- [x] `gintrack init --team --key <KEY> --name <NAME>` scaffolds a team repository
      non-interactively, supports `--register` and `--json`, and mirrors the project flow.
- [x] `gintrack add --team` on a folder with no `team.yaml` refuses with an actionable error
      instead of registering something inert, and `--key` creates the team repository there.
- [x] The web app has a create-team form modelled on `CreateProjectForm`, reachable from the
      add-repository wizard and from the board and retro empty states.
- [x] docs/04, docs/05 and docs/07 describe the create flow; ADR-020 records the decision.
- [x] `make lint`, `make test`, `make wasm`, `make wasm-smoke` and `make build` pass.

## Notes

The scaffolder deliberately writes no `projects:` entry. A team repository that lists a
project it cannot reach is worse than one that lists none, and the project list is what
decides whether a board card renders live or from a snapshot (ADR-007).
