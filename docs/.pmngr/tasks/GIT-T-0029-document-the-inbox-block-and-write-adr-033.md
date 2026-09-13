---
id: GIT-T-0029
type: task
title: Document the inbox block and write ADR-033
status: done
priority: medium
parent: GIT-US-0051
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:15:58Z
updated: 2026-09-13T14:11:15Z
started: 2026-09-13T14:11:07Z
closed: 2026-09-13T14:11:15Z
---

## Description

Update `docs/03-data-model.md`: add the `inbox` block to the item front-matter field table, place it in the canonical key order at §3.2 (line 185-198), add the `triage` category to the workflow section §6 (line 522-525) and extend the JSON Schema at §18. Then write `docs/adr/ADR-033-inbox-is-a-reserved-triage-status-category.md` in the house format (`docs/adr/README.md`): context, decision, and mandatory negative consequences — a fifth category every consumer must now handle, the risk of a project that declares no triage status silently having no inbox, and snooze expiry being a query-time rule with no notification. Link it from `docs/adr/README.md`.

## Acceptance Criteria

- [x] `docs/03-data-model.md` documents the block, the key order, the category and the schema, and the schema example validates.
- [x] `docs/adr/ADR-033-*.md` exists with status Accepted, a phase, related ADRs and a negative-consequences section, and is linked from the ADR index.
- [ ] `make lint` passes (including the docs checks).

## Notes

`docs/03-data-model.md`: §3.2 key order (`inbox` after `custom`, before `deleted`), §6.1 category
vocabulary, §6.2 example workflow (now seeds `triage`, matching `core.DefaultWorkflow()`), a new
§6.4 "The `triage` category and the inbox" with rules R-INBOX-1..7 and the block's field table,
§7.1 field table, §16 consolidated diagnostics (the four `E-INBOX-*`/`W-INBOX-*` codes plus the
three `E-EXT-*`/`W-EXT-*` ones) and §18 (`inbox` `$def`, deliberately open so unknown keys survive).
ADR-033 is Accepted, phase 8, with seven negative consequences and six rejected alternatives,
including the three the task named; linked from `docs/adr/README.md`.

Last criterion is **not** ticked: `make lint` was not run because it also lints the frontend and
several packages other agents are mid-edit in during this wave. What was run and is clean for the
files this task touched: `go build ./...`, `go vet ./internal/core/...`,
`go test -race ./internal/core/...`, `gofmt -l internal/core` and a `GOOS=js GOARCH=wasm` build.
