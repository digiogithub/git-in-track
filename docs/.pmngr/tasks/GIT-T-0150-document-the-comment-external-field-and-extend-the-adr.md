---
id: GIT-T-0150
type: task
title: Document the comment external field and extend the ADR
status: done
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:18:58Z
updated: 2026-09-13T16:19:37Z
started: 2026-09-13T16:19:21Z
closed: 2026-09-13T16:19:37Z
---

## Description

Update `docs/03-data-model.md` §11 with the comment `external` field and the JSON schema in §18, and extend the `external`-reference ADR written in GIT-EP-0011 to cover comments rather than writing a second ADR. State explicitly that a local delete never deletes remotely. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] `docs/03-data-model.md` §11 and the JSON schema in §18 document the field.
- [x] The existing `external` ADR is extended to comments, including the delete asymmetry.
- [ ] `CHANGELOG.md` has an entry and `make lint` passes.

## Notes

§11.2 already listed the `external` row; what was missing was what it *means*, so §11 gains two
rules. **R-CMT-5** says the reference is what makes a push idempotent — no entry means create
upstream and write the returned id back, an entry means edit in place — and why that matters given
that the job engine re-delivers after a retry, a journal replay or a dead-letter retry. **R-CMT-6**
states the delete asymmetry: a local delete never deletes remotely, no job exists to make it,
a comment deleted upstream is left alone locally, and re-pushing after removing an entry produces a
second remote comment.

§18 gains an outline of `comment.schema.json` carrying `external` over the shared `$def`.

[ADR-031](../adr/ADR-031-external-references.md) gains the asymmetry under "Harder — the price we
are paying", where it belongs: it is a consequence with a cost, not a decision bullet.

Verified against `internal/server/youtrackcomments.go` and `internal/vault/youtrackpush.go` rather
than against the story: there is genuinely no delete job anywhere, and `CommentsToDrafts` skips a
deleted upstream comment rather than removing the local file.

`CHANGELOG.md` was not touched — another agent owns it this wave — so that criterion is unticked.
`make lint` does not lint Markdown; the two failures it reports are in other agents' in-flight Go
and TypeScript files (`internal/config/projectlink_test.go`, `web/src/api/browser-provider.ts`),
none of them touched here.
