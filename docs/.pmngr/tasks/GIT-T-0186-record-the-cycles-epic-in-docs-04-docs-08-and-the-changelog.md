---
id: GIT-T-0186
type: task
title: Record the cycles epic in docs/04, docs/08 and the CHANGELOG
status: todo
priority: medium
parent: GIT-US-0092
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:19:46Z
updated: 2026-09-13T13:19:46Z
---

## Description

Finish the documentation pass for the epic: confirm `docs/04-team-repository.md` §8 covers optional dates, the derived-status table, the draft overlap exemption and the `snapshot` block as a single coherent section; add `close_sprint` and `transfer_sprint_items` to the `docs/08-mcp-server.md` §4 table; link ADR-034 from `docs/adr/README.md`; and record the whole epic in `CHANGELOG.md` under Unreleased, noting that existing sprint files are unaffected and gain a snapshot only when they are next closed.

## Acceptance Criteria

- [ ] `docs/04` §8 reads as one consistent section with no contradictions left from the earlier stories.
- [ ] `docs/08` §4 lists the two new tools with their annotations and ADR-034 is in the index.
- [ ] The CHANGELOG entry covers optional dates, derived status, the snapshot and the transfer, plus the backward-compatibility note.
- [ ] `make lint` passes.
