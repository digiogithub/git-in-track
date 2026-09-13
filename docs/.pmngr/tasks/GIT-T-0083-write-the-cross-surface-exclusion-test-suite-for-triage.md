---
id: GIT-T-0083
type: task
title: Write the cross-surface exclusion test suite for triage items
status: todo
priority: medium
parent: GIT-US-0071
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 3
created: 2026-09-13T13:17:22Z
updated: 2026-09-13T13:17:22Z
---

## Description

Add a Go test that builds one fixture project declaring a `triage` status, populates it with items in every inbox status plus ordinary backlog items, and asserts that `Index.Query` with a default filter, `BuildBoardView`, `BuildSprintView`, `SummarizeSprint`, the sprint candidate drawer, `BuildSprintMetrics` and `Index.Search` all return byte-identical results with and without the triage items present. Drive it table-wise so a future surface is one line to add.

## Acceptance Criteria

- [ ] The suite covers all seven surfaces and fails loudly if any one of them leaks a triage item.
- [ ] The fixture is shared, not duplicated per test.
- [ ] `go test -race ./internal/core/...` passes.
