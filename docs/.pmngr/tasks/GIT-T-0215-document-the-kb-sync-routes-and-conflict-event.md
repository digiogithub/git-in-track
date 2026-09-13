---
id: GIT-T-0215
type: task
title: Document the KB sync routes and conflict event
status: done
priority: medium
parent: GIT-US-0090
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:21:22Z
updated: 2026-09-13T16:56:47Z
started: 2026-09-13T16:20:39Z
closed: 2026-09-13T16:56:47Z
---

## Description

Document the three routes and the per-page status shape in `docs/07-cli-and-api.md` §4, and the `youtrack.kb.conflict` event next to the `sync.job.*` events in §5.6. Explain the conflict semantics — last writer wins per direction, both-changed produces `<page>.conflict.md` — where a reader of the KB docs will find them. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] The routes and the status shape are documented in `docs/07-cli-and-api.md` §4.
- [x] The conflict event and semantics are documented in §5.6 and in the KB documentation.
- [x] `CHANGELOG.md` has an entry and `make lint` passes.

## Notes

The knowledge-base half landed first: `docs/03-data-model.md` §14.6 "Deciding who changed" documents the five states (`unlinked`, `in_sync`, `local_ahead`, `remote_ahead`, `conflict`), why change detection is content-based rather than byte-based, that `remote_ahead` is only ever reported when the caller asked for the remote read, and the conflict semantics — last writer wins per direction, both changed writes `<page>.conflict.md` beside the page with a `conflict_of` front-matter key and leaves the original untouched, and there is deliberately no three-way merge. `youtrack.kb.conflict` is documented in `docs/07-cli-and-api.md` §5.6.

The first criterion is now closed. GIT-T-0214 landed the three routes and `docs/07-cli-and-api.md` documents them under "Knowledge-base synchronization (GIT-US-0087, GIT-US-0090)": the route table, the `?key=` convention, the three scoped spellings mounted from the same handlers, the 404 on a project with no block, the full per-page status shape with `linked`, `articleId`, `url`, `state`, `syncedAt` and the per-page `error`, why `remote` is never defaulted on, and the queued `202 {project, jobId, pages}` answer of publish and pull. **Correction to this task's description**: the routes are REST, so they live in §5.5 and not in §4 — §4 is the CLI surface, where `gintrack youtrack kb` is documented separately.

`CHANGELOG.md` carries the entry ("The HTTP surface of knowledge-base synchronization (GIT-US-0090, docs/07 §5.5)"), written by the agent who owns that file. `make lint` passes with zero Go issues.
