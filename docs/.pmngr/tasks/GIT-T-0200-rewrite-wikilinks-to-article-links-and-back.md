---
id: GIT-T-0200
type: task
title: Rewrite wikilinks to article links and back
status: todo
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:20:22Z
updated: 2026-09-13T13:20:22Z
---

## Description

Add the link rewriter: going up, resolve each `[[wikilink]]` to the target page's YouTrack article reference (using `core.ParseWikilink`, `internal/core/kb.go:206`) and emit an article link; an unresolvable target degrades to plain text with a warning rather than a dead link. Coming down, article links pointing at pages we know are converted back to wikilinks, and everything else is left as is.

## Acceptance Criteria

- [ ] Resolvable wikilinks become article links; unresolvable ones degrade to text with a warning.
- [ ] Known article links convert back to wikilinks and unknown ones are left untouched.
- [ ] A round trip of a page with both kinds of link is stable.
- [ ] Golden tests cover all four cases; `go test -race ./internal/youtrack/...` passes.
