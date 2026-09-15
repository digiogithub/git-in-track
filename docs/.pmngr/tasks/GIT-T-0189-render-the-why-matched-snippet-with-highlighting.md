---
id: GIT-T-0189
type: task
title: Render the why-matched snippet with highlighting
status: in_review
priority: medium
parent: GIT-US-0086
milestone: GIT-M-0013
author: mcp
labels: [web, security]
estimate: 2
created: 2026-09-13T13:19:50Z
updated: 2026-09-15T15:50:49Z
started: 2026-09-15T15:50:37Z
---

## Description

Add the snippet renderer: show the chunk Pando returned as escaped text with the query terms that do appear in it highlighted, clamped to a few lines with an accessible expand control. Snippets are repository content, so they are escaped or passed through the sanitising Markdown pipeline — never injected as HTML, and never rendered with `dangerouslySetInnerHTML` around a regex-built string.

## Acceptance Criteria

- [ ] Matching terms are highlighted and non-matching snippets still render cleanly.
- [ ] HTML inside a snippet is displayed as text, not executed.
- [ ] Long snippets clamp with a keyboard-reachable expand control.
- [ ] Vitest covers highlighting, escaping and the expand control.
