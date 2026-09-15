---
id: GIT-T-0027
type: task
title: Add @pando-ai/sdk/agui and the PandoAguiClient transport
status: done
priority: medium
parent: GIT-US-0053
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:15:57Z
updated: 2026-09-15T16:43:48Z
started: 2026-09-15T15:19:21Z
closed: 2026-09-15T16:43:48Z
---

## Description

Add `@pando-ai/sdk` to `web/package.json` pinned at `0.2.0` and create `web/src/features/agent/client.ts` constructing a `PandoAguiClient` (import path `@pando-ai/sdk/agui/client`, the browser-safe entry shipped by PANDO-US-0006) whose base URL is the companion's `/api/v1/agent` and whose headers carry the companion bearer token from `web/src/api/token.ts`. Expose a `createAgentClient({baseUrl, token, agent})` factory plus an abort handle, so callers never touch the library directly. Report the bundle-size delta in the PR, as AGENTS.md requires for any new dependency.

Decision of 2026-09-13: `@ag-ui/client` is out (its schemas drop `outcome: interrupt` and the reasoning events). The SDK's `PandoThread`, `applyJsonPatch` and the HITL helpers (`approve`, `deny`, `answerQuestion`, `cancelQuestion`, `isPermissionRequest`, `isQuestionRequest`) are the reducer and answer builders the sibling tasks build on; do not write a parallel one.

## Acceptance Criteria

- [ ] The dependency is pinned and `npm run build` succeeds with no Node polyfill and no bundler warning, with the size delta recorded in the PR description.
- [ ] The factory produces a client that POSTs to the companion route with the bearer header and can be aborted.
- [ ] Vitest covers construction, header injection and abort against a mocked fetch.

## Notes

Published 2026-09-15: `@pando-ai/sdk@0.2.0` on npmjs (org `pando-ai`), source `github.com/madeindigio/pando-typescript-sdk` tag `v0.2.0`. Verified by a clean `npm install` and a dynamic import of `@pando-ai/sdk/agui/client` exporting `PandoAguiClient`, `PandoThread`, `parseSSE`, `applyJsonPatch` and the HITL helpers.
