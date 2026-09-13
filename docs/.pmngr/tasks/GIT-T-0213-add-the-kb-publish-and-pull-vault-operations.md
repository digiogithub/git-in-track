---
id: GIT-T-0213
type: task
title: Add the KB publish and pull vault operations
status: done
priority: medium
parent: GIT-US-0090
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:21:10Z
updated: 2026-09-13T15:33:38Z
started: 2026-09-13T15:33:25Z
closed: 2026-09-13T15:33:38Z
---

## Description

Add `youtrack.kb.publish` and `youtrack.kb.pull` to the vault dispatch table taking `{project, path, recursive}`. They validate the path, check the project link and enqueue the corresponding job, returning the job id rather than blocking. Add workspace routing in `internal/vault/dispatch.go:73` so the companion can address them per project.

## Acceptance Criteria

- [x] Both methods exist, validate their arguments and check the project link.
- [x] Each enqueues its job and returns the job id without blocking.
- [x] Workspace routing resolves the project correctly.
- [x] `go test -race ./internal/vault/...` covers validation, the unlinked-project error and the enqueue.

## Notes

Landed in `internal/vault/youtrackkb.go`, routed in `internal/vault/dispatch.go`.

The enqueue goes through a new host seam, `Vault.SetYouTrackEnqueuer`, mirroring
`SetYouTrackProvider`: the vault decides that a job is needed and hands it over
as a `YouTrackJob{Kind, Key, Project, Payload}`; it never runs one, so no
network call can reach the write path or `internal/core`. A host with no engine
gets `unavailable` rather than a silent no-op. The coalescing key is the
selection (`<project>:<path>[:recursive]`), so two clicks on the same folder
fold into one job in the engine.

The job kinds are `vault.JobKindKBPublish` and `vault.JobKindKBPull`. Their
handlers are not part of this task — they belong to the job-kind story of this
epic — and the payload they receive is the `YouTrackKBParams` of the call.
