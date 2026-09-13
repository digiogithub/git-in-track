---
id: GIT-T-0044
type: task
title: Add the import parameter types and validation to the vault
status: todo
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:16:22Z
updated: 2026-09-13T13:16:22Z
---

## Description

Add `internal/vault/youtrack.go` with the request and result types for the import — `importParams{Project, Query, IDs, Depth, IncludeLinks, IncludeComments, IncludeAttachments}` and `importResult{Issues []issueResult}` — and register `youtrack.import.preview` and `youtrack.import.run` as cases in `Vault.Dispatch` (`internal/vault/vault.go:320-404`), with workspace routing in `internal/vault/dispatch.go:73`. Validation is field-level: exactly one of `query` or `ids` must be present, `depth` is 0 to 5, and the project must be linked. A YouTrack client is supplied through a small interface on the vault so tests can substitute a fake.

## Acceptance Criteria

- [ ] Both methods appear in the dispatch table and are reachable through the workspace layer.
- [ ] Parameter validation returns field-level errors for missing, conflicting and out-of-range arguments.
- [ ] The client is an interface on the vault, not a concrete type, so a fake can be injected.
- [ ] `go test -race ./internal/vault/...` covers the validation cases.
