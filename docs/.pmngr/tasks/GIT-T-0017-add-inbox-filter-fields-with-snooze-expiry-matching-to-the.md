---
id: GIT-T-0017
type: task
title: Add inbox filter fields with snooze-expiry matching to the query layer
status: done
priority: medium
parent: GIT-US-0051
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:15:43Z
updated: 2026-09-13T14:08:17Z
started: 2026-09-13T14:08:10Z
closed: 2026-09-13T14:08:17Z
---

## Description

Extend `core.Filter` (`internal/core/query.go:33`) with `Inbox InboxScope` (`exclude` as the zero value, `only`, `include`), `InboxStatuses []InboxStatus` and `SnoozeAsOf Timestamp`. Implement the matching in the filter predicate so that the default filter drops every item whose status is in the `triage` category, `only` keeps just those, and a `snoozed` item whose `snoozed_until` is at or before `SnoozeAsOf` matches a query for `pending`. `SnoozeAsOf` is supplied by the caller; `internal/core` must never read a clock.

## Acceptance Criteria

- [x] A table-driven test covers each scope, each inbox status and the snooze-expiry boundary (before, exactly at, after).
- [x] A default `core.Filter{}` excludes triage items with no extra configuration.
- [x] Sorting, pagination and the opaque cursor are unaffected by the new fields.
- [x] `go test -race ./internal/core/...` passes.

## Notes

`InboxScope` (`""` = exclude, `only`, `include`) and `ParseInboxScope` are in
`internal/core/inbox.go`; matching is `Index.matchInbox` in `query.go`, which resolves the category
through the project workflow rather than trusting any field on the item — so a project that declares
no triage status simply has no inbox and nothing is ever hidden from it. Snooze expiry is
`ItemInbox.EffectiveStatus(asOf)`, a pure comparison; a zero `SnoozeAsOf` expires nothing, and core
still reads no clock.

An item in triage with **no** `inbox` block counts as `pending` (nobody has looked at it yet), and an
`InboxStatuses` filter never matches a non-triage item. `Index.Inbox(ctx, f)` is a convenience that
forces `InboxOnly`, so a surface rendering the triage queue cannot get the scope wrong.
