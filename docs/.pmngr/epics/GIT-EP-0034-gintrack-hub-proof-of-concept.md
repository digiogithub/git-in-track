---
id: GIT-EP-0034
type: epic
title: gintrack hub proof of concept
status: todo
priority: high
author: claude-code
labels: [server, core, mcp, security]
created: 2026-11-08T17:59:20Z
updated: 2026-11-08T17:59:20Z
---

## Description

Proof of concept of the optional self-hosted multi-user server of
[ADR-043](../../adr/ADR-043-gintrack-hub-optional-self-hosted-multi-user-server.md),
with memory items and knowledge-only projects from
[ADR-044](../../adr/ADR-044-memory-items-and-knowledge-only-projects.md).

Scope decided by the maintainer on 2026-11-08: one organisation with one to
three projects cloned from the team's existing forge with a deploy key; opaque
agent tokens; grants per project and scope (`project/`, `org/` only); the
server side of `alchemai.memory.v2`; `pando/rag` as a library; OAuth 2.1 for
MCP so a claude.ai Project can connect; the existing web UI unchanged except
for the inbox listing memory proposals.

Out of scope: an own git server, binary object store, Drive and artifact
connectors, the non-technical UI, path-based views, personal memory, several
organisations.

Human-only areas (the `internal/core` data model, `internal/server` auth) are
implemented under supervision: the concrete design is approved before code.

## Acceptance Criteria

- [ ] Pando with memsync, Claude Code over MCP and a person in the web UI share memory and knowledge of one project through the hub.
- [ ] Every write is committed with the authenticated principal as author.
- [ ] A memory proposed by an agent is reviewed in the inbox before anyone else sees it.
- [ ] `make lint`, `make test` and `make wasm` pass.

## Notes

Design: ADR-043, ADR-044. Agents never push; the maintainer decides push and merge.
