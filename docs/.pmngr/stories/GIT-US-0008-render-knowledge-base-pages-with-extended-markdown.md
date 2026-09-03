---
id: GIT-US-0008
type: story
title: Render knowledge base pages with extended Markdown
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: critical
parent: GIT-EP-0002
milestone: GIT-M-0002
estimate: 8
labels: [web]
links:
  - kind: blocked_by
    target: GIT-US-0006
---

## Description

As a team member, I want the documentation folder rendered as a readable, navigable
knowledge base, so that our Markdown docs become a real wiki without leaving the
repository.

The renderer is a unified/remark/rehype pipeline supporting GFM (tables, strikethrough,
autolinks), task lists, footnotes, callouts/admonitions, wikilinks `[[Page]]` with
resolution against the vault index, Mermaid diagrams, and optional math. It produces a file
tree, a per-page outline, and a backlinks panel derived from the link graph.

Repository content is untrusted input: HTML is sanitised, `javascript:` and `data:` URLs
are stripped, and external links get `rel="noopener noreferrer"`.

## Acceptance Criteria

- [ ] GFM tables, task lists, footnotes and callouts render correctly.
- [ ] `[[Page]]` and `[[Page|alias]]` resolve to vault pages; unresolved links are marked.
- [ ] Mermaid diagrams render client-side and fail gracefully on a syntax error.
- [ ] Relative links and images inside the vault resolve through the directory handles.
- [ ] A page outline and a backlinks panel are generated from the link graph.
- [ ] Output is sanitised: no raw `<script>`, no `javascript:`/`data:` URLs.
- [ ] Rendering is covered by golden tests against fixture pages.
- [ ] Code blocks are syntax-highlighted and long content scrolls without breaking layout.
- [ ] The viewer is keyboard navigable and passes an accessibility check.
