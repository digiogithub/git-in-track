---
id: GIT-US-0213
type: story
title: Server side of alchemai.memory.v2 on the hub
status: todo
priority: high
parent: GIT-EP-0034
author: claude-code
labels: [server, agent-ok]
estimate: 8
created: 2026-11-08T17:59:20Z
updated: 2026-11-08T17:59:20Z
links:
  - { kind: blocked_by, target: GIT-US-0209 }
  - { kind: blocked_by, target: GIT-US-0211 }
---

## Description

Implement `POST /v1/memory/batch`, `GET /v1/memory/search`, `GET /v1/memory/changes`, `POST /v1/memory/enrich` and the additive extensions (`/capabilities`, proposals, `base_version` → `stale_revision`) over the vault memory methods (ADR-043 §6). Scopes `project/<key>` and `org/<team>` only; `user/*` is `scope_unknown`, `private/*` is `private_scope`. Uses the public `alchemai-sdk/wire` module — blocked until it is published; never copy the types.

## Acceptance Criteria

- [ ] memsync from alchemai-agent pushes, pulls and searches against the hub with no client change.
- [ ] Per-item rejections return HTTP 200 with the reasons of the protocol.
- [ ] Proposals count as accepted but do not appear in search or changes until accepted.
- [ ] Documents whose path lies inside a hub project checkout count as duplicates.
- [ ] Responses always carry `schema_version: alchemai.memory.v2`.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
