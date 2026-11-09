---
id: GIT-US-0217
type: story
title: jj test fixtures push new bookmarks on jj releases without --allow-new
status: done
priority: medium
author: claude-code
assignees: [claude-code]
labels: [git, ci, agent-ok]
estimate: 1
created: 2026-11-08T19:10:00Z
updated: 2026-11-09T13:00:00Z
---

## Description

Bug. `make test-go` fails on a machine with jj 0.43: the Jujutsu fixtures in
`internal/gitops` push their first bookmark with `jj git push --allow-new -b
<name>`, and jj 0.43 rejects `--allow-new` as an unknown argument
(`TestJujutsuCommits`, `TestJujutsuReadsCreateNoOperation` and the jj rows of
the branch and change tables). CI does not see it because the fixtures skip
when no jj binary is installed.

Only the test fixtures are affected; production code in `internal/gitops`
never passes `--allow-new`.

## Acceptance Criteria

- [x] The fixtures push a new bookmark with `--allow-new` on releases that need it and without it on releases that reject it, detected from jj's own answer rather than from a version string.
- [x] `make test-go` passes with jj 0.43 installed and still skips cleanly without jj.
- [x] No change to production code in `internal/gitops`.

## Notes

Found while wiring the Sluice gates for the gintrack hub PoC (GIT-EP-0034).
