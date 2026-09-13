---
id: GIT-T-0212
type: task
title: Add the KB sync status vault operation
status: done
priority: medium
parent: GIT-US-0090
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-13T13:21:06Z
updated: 2026-09-13T15:33:22Z
started: 2026-09-13T15:27:05Z
closed: 2026-09-13T15:33:22Z
---

## Description

Add `youtrack.kb.status` to the vault dispatch table taking `{project, path, recursive}` and returning per page `{path, linked, articleId, url, state, syncedAt}` with `state` one of `unlinked`, `in_sync`, `local_ahead`, `remote_ahead`, `conflict`. It reads the remote only when explicitly asked, so a tree view does not fan out into hundreds of requests; otherwise it answers from `external.synced_at` and the local rev alone.

## Acceptance Criteria

- [x] The method exists in the dispatch table and returns the five states per page.
- [x] Without the remote-check flag it makes no network request.
- [x] A recursive call answers for a whole subtree in one response.
- [x] `go test -race ./internal/vault/...` covers every state and the no-network default.

## Notes

Landed in `internal/vault/youtrackkb.go`. The remote check is opt-in through a
`remote` parameter rather than being inferred, and `TestYouTrackKBStatusStaysOfflineByDefault`
counts the article reads to prove the default stays offline.

Change detection is content-based, never byte-based: the comparison is
`mapping.EqualContent`, and the `key` of a page's `external` entry records
`kbFingerprint` — the revision of the content that actually crossed the
boundary. That is what lets the local half decide `local_ahead` with no network
at all, and what keeps a local `## Feedback` note from looking like a remote
edit (ADR-030, R-FB-4; `TestYouTrackKBStatusIgnoresTheFeedbackBlock`). A page
whose entry carries no `key` — published before this, or linked by hand — falls
back to comparing timestamps.
