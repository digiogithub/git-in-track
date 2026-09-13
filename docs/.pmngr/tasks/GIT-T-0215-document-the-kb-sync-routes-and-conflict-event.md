---
id: GIT-T-0215
type: task
title: Document the KB sync routes and conflict event
status: in_review
priority: medium
parent: GIT-US-0090
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:21:22Z
updated: 2026-09-13T16:20:51Z
started: 2026-09-13T16:20:39Z
---

## Description

Document the three routes and the per-page status shape in `docs/07-cli-and-api.md` §4, and the `youtrack.kb.conflict` event next to the `sync.job.*` events in §5.6. Explain the conflict semantics — last writer wins per direction, both-changed produces `<page>.conflict.md` — where a reader of the KB docs will find them. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The routes and the status shape are documented in `docs/07-cli-and-api.md` §4.
- [x] The conflict event and semantics are documented in §5.6 and in the KB documentation.
- [ ] `CHANGELOG.md` has an entry and `make lint` passes.

## Notes

**Partial, and deliberately left in review.** The knowledge-base half landed:
`docs/03-data-model.md` §14.6 "Deciding who changed" now documents the five states
(`unlinked`, `in_sync`, `local_ahead`, `remote_ahead`, `conflict`), why change detection is
content-based rather than byte-based, that `remote_ahead` is only ever reported when the caller
asked for the remote read, and the conflict semantics — last writer wins per direction, both
changed writes `<page>.conflict.md` beside the page with a `conflict_of` front-matter key and
leaves the original untouched, and there is deliberately no three-way merge. `youtrack.kb.conflict`
is already documented in `docs/07-cli-and-api.md` §5.6, which landed with wave 4.

What is left is the first criterion, and it is not the docs agent's to close. At the commit this
was verified against (`7d8d9a3`) the three routes **did not exist**: `mountYouTrack` served
settings, test, projects, fields, issues and the two import routes and nothing else, and the KB
methods were reachable only over the core API and the two MCP tools. GIT-T-0214 is landing them
now; the `docs/07-cli-and-api.md` §4 entry belongs with that work, in the file that agent owns.

`CHANGELOG.md` was not touched — another agent owns it this wave.
