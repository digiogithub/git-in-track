---
id: GIT-US-0202
type: story
title: Render Mermaid diagrams with Pele instead of mermaid
status: in_review
priority: medium
parent: GIT-EP-0002
assignees: [claude-code]
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-10-28T21:30:01Z
updated: 2026-10-28T21:42:18Z
started: 2026-10-28T21:30:05Z
---

## Description

Fenced `mermaid` blocks in item bodies and KB pages are rendered client-side by
`web/src/markdown/MermaidBlock.tsx`, which lazy-imports `mermaid` 11. Replace the
renderer with [Pele](https://pele.run) (`obsidianmd/pele`, MIT), a dependency-free
renderer of the same Mermaid syntax (targets Mermaid 12.1) that returns an SVG
string synchronously.

Why (spike on 2026-10-28, 23 diagrams from `docs/`):

- **Size.** Today one diagram downloads `mermaid.core` (~168 KB gz) plus its
  diagram chunk (sequence ~31 KB, flowchart ~19 KB, plus cytoscape/dagre).
  `pele/core` plus one type is ~32 KB gz (sequence) or ~57 KB gz (flowchart).
- **Speed.** Synchronous, sub-millisecond per diagram; no DOM container,
  `initialize` or global id.
- **Theme.** `--pele-*` CSS variables follow theme changes without a
  re-render, replacing `mermaid-theme.ts`'s `themeVariables`.
- **Security.** Text is escaped, no HTML labels, `click` callbacks ignored,
  `javascript:`/`data:` links blocked, input/output/edge limits. Equivalent to
  today's `securityLevel: 'strict'`.
- **Dependencies.** Drops mermaid's `dompurify`, `cytoscape`, `d3`, `dagre`.
- Compatibility: Pele renders 21/23 doc diagrams; the 2 failures (a `;` inside a
  sequence message in docs/04 and docs/08) fail in Mermaid 11 too.

Costs accepted: Pele's own layout and visual style (diagrams will not look like
GitHub's), a project created 2026-10-05 (pin an exact version), and `katex`
stays because `remark-math` still pulls it.

## Acceptance Criteria

- [ ] `MermaidBlock` renders with Pele loaded on demand (`pele/lazy` or an
      equivalent dynamic import), keeping the visibility-gated lazy load and the
      `pre.mermaid` placeholder; `mermaid` is removed from `web/package.json`.
- [ ] `pele` is pinned to an exact version.
- [ ] Theme colours come from `--pele-*` CSS variables mapped to the design
      system tokens, in light and dark, with no re-render on theme change.
- [ ] A syntax error keeps the source visible and shows the error as text,
      never as HTML.
- [ ] Links in diagrams are limited to safe schemes; diagram authors cannot
      pass Pele options.
- [ ] Vitest covers render, error and theme behaviour; `make test`, `make lint`
      and `make wasm` pass.
- [ ] The two doc diagrams with a `;` inside a sequence message are fixed.
- [ ] `docs/05-web-app.md` (§7 "Mermaid") and any other doc naming the renderer
      are updated, an ADR records the switch, and `CHANGELOG.md` has an entry.

## Notes

Original Mermaid rendering came from GIT-US-0008. Security guidance:
https://pele.run/security.
