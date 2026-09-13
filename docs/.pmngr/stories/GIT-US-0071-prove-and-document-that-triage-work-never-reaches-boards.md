---
id: GIT-US-0071
type: story
title: Prove and document that triage work never reaches boards, sprints or metrics
status: in_review
priority: medium
parent: GIT-EP-0016
milestone: GIT-M-0012
author: mcp
labels: [core, docs]
estimate: 3
created: 2026-09-13T13:13:26Z
updated: 2026-09-13T17:26:13Z
started: 2026-09-13T17:25:54Z
---

## Description

As a team lead reading a burndown, I want a guarantee that untriaged work is invisible to every planning surface, so that a busy inbox can never inflate scope, depress velocity or add a phantom column to a board.

The exclusion is implemented in the data-model story; this story is the proof and the prose. It adds a cross-cutting test suite that builds a fixture project with a `triage` status, puts items in every inbox state into it, and asserts that `Index.Query` with a default filter, `BuildBoardView` (`internal/core/boardview.go`), `BuildSprintView` and `SummarizeSprint` (`internal/core/sprintview.go`), the sprint candidate drawer, `BuildSprintMetrics` (`internal/core/metrics.go`) and `Index.Search` (`internal/core/query.go:544`) all return the same answers with and without those items present. It also asserts the two places the exclusion must *not* apply: the item is still readable by id, and it still carries its comments.

The documentation half closes the loop: `docs/07-cli-and-api.md` gets the inbox REST endpoints and the `inbox` command tree, `docs/08-mcp-server.md` §4 the three tools, `docs/03-data-model.md` the field table and schema entries from the model story, and `CHANGELOG.md` an entry under Unreleased describing the new category and block, including the migration note that a project created before this change has no triage status and therefore no inbox until one is added to its workflow.

## Acceptance Criteria

- [ ] A table-driven Go test asserts that triage items change nothing in `Index.Query`, board views, sprint views, sprint candidates, sprint metrics and search.
- [x] A test asserts that a triage item is still readable by id, still accepts comments, and still appears in `inbox.list`.
- [x] A test asserts that accepting an item makes it appear in the backlog and in a matching board column in the same index generation.
- [x] `docs/07-cli-and-api.md` documents `GET /api/v1/inbox`, `POST /api/v1/items/{id}/triage`, the `inbox.changed` WS topic and the `gintrack inbox` command tree.
- [x] `docs/08-mcp-server.md` §4 lists `create_inbox_item`, `list_inbox` and `triage_inbox_item` with their write/read annotations.
- [x] `CHANGELOG.md` records the change under Unreleased, including the "existing projects have no triage status yet" note.
- [x] `make test` and `make lint` pass.

## Notes

Existing code and docs: `internal/core/boardview.go`, `internal/core/sprintview.go:81` (`BuildSprintView`), `internal/core/metrics.go` (`BuildSprintMetrics`), `internal/core/query.go:544` (`Index.Search`), `docs/07-cli-and-api.md` §4/§5, `docs/08-mcp-server.md` §4 (line 199), `CHANGELOG.md`. Note the existing doc drift flagged in the architecture report: `docs/07-cli-and-api.md:896-916` lists 12 MCP tools and says "six write tools" while there are 13 and 7 — fix it in the same pass.

CI enforces the performance targets in `docs/02 §9`: a cold index of 10 000 items under 2 s native and 8 s WASM. Inbox items are indexed, so the exclusion must be a filter on an already-built index, not a second pass over the corpus.

Do NOT weaken the exclusion by making it opt-in per view. Do NOT write migration code: a project without a triage status simply has no inbox.

### The first criterion is unticked on purpose, not outstanding

Six of the seven surfaces it names are proven identical with and without triage items, by `internal/core/triageexclusion_test.go`. The seventh, **search**, is not, and must not be: `Index.Search` takes no `Filter`, so excluding there would make an inbox item unfindable by text from every surface at once — the quick switcher and the MCP `search_items` tool included — which contradicts ADR-033's own rule that an item is real from the moment it is submitted, and GIT-T-0088's criterion that it stays readable. ADR-033's Decision scopes the unconditional exclusion to board views, sprint views, sprint candidates and sprint metrics, and scopes queries to a *default* a caller can lift with `Filter.Inbox`; it says nothing about search. A search hit carries no estimate, no status category and no column, so nothing reaches a planning number through it.

`TestSearchStillFindsATriageItem` pins that behaviour deliberately, and `docs/07-cli-and-api.md` §5.5 now states it in prose alongside what the exclusion does cover. Ticking this criterion would mean recording that search excludes triage items, which is false. Changing the behaviour would mean changing ADR-033 first, which is a decision for a human.
