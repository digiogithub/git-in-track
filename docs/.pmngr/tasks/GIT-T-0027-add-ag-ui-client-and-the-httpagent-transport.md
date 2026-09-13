---
id: GIT-T-0027
type: task
title: Add @ag-ui/client and the HttpAgent transport
status: todo
priority: medium
parent: GIT-US-0053
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:15:57Z
updated: 2026-09-13T13:15:57Z
---

## Description

Add `@ag-ui/client` to `web/package.json` at a pinned version and create `web/src/features/agent/client.ts` constructing an `HttpAgent` whose URL is the companion's `/api/v1/agent/run` and whose headers carry the companion bearer token from `web/src/api/token.ts:133-136`. Expose a `createAgentClient({baseUrl, token, agent})` factory plus an abort handle, so callers never touch the library directly. Report the bundle-size delta in the PR, as AGENTS.md requires for any new dependency.

## Acceptance Criteria

- [ ] The dependency is pinned and `npm run build` succeeds with the size delta recorded in the PR description.
- [ ] The factory produces a client that POSTs to the companion route with the bearer header and can be aborted.
- [ ] Vitest covers construction, header injection and abort against a mocked fetch.
