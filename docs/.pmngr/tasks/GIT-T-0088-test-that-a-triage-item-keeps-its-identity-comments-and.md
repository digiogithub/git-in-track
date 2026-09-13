---
id: GIT-T-0088
type: task
title: Test that a triage item keeps its identity, comments and acceptance path
status: done
priority: medium
parent: GIT-US-0071
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:17:29Z
updated: 2026-09-13T16:19:33Z
started: 2026-09-13T16:19:19Z
closed: 2026-09-13T16:19:33Z
---

## Description

Complement the exclusion suite with the cases the exclusion must not swallow: a triage item is still readable by id through `item.get`, still accepts and lists comments, still appears in `inbox.list`, and — once accepted — appears in the backlog and in a matching board column within the same index generation. Put the accept case in `internal/vault` so it exercises the real write path.

## Acceptance Criteria

- [x] Tests cover read-by-id, comments, inbox listing and the accept-then-appears transition.
- [x] The accept case asserts the item is in the board column its new status maps to.
- [x] `go test -race ./internal/core/... ./internal/vault/...` passes.

## Notes

`internal/core/triageexclusion_test.go` holds `TestTriageItemKeepsItsIdentity`
(readable by id through both the store and the index, accepts and lists a
comment, listed by an inbox query) and `TestAcceptingATriageItemPutsItOnTheBoard`
(the status change alone is enough — the `inbox:` block survives as provenance).

`internal/vault/inboxaccept_test.go` is the real-write-path half, and it runs
through a `Workspace`, not a single vault, because the board lives in the team
repository while the items live in the project one — the arrangement the
guarantee actually has to hold in. Its fixture board declares a column that
names the `triage` status outright; the submission reaches none of the four
columns while it is in the inbox, and `inbox.triage` with `action: accept`
puts it in the To Do column and in the default `item.list` **in the same index
generation**, with nothing reloaded in between. That is the point worth
remembering: the exclusion is a filter over one index, not a second corpus, so
acceptance needs no rebuild.
