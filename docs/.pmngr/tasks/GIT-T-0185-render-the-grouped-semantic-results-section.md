---
id: GIT-T-0185
type: task
title: Render the grouped semantic results section
status: todo
priority: medium
parent: GIT-US-0086
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:19:43Z
updated: 2026-09-13T13:19:43Z
---

## Description

Update `web/src/features/workspace/WorkspaceSearch.tsx` to render results in two labelled groups, exact matches first and a "Related by meaning" section below, shown only when `capabilities.fullTextSearch` is `'pando'`. Each semantic row shows the title, the snippet and a subdued relevance indicator. Keep the existing debounce and two-character minimum; do not introduce a command palette or a new UI primitive.

## Acceptance Criteria

- [ ] The two groups render with headings and correct ordering.
- [ ] The semantic group is absent entirely when the capability is not `'pando'`, leaving no empty heading.
- [ ] The existing debounce and minimum-length behaviour is unchanged.
- [ ] Vitest covers both capability states and the ordering.
