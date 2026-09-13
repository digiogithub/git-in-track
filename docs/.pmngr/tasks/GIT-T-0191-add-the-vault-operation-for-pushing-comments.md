---
id: GIT-T-0191
type: task
title: Add the vault operation for pushing comments
status: todo
priority: medium
parent: GIT-US-0079
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:57Z
updated: 2026-09-13T13:19:57Z
---

## Description

Add `youtrack.comment.push` to the vault dispatch table (`internal/vault/vault.go:320-404`) taking `{project, itemId, commentPath?, all?}`, validating the arguments, checking the project link, and enqueueing one push job per selected comment. It returns `{jobId, pushed[], skipped[], failed[]}` so both the MCP tool and the CLI can report without re-deriving anything.

## Acceptance Criteria

- [ ] The method exists in the dispatch table and validates its arguments with field-level errors.
- [ ] `all: true` selects every not-yet-pushed comment of the item; `commentPath` selects one.
- [ ] The result carries pushed, skipped and failed lists.
- [ ] `go test -race ./internal/vault/...` covers both selection modes and the unlinked-item error.
