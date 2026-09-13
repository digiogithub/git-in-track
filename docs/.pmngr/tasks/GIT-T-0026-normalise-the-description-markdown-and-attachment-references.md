---
id: GIT-T-0026
type: task
title: Normalise the description Markdown and attachment references
status: done
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:15:56Z
updated: 2026-09-13T14:34:31Z
started: 2026-09-13T14:34:04Z
closed: 2026-09-13T14:34:31Z
---

## Description

Add `internal/youtrack/mapping/markdown.go` with `NormalizeDescription(body, itemID string) (string, []Warning)`. It rewrites attachment image embeds `![alt](file.png)` — which YouTrack resolves against the entity's own attachments, not a URL — to `.pmngr/attachments/<ITEM-ID>/file.png`, leaves bare issue ids such as `ACME-42` untouched because YouTrack auto-links them and re-linking would double-wrap, and handles the YouTrack-specific `{color:…}…{color}` and `{width=…}` extensions explicitly with the chosen behaviour documented in the function comment.

## Acceptance Criteria

- [x] Attachment image refs are rewritten to the local attachments path; absolute URLs are left alone.
- [x] Bare issue ids are never wrapped in links.
- [x] `{color:…}` and `{width=…}` handling is explicit and documented.
- [x] Golden tests under `internal/youtrack/mapping/testdata/` cover each case; `go test -race ./internal/youtrack/...` passes.

## Notes

Landed as `markdown.go`. Chosen behavior, documented on the function: `{color:…}…{color}` loses both markers and keeps the text (losing a color is acceptable, losing the sentence is not); `{width=…}` is removed; each is reported once per body rather than once per occurrence, so a long description does not flood the import report. Only a **bare** file name is rewritten — a target with a scheme, a protocol-relative `//host`, a `data:` payload or any path separator is left alone, and a plain link is never touched. Fenced code blocks and inline code spans are exempt from all three rules, so a body documenting the `{color:…}` syntax survives the round trip. With an empty `itemID` nothing is rewritten and a warning per embed says why. The prefix is `.pmngr/attachments` by default and overridable through `Options.AttachmentPrefix`.
