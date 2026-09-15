---
id: GIT-T-0053
type: task
title: Build the tool-call card and collapsed reasoning block
status: done
priority: medium
parent: GIT-US-0057
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:16:39Z
updated: 2026-09-15T16:43:01Z
started: 2026-09-15T15:45:47Z
closed: 2026-09-15T16:43:01Z
---

## Description

Add `ToolCallCard.tsx` rendering a tool call as a card over `web/src/components/ui/card.tsx` with the tool name, a status indicator (running, done, failed) and a collapsible body holding pretty-printed arguments and the result. Add a matching collapsed reasoning block. The UI kit has no `Accordion` or `Collapsible` primitive, so build the disclosure from a native `<details>` element or a small local component styled with design tokens — do not add a Radix package for it.

## Acceptance Criteria

- [ ] A tool call renders with name, status and a collapsed body that expands on click and by keyboard.
- [ ] Arguments and results are shown as escaped, pretty-printed text; a huge result is clamped with an expand control.
- [ ] Reasoning blocks render collapsed by default.
- [ ] `npm run tokens:check` passes and Vitest covers the three states.
