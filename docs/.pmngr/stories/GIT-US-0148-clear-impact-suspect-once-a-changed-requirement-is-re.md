---
id: GIT-US-0148
type: story
title: Clear impact suspect once a changed requirement is re-verified at head
status: in_review
priority: high
parent: GIT-EP-0028
milestone: GIT-M-0015
author: mcp
labels: [core, ci, agent-ok]
estimate: 3
created: 2026-09-24T21:19:05Z
updated: 2026-09-24T22:23:26Z
---

## Description

With `--fail-on suspect`, every PR that edits traced code of a passing requirement trips the CI gate. Neither a MODIFIED Spec Delta nor a fresh stamp clears it, because `internal/impact/impact.go` marks a passing requirement `suspect` whenever the diff touches its traced code. The gate would therefore block every PR that touches specified behaviour. That defeats the point of the gate: "suspect" should mean *changed and not re-verified*.

Found in GIT-US-0133 (PR #54). The limitation is recorded in docs/09.

## Acceptance Criteria

- [ ] A requirement touched by the diff is **not** suspect when any of these holds:
  - its linked tests all passed in results ingested at the diff's head (same commit, or the working tree when head is the worktree);
  - its `verified.commit` is the head commit and `verified.rev` matches the block rev;
  - the diff carries a MODIFIED Spec Delta for it and its linked tests pass at head.
- [ ] It stays suspect when its tests were not re-run at head, or when they fail (failing wins).
- [ ] `coverage.list` and `impact` agree on these rules, as documented in docs/03 §21.6 and §21.11.
- [ ] Golden and table tests cover each case. `make spec-check` on a fixture PR that edits traced code and re-runs the tests passes the gate.
- [ ] The known limitation is removed from docs/09.

## Notes

The rules must stay deterministic: tiers 1 and 2 give the same result for the same diff and the same ingested results.
