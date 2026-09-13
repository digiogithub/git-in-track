---
id: GIT-US-0051
type: story
title: "Inbox data model: reserved triage category and the inbox front-matter block"
status: backlog
priority: high
parent: GIT-EP-0016
milestone: GIT-M-0012
author: mcp
labels: [core, docs]
estimate: 8
created: 2026-09-13T13:11:52Z
updated: 2026-09-13T13:11:52Z
---

## Description

As a product owner, I want incoming work to land in a triage area that is stored as ordinary item files but kept out of the backlog, boards, sprints and metrics, so that unreviewed input never pollutes planning numbers while still getting an id, a history and comments.

Two model changes carry this. First, a fifth reserved `StatusCategory` — `triage` — next to `todo`, `in_progress`, `done` and `cancelled` in `internal/core/model.go:58-76`, accepted by `StatusCategory.Valid()`, by the `project.yaml` workflow decoder (`internal/core/project.go:61` `StatusDef`) and by the `E-PROJ-STATUS-CATEGORY` validation in `internal/core/index.go`. A project that declares no triage status simply has no inbox; `core.CreateProject` (`internal/core/scaffold.go`) seeds one `{id: triage, name: Triage, category: triage}` in the default workflow. Second, a new `inbox` front-matter block on `core.Item` (`internal/core/model.go:324-376`): `status` (`pending|accepted|rejected|snoozed|duplicate`), `snoozed_until` (a `Date`), `duplicate_of` (an `ItemID`), `source` (free string, e.g. `web`, `mcp`, `youtrack`) and `received` (a `Timestamp`). It is parsed in `ParseItem` (`internal/core/frontmatter.go:189`), emitted by `SerializeItem` (`:362`) at a fixed place in the canonical key order, and round-trips through `Extra` preservation rules (R-FMT-6).

The index and the query layer then have to honour the exclusion. `core.Filter` (`internal/core/query.go:33`) gains `Inbox` (tri-state: exclude — the default — only, or include) and `InboxStatuses []InboxStatus`, plus a `SnoozeAsOf Timestamp` so that a snoozed item whose `snoozed_until` has passed is matched as pending without any scheduler. `Index.Build`/`ApplyFileEvents` (`internal/core/index.go:445`, `:843`) keep triage items indexed but tagged, and every consumer that walks the index for planning — `BuildBoardView` (`internal/core/boardview.go`), `BuildSprintView`/`SummarizeSprint` (`internal/core/sprintview.go`), `BuildSprintMetrics` (`internal/core/metrics.go`) and the sprint candidate drawer — filters them out.

## Acceptance Criteria

- [ ] `core.StatusCategory` accepts `triage`; `Valid()`, the workflow decoder and the project diagnostics all know it, and no existing category behaviour changes.
- [ ] `core.Item` carries an `Inbox *ItemInbox` block with `status`, `snoozed_until`, `duplicate_of`, `source` and `received`; `ParseItem` and `SerializeItem` round-trip it byte-for-byte and unknown keys inside the block are still preserved.
- [ ] An item whose status maps to category `triage` is excluded by default from `Index.Query`, board views, sprint views and sprint metrics.
- [ ] `core.Filter` gains `Inbox`, `InboxStatuses` and `SnoozeAsOf`; a `snoozed` item with `snoozed_until <= SnoozeAsOf` matches a `pending` query.
- [ ] Validation: `duplicate_of` must resolve to an existing item (warning when it does not), `snoozed_until` is required for `status: snoozed` and refused otherwise, and an `inbox` block on a non-triage item is a warning.
- [ ] `docs/03-data-model.md` documents the block, the canonical key order (§3.2), the `triage` category (§6) and the JSON Schema (§18).
- [ ] `docs/adr/ADR-033-inbox-is-a-reserved-triage-status-category.md` records why the inbox is a status category plus a front-matter block rather than a new item type or a separate folder, with its negative consequences.
- [ ] `go test -race ./internal/core/...` covers parse, serialize, exclusion and snooze-expiry, and `make wasm` still builds.

## Notes

Existing code: `internal/core/model.go:54-76` (categories), `:324-376` (`Item`), `internal/core/frontmatter.go:189/:362` (parse/serialize + key order), `internal/core/query.go:33` (`Filter`), `internal/core/index.go:445` (`Build`), `internal/core/project.go:54-69` (`Workflow`/`StatusDef`), `internal/core/scaffold.go` (default workflow). Canonical key order is specified at `docs/03-data-model.md:185-198`.

Reference model: Plane's `IntakeIssue` (`apps/api/plane/db/models/intake.py` L50-84) keeps the Issue real from the moment of submission and wraps it with a triage status — the same shape adopted here, minus the join table.

Do NOT add a new `ItemType`, a new folder under `.pmngr/`, or a stored `is_inbox` boolean: the category is the truth and the block is the metadata. Do NOT introduce any scheduler or background job for snooze expiry — it is a query-time comparison (Plane does exactly this: `apps/api/plane/api/views/intake.py` L77). `internal/core` must stay WASM-clean: no `os`, no `net/http`, no `time.Now()` captured implicitly — the caller passes `SnoozeAsOf`.
