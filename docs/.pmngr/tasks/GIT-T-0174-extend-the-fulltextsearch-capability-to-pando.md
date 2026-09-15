---
id: GIT-T-0174
type: task
title: Extend the fullTextSearch capability to pando
status: done
priority: medium
parent: GIT-US-0082
milestone: GIT-M-0013
author: mcp
labels: [server, web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:30Z
updated: 2026-09-15T16:59:59Z
started: 2026-09-15T16:04:08Z
closed: 2026-09-15T16:59:59Z
---

## Description

Report `features.search` as `"pando"` in `handleCapabilities` (`internal/server/server.go:411-435`) when the Pando backend is actually selected. On the web side widen `Capabilities.fullTextSearch` to `'core' | 'bleve' | 'pando'` (`web/src/api/provider.ts:689`), extend the mapping in `companion-provider.ts:761` from its current two-value ternary, keep `browser-provider.ts` reporting `'core'`, and update the defaults and the settings page capability row. Extend the existing enum rather than adding a parallel flag.

## Acceptance Criteria

- [ ] `features.search` is `"pando"` only when that backend is selected and `"core"` otherwise.
- [ ] All four providers and the settings capability row handle the third value.
- [ ] `go test -race ./internal/server/...` and Vitest cover both values of the mapping.
