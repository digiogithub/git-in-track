---
id: GIT-US-0215
type: story
title: OAuth 2.1 authorization for the hub MCP endpoint
status: todo
priority: high
parent: GIT-EP-0034
author: claude-code
labels: [server, security, mcp, agent-ok]
estimate: 8
created: 2026-11-08T17:59:20Z
updated: 2026-11-08T17:59:20Z
links:
  - { kind: blocked_by, target: GIT-US-0208 }
---

## Description

Make the hub an OAuth 2.1 authorization server for `/mcp` as the MCP specification describes (protected-resource metadata, dynamic client registration, PKCE, refresh, revocation), with Google OIDC as the upstream identity provider, so a claude.ai Project can connect. Human-only area: design approved before code.

## Acceptance Criteria

- [ ] Design approved by the supervisor before implementation.
- [ ] A claude.ai custom connector completes the flow and lists the hub tools.
- [ ] Tokens map to the same `Principal` and grants as agent tokens.
- [ ] Revoked or expired tokens are refused; secrets are never logged or committed.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
