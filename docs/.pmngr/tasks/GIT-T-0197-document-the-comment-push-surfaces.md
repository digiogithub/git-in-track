---
id: GIT-T-0197
type: task
title: Document the comment push surfaces
status: done
priority: medium
parent: GIT-US-0079
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:20:09Z
updated: 2026-09-13T16:40:16Z
started: 2026-09-13T16:40:04Z
closed: 2026-09-13T16:40:16Z
---

## Description

Document `push_comment_to_youtrack` in the `docs/08-mcp-server.md` §4 tool table and `gintrack youtrack push-comments` in `docs/07-cli-and-api.md` §4, each with an example, and state that a local delete never deletes remotely. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Both surfaces are documented with examples.
- [x] The delete asymmetry is stated where a reader will find it.
- [x] `CHANGELOG.md` has an entry and `make lint` passes.

## Notes

`docs/08` §4.17 was already done. This closes the `docs/07` half: §4.15 gained the `push-comments` subsection with a `--wait` example showing a pushed comment beside a skipped one, the three things a reader has to know (`pushed` means queued, an already-referenced comment is skipped rather than sent twice, an unlinked item is refused non-retryably with exit 3), and the delete asymmetry in its own bold paragraph.
