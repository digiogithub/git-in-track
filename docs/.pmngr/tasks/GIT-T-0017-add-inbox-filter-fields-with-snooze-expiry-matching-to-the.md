---
id: GIT-T-0017
type: task
title: Add inbox filter fields with snooze-expiry matching to the query layer
status: todo
priority: medium
parent: GIT-US-0051
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:15:43Z
updated: 2026-09-13T13:15:43Z
---

## Description

Extend `core.Filter` (`internal/core/query.go:33`) with `Inbox InboxScope` (`exclude` as the zero value, `only`, `include`), `InboxStatuses []InboxStatus` and `SnoozeAsOf Timestamp`. Implement the matching in the filter predicate so that the default filter drops every item whose status is in the `triage` category, `only` keeps just those, and a `snoozed` item whose `snoozed_until` is at or before `SnoozeAsOf` matches a query for `pending`. `SnoozeAsOf` is supplied by the caller; `internal/core` must never read a clock.

## Acceptance Criteria

- [ ] A table-driven test covers each scope, each inbox status and the snooze-expiry boundary (before, exactly at, after).
- [ ] A default `core.Filter{}` excludes triage items with no extra configuration.
- [ ] Sorting, pagination and the opaque cursor are unaffected by the new fields.
- [ ] `go test -race ./internal/core/...` passes.
