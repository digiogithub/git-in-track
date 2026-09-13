---
id: GIT-T-0223
type: task
title: Document the KB sync tools and commands
status: done
priority: medium
parent: GIT-US-0094
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:22:01Z
updated: 2026-09-13T16:40:26Z
started: 2026-09-13T16:40:05Z
closed: 2026-09-13T16:40:26Z
---

## Description

Document both KB sync tools in the `docs/08-mcp-server.md` §4 tool table and `gintrack youtrack kb push|pull` in `docs/07-cli-and-api.md` §4, each with an example, including the non-zero exit on conflict so CI authors can rely on it. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Both surfaces are documented with examples and the exit-code behaviour is stated.
- [x] `CHANGELOG.md` has an entry.
- [x] `make lint` passes.

## Notes

`docs/08` §4.18 was already done. This closes the `docs/07` half: §4.15 gained the `kb push|pull` subsection with a `--wait` example that shows a conflicted page and `echo $?` printing 5, plus `kb status` and why `--remote` is opt-in. §5.5 gained the three REST routes under their own heading, with the mount spellings, the 404 on an unlinked project and the conflict rule.

Worth knowing for a CI author: a conflict and a failed job have **different** exit codes — 5 and 1 — because they need different responses.
