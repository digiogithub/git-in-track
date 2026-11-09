---
id: GIT-US-0208
type: story
title: Authenticated principal on every hub write, recorded as the commit author
status: done
priority: high
parent: GIT-EP-0034
author: claude-code
assignees: [claude-code]
labels: [server, security, git, agent-ok]
estimate: 5
created: 2026-11-08T17:59:20Z
updated: 2026-11-09T13:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0207 }
---

## Description

Replace the single bearer token with an `Authenticator` that resolves opaque per-identity agent tokens (hashes in a YAML file) and per-person tokens to a `Principal` carried in `context.Context` (ADR-043 §3). `gitops.Change` gains the author; the Committer passes it as `CommitRequest.Author` (`internal/gitops/committer.go:335`) and includes it in the coalescing key (`committer.go:201`); `Agent:` and `On-Behalf-Of:` trailers come from the principal. Human-only area: the concrete design is approved before code.

## Acceptance Criteria

- [x] Design (types, signatures, token file format) approved by the supervisor before implementation.
- [x] A write through REST or MCP commits with the principal's name and email as author; an agent write carries `Agent:`.
- [x] Two principals editing the same item within the debounce window produce two commits.
- [x] Token hashes only on disk; a wrong or revoked token is 401 and never reaches the vault.
- [x] The companion's behaviour is unchanged (no principal → today's git identity).
- [x] Table-driven tests in `internal/gitops` and the hub auth package.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
