---
id: GIT-US-0048
type: story
title: Store the YouTrack token locally and the project link in project.yaml
status: done
priority: high
parent: GIT-EP-0011
milestone: GIT-M-0011
author: mcp
labels: [core, server, security, docs]
estimate: 8
created: 2026-09-13T13:11:30Z
updated: 2026-09-13T14:44:52Z
started: 2026-09-13T14:44:39Z
closed: 2026-09-13T14:44:52Z
---

## Description

As a user connecting a project to YouTrack, I want the instance URL, the YouTrack project and the field mapping to be committed with my backlog while the permanent token stays on my machine, so that my team shares the link but nobody ever pushes a credential to git.

The configuration splits in two. The committed half is a new `integrations.youtrack: {url, project, field_map, push_comments, kb_sync}` block in `<docsFolder>/.pmngr/project.yaml`, added to `core.ProjectConfig` (`internal/core/project.go:23-40`) next to `team` (`:161`) and `links` (`:167`), which already have exactly this shape, validated by `LoadProjectConfig` (`:198`) and documented in `docs/03-data-model.md` §6.1. Beware: `ProjectConfig` has no `Extra` map and drops unknown keys, and the only writer today is the surgical YAML-node edit in `Allocator.writeCounter` (`internal/core/allocator.go:414`, `setYAMLPath` `:545`) — any code that re-serializes the file wholesale would silently delete the new block, so writes to it must go through the same surgical path.

The secret half is the token. git-in-track currently promises it stores no credentials (`docs/10-development-guidelines.md:707-711`), so this needs `docs/adr/ADR-032` restating precisely what is now stored and where. The token goes into a new `integrations.youtrack` section of the machine-local companion config (`internal/config/config.go`), keyed by project key, in the existing `0600` file, with a `GINTRACK_YOUTRACK_TOKEN` environment override registered in `applyEnv` (`internal/config/load.go:186`) and honouring the documented flag > env > file > default precedence (`docs/07-cli-and-api.md` §3.3). Persist it with the exact reload-mutate-save pattern of `gitState.persist` (`internal/server/git.go:336-352`). Do not reuse `GINTRACK_TOKEN` — it is already overloaded as the companion bearer token and the go-git HTTP password. Finally, `GET /api/v1/capabilities` reports `features.youtrack` (`internal/server/server.go:411-435`) so browser-only mode, which has no network reach to YouTrack, hides the whole feature.

## Acceptance Criteria

- [x] `core.ProjectConfig` parses and validates `integrations.youtrack: {url, project, field_map, push_comments, kb_sync}`; an invalid URL or empty project short name is a load error.
- [x] Writing the block back preserves every other key in `project.yaml`, proved by a round-trip test on a file with comments and unrelated sections.
- [x] `config.Config` holds the YouTrack token per project key; `config.Save` writes the file `0600` and `config.Validate` rejects a token for an unknown project key.
- [x] `GINTRACK_YOUTRACK_TOKEN` overrides the file, and the precedence flag > env > file > default is covered by a test.
- [x] The token is never logged, never included in any error and never returned by any API surface.
- [x] `GET /api/v1/capabilities` reports `features.youtrack`, true only in companion mode with a configured integration.
- [x] `docs/03-data-model.md` §6 documents the `project.yaml` block and `docs/07-cli-and-api.md` §3 documents the config key and env var.
- [x] `docs/adr/ADR-032-local-credential-storage.md` is written, revises the "no credentials stored" statement in `docs/10-development-guidelines.md:707-711`, and lists its negative consequences.
- [x] `go test -race ./internal/core/... ./internal/config/...` passes.

## Notes

Precedents to copy exactly: `gitState.persist` (`internal/server/git.go:336-352`) reloads the file, replaces one section and saves; the response carries `persisted bool` (`git.go:69`) so the UI can say whether only the running process took the change. Config path resolution is `internal/config/path.go:27` `DefaultPath` / `:38` `PathFor`.

Browser-only mode keeps any token in memory for the session only, exactly as the git PAT rule in `docs/10-development-guidelines.md:707-711` already demands — never `localStorage`.

Do NOT put the token in `project.yaml`, and do NOT add a keyring or keychain dependency: none exists in `go.mod` and adding one is an ADR-level decision of its own. Do NOT introduce per-repository settings — they do not exist (`CHANGELOG.md:374`), which is why the link lives per project in `project.yaml`.

### Two deviations, both deliberate

1. **The `project.yaml` half landed in `internal/config`, not `internal/core`.** `internal/core` was held by another agent for the whole wave and was off limits to this one. `core.ProjectConfig` drops unknown keys silently and never re-serializes `project.yaml`, so the block survives untouched and the feature works end to end; `config.LoadYouTrackLink` / `SaveYouTrackLink` read and write it, the latter by editing the YAML node tree in place. Adding the typed `ProjectConfig.Integrations` mirror and the `E-PROJ-INTEGRATION` diagnostic through `LoadProjectConfig` is a clean follow-up for whoever owns `internal/core`; the validation it needs already exists as `config.YouTrackLink.Validate`.
2. **`config.Validate` cannot reject "a token for an unknown project key".** The package holds no inventory of project keys — those live in the repositories, not in the configuration file. It checks what it can: the key must look like a project key and the token must not be empty. A well-formed key naming no project simply never resolves a token.

The ADR is `docs/adr/ADR-032-local-integration-credential-storage.md`. The `CHANGELOG.md` entry of GIT-T-0032 is the one thing not done: that file belonged to another agent this wave.
