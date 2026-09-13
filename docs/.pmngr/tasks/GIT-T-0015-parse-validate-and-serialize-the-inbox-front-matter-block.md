---
id: GIT-T-0015
type: task
title: Parse, validate and serialize the inbox front-matter block
status: todo
priority: medium
parent: GIT-US-0051
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:15:37Z
updated: 2026-09-13T13:15:37Z
---

## Description

Add `Inbox *ItemInbox` to `core.Item` (`internal/core/model.go:324-376`) with `Status InboxStatus` (`pending|accepted|rejected|snoozed|duplicate`), `SnoozedUntil Date`, `DuplicateOf ItemID`, `Source string` and `Received Timestamp`. Parse it in `ParseItem` (`internal/core/frontmatter.go:189`) with the existing typed readers, emit it from `SerializeItem` (`:362`) at a fixed position in the canonical key order, and preserve unknown keys inside the block the way `Item.Extra` preserves unknown top-level keys (R-FMT-6). Add the validation rules: `snoozed_until` required for and only for `status: snoozed`, `duplicate_of` must parse as an item id, and an `inbox` block on an item whose status is not in the `triage` category is a warning.

## Acceptance Criteria

- [ ] A round-trip test parses a file with an `inbox` block, serializes it and gets byte-identical output, including an unknown key inside the block.
- [ ] Each validation rule has a test asserting its diagnostic code and severity.
- [ ] The canonical key order test in `internal/core` is updated and still pins the full order.
- [ ] `go test -race ./internal/core/...` passes.
