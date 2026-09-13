---
id: GIT-T-0211
type: task
title: Detect both-changed conflicts and write a conflict page
status: done
priority: medium
parent: GIT-US-0087
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:21:01Z
updated: 2026-09-13T16:01:58Z
started: 2026-09-13T16:01:08Z
closed: 2026-09-13T16:01:58Z
---

## Description

Add conflict detection comparing the page's `external.synced_at` and recorded rev against the current local rev and the article's `updated`. One side changed means last writer wins in that direction; both changed means the incoming content is written to `<page>.conflict.md`, the original page is left untouched, and a `youtrack.kb.conflict` event is published on the hub. Feedback-block pruning must not count as a local change.

## Acceptance Criteria

- [x] The four cases (neither, local only, remote only, both) are decided from `synced_at`, the local rev and the article `updated`.
- [x] Both-changed writes `<page>.conflict.md`, leaves the page untouched and publishes `youtrack.kb.conflict`.
- [x] Feedback pruning does not register as a local change.
- [x] `go test -race ./internal/server/...` covers all four cases.

## Notes

The pivot is the **fingerprint** recorded in the page's `external.key`, not the
revs: it was taken over the content that was published, so both sides are
compared against it rather than against each other, which is the only way
"both changed" is distinguishable from "one changed". `synced_at` and the
article's `updated` are the fallback for a page published by an older build or
linked by hand, where there is no fingerprint to compare.

The fingerprint is taken over `mapping.PageToArticle`'s output and matches
`internal/vault`'s own `kbFingerprint`, so the status call and the jobs agree
about whether a page has moved. That is also what makes feedback pruning
invisible to the comparison: the block is stripped before the fingerprint is
taken, and `mapping.EqualContent` — never a byte comparison — decides whether a
write is needed at all.

Detection runs in **both** directions. A publish that finds both sides moved
writes the conflict page and does **not** update the article, exactly as a pull
does not write the page; a one-sided change wins in its own direction and is a
no-op in the other. The conflict page carries `conflict_of: <page>` in its front
matter so a reviewer can see which page it belongs to without reading the event.
