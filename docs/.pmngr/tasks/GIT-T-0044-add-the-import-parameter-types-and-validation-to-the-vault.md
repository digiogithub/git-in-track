---
id: GIT-T-0044
type: task
title: Add the import parameter types and validation to the vault
status: done
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:16:22Z
updated: 2026-09-13T15:07:55Z
started: 2026-09-13T15:07:41Z
closed: 2026-09-13T15:07:55Z
---

## Description

Add `internal/vault/youtrack.go` with the request and result types for the import — `importParams{Project, Query, IDs, Depth, IncludeLinks, IncludeComments, IncludeAttachments}` and `importResult{Issues []issueResult}` — and register `youtrack.import.preview` and `youtrack.import.run` as cases in `Vault.Dispatch` (`internal/vault/vault.go:320-404`), with workspace routing in `internal/vault/dispatch.go:73`. Validation is field-level: exactly one of `query` or `ids` must be present, `depth` is 0 to 5, and the project must be linked. A YouTrack client is supplied through a small interface on the vault so tests can substitute a fake.

## Acceptance Criteria

- [x] Both methods appear in the dispatch table and are reachable through the workspace layer.
- [x] Parameter validation returns field-level errors for missing, conflicting and out-of-range arguments.
- [x] The client is an interface on the vault, not a concrete type, so a fake can be injected.
- [x] `go test -race ./internal/vault/...` covers the validation cases.

## Notes

`YouTrackImportParams`, `YouTrackImportPreview` and `YouTrackImportResult` live in `internal/vault/youtrack.go`. Both methods are cases in `Vault.Dispatch` answered **before** the vault mutex is taken (resolution talks to the network), and they are cases in `Workspace.Dispatch`, routed by `project` like any other project call. Every validation message names the offending parameter in quotes (`"query" or "ids"`, `"depth"`).

The client is the `YouTrackSource` interface (`Issue`, `IssueLinks`, `SearchAllIssues`, `AllComments`, `Attachments`), handed to the vault by a `YouTrackProvider` installed with `SetYouTrackProvider`; `*youtrack.Client` satisfies it and a host without one gets `unavailable`. Link configuration travels as the plain `vault.YouTrackLink` struct so the vault keeps compiling to WebAssembly without `internal/config`.
