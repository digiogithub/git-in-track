---
id: GIT-US-0016
type: story
title: Load a team repository and its projects
status: in_review
created: 2026-09-03T00:00:00Z
updated: 2026-09-04
author: team
priority: critical
parent: GIT-EP-0004
milestone: GIT-M-0004
estimate: 5
labels: [core, web]
links:
  - kind: blocked_by
    target: GIT-US-0015
---

## Description

As a team lead, I want to open our team repository and have it pull together every project
we work on, so that the team has one place to look instead of one tab per repository.

`team.yaml` defines the team metadata, members, and the list of project repositories: remote
URL, default branch, docs folder path, and project key. The app opens the team repo plus any
project repos the user has locally, resolves `ref: <projectKey>/<itemId>` references across
them, and provides unified search over every open vault and the team knowledge base.

Projects that are not cloned locally are recognised and marked as remote, ready for
GIT-US-0019.

## Acceptance Criteria

- [x] `team.yaml` is parsed and validated, with clear errors for malformed entries.
- [x] Members, projects and the team knowledge base are all visible in the UI.
- [x] Several project repositories can be open at once alongside the team repository.
- [x] `ref: <projectKey>/<itemId>` resolves to the right item across repositories.
- [x] Search spans all open vaults and shows which project each result came from.
- [x] Projects not present locally are listed and marked as not cloned.
- [x] Project keys are validated as unique within a team.
- [x] The `team-basic` fixture loads end to end in both operating modes.

## Notes

Implemented in `internal/core` (`team.go`, `teamdiscover.go`: `TeamConfig`, `TeamRef`, `Ref`,
`DiscoverTeam`, the `E-TEAM-*` catalog) and `internal/vault` (`Workspace`: multi-repository
routing, `team.get`, `ref.resolve`, `workspace.*`, merged search). The companion serves it at
`/api/v1/{workspace,teams,teams/{key},refs}` and a project-less `/api/v1/search`; the browser
worker hosts a `vault.Workspace` instead of a single vault, so both modes answer the same
contract. The web app gained `features/workspace/TeamPanel.tsx` and `WorkspaceSearch.tsx`.

Snapshot rendering for a project that is not cloned stays with GIT-US-0019: this story recognises
and marks it, and offers the clone URL.
