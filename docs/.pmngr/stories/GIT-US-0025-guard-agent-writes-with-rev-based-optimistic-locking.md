---
id: GIT-US-0025
type: story
title: Guard agent writes with rev-based optimistic locking
status: in_review
created: 2026-09-03T00:00:00Z
updated: 2026-09-04
author: team
priority: critical
parent: GIT-EP-0006
milestone: GIT-M-0006
estimate: 5
labels: [mcp, core, server]
links:
  - kind: blocked_by
    target: GIT-US-0024
---

## Description

As a team with humans and several agents writing at once, I want concurrent updates to fail
loudly instead of overwriting each other, so that no one's work is silently lost.

Every read returns a `rev` computed as a content hash. Every write carries the `rev` it was
based on; if the file has changed since, the write is rejected with a structured conflict
that includes the current `rev` and the fields that differ, so the caller can re-read and
retry deliberately rather than blindly.

This applies uniformly to the REST API, the MCP tools and the web UI — there is one locking
model, not three. It is also the mechanism that keeps two agents from both claiming the same
story.

## Acceptance Criteria

- [x] `rev` is a deterministic content hash, identical across platforms and modes.
- [x] Every read path returns a `rev`; every write path requires one.
- [x] A stale `rev` is rejected with a structured conflict naming the differing fields.
- [x] The conflict response includes the current `rev` so a retry needs one round trip.
- [x] Two concurrent writers of the same item yield exactly one success.
- [x] A concurrency test with two agents claiming one story never loses an update.
- [x] The web UI surfaces the conflict as a merge prompt, not as a raw error.
- [x] The locking contract is documented once and referenced from the API and MCP docs.

## Notes

- The contract is defined once in `docs/03-data-model.md` section 5 (R-REV-1 … R-REV-6);
  `docs/07-cli-and-api.md` section 5.3 and `docs/08-mcp-server.md` section 3.5 describe how HTTP
  and MCP spell it and add no rule of their own.
- A refused write reports `currentRev` plus `conflicts[]`, the fields it would **still** have
  changed against the content on disk now. It is not a diff against the caller's base version:
  the base is a hash, not a document, so no reader holds it. An empty list means the change had
  already been made by whoever won the race.
- `update_item` used to make two writes when a call changed fields *and* status, and the second
  quoted the rev the first produced rather than the caller's. `core.FileStore.Update` now
  validates a status change against the project workflow inside the same conditional write, so
  the tool dispatches `item.update` once and there is no window left (R-REV-3c).
- The web UI already surfaces `stale_revision` as a merge prompt in the item editor
  (`ConflictDialog`) and as a reload prompt on the status control; this change kept both and
  gave them a richer payload to work with.
