---
id: GIT-T-0026
type: task
title: Normalise the description Markdown and attachment references
status: todo
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:15:56Z
updated: 2026-09-13T13:15:56Z
---

## Description

Add `internal/youtrack/mapping/markdown.go` with `NormalizeDescription(body, itemID string) (string, []Warning)`. It rewrites attachment image embeds `![alt](file.png)` — which YouTrack resolves against the entity's own attachments, not a URL — to `.pmngr/attachments/<ITEM-ID>/file.png`, leaves bare issue ids such as `ACME-42` untouched because YouTrack auto-links them and re-linking would double-wrap, and handles the YouTrack-specific `{color:…}…{color}` and `{width=…}` extensions explicitly with the chosen behaviour documented in the function comment.

## Acceptance Criteria

- [ ] Attachment image refs are rewritten to the local attachments path; absolute URLs are left alone.
- [ ] Bare issue ids are never wrapped in links.
- [ ] `{color:…}` and `{width=…}` handling is explicit and documented.
- [ ] Golden tests under `internal/youtrack/mapping/testdata/` cover each case; `go test -race ./internal/youtrack/...` passes.
