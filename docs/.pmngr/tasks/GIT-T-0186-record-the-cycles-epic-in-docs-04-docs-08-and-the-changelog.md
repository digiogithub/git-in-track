---
id: GIT-T-0186
type: task
title: Record the cycles epic in docs/04, docs/08 and the CHANGELOG
status: done
priority: medium
parent: GIT-US-0092
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:19:46Z
updated: 2026-09-13T17:24:20Z
started: 2026-09-13T17:24:02Z
closed: 2026-09-13T17:24:20Z
---

## Description

Finish the documentation pass for the epic: confirm `docs/04-team-repository.md` §8 covers optional dates, the derived-status table, the draft overlap exemption and the `snapshot` block as a single coherent section; add `close_sprint` and `transfer_sprint_items` to the `docs/08-mcp-server.md` §4 table; link ADR-034 from `docs/adr/README.md`; and record the whole epic in `CHANGELOG.md` under Unreleased, noting that existing sprint files are unaffected and gain a snapshot only when they are next closed.

## Acceptance Criteria

- [x] `docs/04` §8 reads as one consistent section with no contradictions left from the earlier stories.
- [x] `docs/08` §4 lists the two new tools with their annotations and ADR-034 is in the index.
- [x] The CHANGELOG entry covers optional dates, derived status, the snapshot and the transfer, plus the backward-compatibility note.
- [x] `make lint` passes.

## Notes

The second criterion was already satisfied: `docs/08-mcp-server.md` §4 lists `close_sprint` (write, `sprint.close`) and `transfer_sprint_items` (write, `sprint.transfer`) in the tool table and gives them §4.14 and §4.15, and `docs/adr/README.md` carries ADR-034. §8.2 of docs/04 already read coherently — R-SPR-9 (dates together or not at all, a dateless sprint is a draft), R-SPR-10 (listing order by derived status), R-SPR-11 (the snapshot written exactly once), the derived-status table and the full `snapshot` key table with its emitted shape.

Two contradictions left by the earlier stories were real and are fixed.

`docs/04` §8's header paragraph still promised that "§8.2 says which halves of them the vault and the API drive today and which are still model-only" — a pointer to markers §8.2 no longer carries, because nothing is model-only any more. Verified against the code before rewriting it: `internal/vault/sprint.go` accepts and parks a dateless sprint (`checkSprintDates`, R-SPR-9), `SprintClose` freezes the snapshot before any carry decision rewrites an item out of the scope, and `internal/vault/metrics.go` answers a closed sprint from `core.SprintMetricsFromSnapshot` without touching git. The paragraph now says the feature is wired end to end and points at the CLI, REST and MCP surfaces.

`CHANGELOG.md` carried the same stale caveat, and worse: it stated that dateless sprints were "still refused by `sprint.create` and `sprint.update`" and that "`sprint.close` does not write one yet and the metrics do not read one back yet". All three are now false. The entry says so, and carries the backward-compatibility note the criterion asks for: existing sprint files parse unchanged, keep whatever dates they have, and gain a `snapshot` block only when they are next closed — a sprint closed before this change has none, and its metrics keep being reconstructed from git.
