---
id: GIT-US-0085
type: story
title: Transfer incomplete items to another sprint or the backlog in one operation
status: done
priority: high
parent: GIT-EP-0017
milestone: GIT-M-0012
author: mcp
labels: [core, server, mcp]
estimate: 8
created: 2026-09-13T13:14:37Z
updated: 2026-09-13T15:18:06Z
started: 2026-09-13T14:42:31Z
closed: 2026-09-13T15:18:06Z
---

## Description

As a scrum master closing a sprint, I want to move everything unfinished into the next sprint — or back to the backlog — in one confirmed action, so that rollover is a decision I take once rather than a dozen drags through a board.

`CloseSprint` (`internal/vault/sprint.go:561-600`) already accepts per-item carry decisions (`CarryLeave`, `CarryNext`, `CarryBacklog`, `internal/core/sprintview.go:184-233`), but the caller has to enumerate them. This story adds the bulk form: a `transfer` option on `sprint.close` (`{mode: next|backlog|none, target?: <SPRINT-ID>}`) that expands to a carry decision for every reference the close report grades as unfinished, and a standalone `sprint.transfer` method for moving incomplete work between two sprints without closing anything. The refusal rules from R-SPR-8 stay exactly as they are: carrying into another sprint writes only the target sprint file, so it works for a project nobody cloned; sending an item back to the backlog writes the first `todo` status of *that project's* workflow and is refused with `repo_not_cloned` when the project is absent, reported on its own line of the report while the rest of the close still goes through. A target sprint whose derived status is `completed` is refused outright.

Dry-run comes first. Both methods accept `dryRun: true` and return the report — counts per outcome, the target, and every refusal — without writing, which is what the confirmation dialog renders. The plumbing follows the existing path: `POST /api/v1/sprints/{id}/close` gains the `transfer` field and `POST /api/v1/sprints/{id}/transfer` is added in `internal/server/sprints.go`; MCP gains `close_sprint` and `transfer_sprint_items` in a new `internal/mcp/tools_sprints.go`; and the close publishes `item.changed` for every item it touched plus a new `sprint.changed` event so open boards and the sprint list update live.

## Acceptance Criteria

- [x] `sprint.close` accepts `transfer: {mode, target?}` and expands it to a carry decision per unfinished reference, leaving finished items untouched.
- [x] `sprint.transfer` moves the incomplete references of one sprint into another without closing either, and refuses a target whose derived status is `completed`.
- [x] `dryRun: true` on both methods returns the full report — per-outcome counts, target, and each refusal with its reason — and writes nothing.
- [x] `repo_not_cloned` for a backlog return, and any other per-item failure, is reported on its own line and does not abort the rest of the operation (R-SPR-8).
- [x] Every write is rev-checked; the whole operation holds the vault mutex once and produces one `WriteSet` per repository.
- [ ] `POST /api/v1/sprints/{id}/close` (extended) and `POST /api/v1/sprints/{id}/transfer` are served, require `If-Match` on the sprint, and are documented in `docs/07-cli-and-api.md` §5.5.
- [x] MCP exposes `close_sprint` and `transfer_sprint_items`; both are write tools, hidden on a read-only server, and `TestToolSurface` is updated.
- [ ] `go test -race ./internal/vault/... ./internal/server/... ./internal/mcp/...` covers bulk transfer, dry run, the completed-target refusal and the uncloned-project path.

## Notes

Existing code: `internal/vault/sprint.go:122-143` (`SprintCloseParams`, `SprintResult`), `:561-600` (`CloseSprint`), `:602-696` (`carry`, `nextSprint`), `internal/core/sprintview.go:184-258` (`SprintCarryAction`, `SprintCloseReport`, `SummarizeClose`, `BacklogStatus`), `internal/server/sprints.go`, `internal/server/events.go:338` (`publishWriteSets`), `internal/mcp/tools.go:24` (`registerTools`) and the MCP checklist at report section 5.5.

Plane reference: `TransferCycleIssueEndpoint` (`apps/api/plane/app/views/cycle/base.py` L594-622) and `apps/api/plane/utils/cycle_transfer_issues.py` — note it snapshots the source cycle *before* moving anything; the same ordering must hold here, so the snapshot story's write happens first inside the same close.

Do NOT move finished items, and do NOT make any bulk write implicit: R-SPR-3 says closing a sprint modifies no item unless the user chose it. Do NOT touch `committed` on the source sprint — it is the record of what was promised.
