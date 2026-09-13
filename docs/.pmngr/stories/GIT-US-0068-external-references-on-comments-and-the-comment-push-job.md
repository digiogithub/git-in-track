---
id: GIT-US-0068
type: story
title: External references on comments and the comment push job kind
status: backlog
priority: medium
parent: GIT-EP-0013
milestone: GIT-M-0011
author: mcp
labels: [core, server, docs]
estimate: 8
created: 2026-09-13T13:13:20Z
updated: 2026-09-13T13:13:20Z
---

## Description

As a team publishing decisions back to YouTrack, I want a comment to record the YouTrack comment it became, so that the same comment is never posted twice and a later edit updates the remote instead of duplicating it.

Extend `core.Comment` (`internal/core/model.go:377-400`) with the `external` field agreed for items in GIT-EP-0011, and teach `ParseComment` (`internal/core/frontmatter.go:289`) and `SerializeComment` (`:404`) about it, with the same canonical key order and the same `[{system, id, url, key?, synced_at?}]` shape. Unknown comment keys already round-trip through `Comment.Extra`, but a first-class field is what makes the push idempotent and queryable, and `docs/03-data-model.md` §11 plus the JSON schema must be updated in the same change.

Then register a `youtrack.comment.push` job kind with the sync engine. Its payload is `{project, itemId, commentPath}`; the handler reads the comment, resolves the item's `external` YouTrack id, renders the body as Markdown with a trailing attribution line from a configurable template (defaults to the author and the gintrack item id), and posts `POST /api/issues/{id}/comments` with `{text}`. On success it writes the returned comment id back into the comment's `external` block through the vault, quoting the comment's rev. When `external` already holds a YouTrack id the handler edits instead: `POST /api/issues/{id}/comments/{cid}`. A locally deleted comment is never deleted remotely — the job is simply not enqueued, and the divergence is documented.

## Acceptance Criteria

- [ ] `core.Comment` carries `external`; parser, serializer, key order, JSON schema and `docs/03-data-model.md` §11 are updated together.
- [ ] A `youtrack.comment.push` job kind is registered with `internal/syncengine` and enqueued with `{project, itemId, commentPath}`.
- [ ] First push creates the remote comment and writes its id back into `external` quoting the comment's rev.
- [ ] A push for a comment that already has an `external` YouTrack id edits the remote comment rather than creating a new one.
- [ ] The attribution line is rendered from a template configurable per project, and the item id is resolvable from it.
- [ ] Deleting a comment locally never deletes it in YouTrack; the behaviour is documented.
- [ ] An item without a YouTrack `external` reference fails the job with a clear, non-retryable error.
- [ ] `go test -race ./internal/core/... ./internal/server/...` covers round-trip serialization, create, edit and the missing-link failure.

## Notes

Depends on GIT-EP-0011 for the `external` field on items and for the client, and on GIT-EP-0015 for the engine, the rate limiter and retry. A data-model change needs an ADR per AGENTS.md; GIT-EP-0011's ADR-031 should be extended rather than duplicated.

Endpoints: create `POST /api/issues/{id}/comments?fields=id,text,created,author(login)` with `{"text": "..."}`; edit `POST /api/issues/{id}/comments/{cid}` (scratchpad YouTrack report §3.6). YouTrack auto-links bare issue ids in comment text, so the attribution line must not wrap the item id in a link.

Do NOT push inline from the write path — it would block the write. Do NOT mirror YouTrack comments back into the repository in this epic; import is GIT-EP-0012's concern and there is no bidirectional sync this phase.
