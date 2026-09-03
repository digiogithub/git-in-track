---
id: GIT-US-0025
type: story
title: Guard agent writes with rev-based optimistic locking
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
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

- [ ] `rev` is a deterministic content hash, identical across platforms and modes.
- [ ] Every read path returns a `rev`; every write path requires one.
- [ ] A stale `rev` is rejected with a structured conflict naming the differing fields.
- [ ] The conflict response includes the current `rev` so a retry needs one round trip.
- [ ] Two concurrent writers of the same item yield exactly one success.
- [ ] A concurrency test with two agents claiming one story never loses an update.
- [ ] The web UI surfaces the conflict as a merge prompt, not as a raw error.
- [ ] The locking contract is documented once and referenced from the API and MCP docs.
