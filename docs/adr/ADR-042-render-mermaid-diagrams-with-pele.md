# ADR-042 — Render Mermaid diagrams with Pele instead of mermaid

- **Status:** Accepted
- **Date:** 2026-10-28
- **Phase:** Post-1.0 (Browser-only MVP, `GIT-EP-0002`; story `GIT-US-0202`)
- **Related:** [ADR-009](ADR-009-react-vite-typescript.md) (its Markdown-rendering row and "bundle weight" consequence), docs/05-web-app.md §7, docs/13-design-system.md §11

## Context

Fenced `mermaid` blocks in item bodies and knowledge-base pages are rendered in
the browser, never at parse time: the rehype pipeline leaves a
`<pre class="mermaid" data-mermaid>` holding the source, and the React
`MermaidBlock` renders it on first visibility (GIT-US-0008). Until now that
component dynamically imported `mermaid` 11 and called `mermaid.render` with
`securityLevel: 'strict'` and a `base` theme whose colours were written out as
literals in `mermaid-theme.ts`.

That worked, and cost more than it should:

- **Size.** The first diagram on a page downloaded `mermaid.core` (about 170 KB
  gzip) plus a per-type chunk (sequence about 31 KB, flowchart about 20 KB plus
  dagre and layout chunks). The build emitted dozens of mermaid chunks, among
  them cytoscape (142 KB gzip) and a KaTeX copy (78 KB gzip).
- **Theme.** mermaid bakes colours into the SVG, so the app kept a second copy
  of the design tokens as hex literals (one of the two documented exceptions to
  "a colour in a component is a bug") and re-rendered every diagram on a theme
  switch.
- **Dependencies.** mermaid pulls in `dompurify`, `cytoscape`, `d3`, `dagre` and
  `katex`, and a global `initialize` plus a document-level render id.

[Pele](https://pele.run) (`obsidianmd/pele`, MIT, no runtime dependencies)
renders the same Mermaid syntax (it targets Mermaid 12.1) to an SVG string,
synchronously. A spike on 2026-10-28 rendered 21 of the 23 diagrams under
`docs/`; the two failures were sequence messages containing a `;`, which
mermaid 11 rejects too.

## Decision

**`MermaidBlock` renders with Pele, loaded on demand through `pele/lazy`, with
render options fixed in the component; `mermaid` is removed.**

1. **Same lifecycle.** The rehype placeholder, the sanitiser rules and the
   visibility-gated dynamic import stay. On first visibility the component
   imports `pele/lazy`, whose entry loads each diagram type on demand, and
   calls `renderAsync`. The SVG replaces the `<pre>`.
2. **Pinned exactly.** `pele` is pinned to one version (`1.12.0`) in
   `web/package.json`: the project is young (first release 2026-10-05), and a
   renderer upgrade is a visible change that deserves its own review.
3. **Fixed options, nothing from the source.** Following
   [Pele's security guidance](https://pele.run/security), the options are
   constants in the component: links limited to `http`, `https` and `mailto`
   (the schemes `sanitize.ts` allows on a Markdown `href`) with
   `rel="noopener noreferrer nofollow"`; `images: false`, because an image in a
   diagram would make a network request the asset resolver never sees; a unique
   `idPrefix` per diagram so SVG ids never collide on a page; and no `icons` or
   `math` hook, both of which insert unfiltered markup. Directives and front
   matter in the diagram source configure the diagram, never these options.
   Pele itself escapes label text, emits no HTML labels or scripts, ignores
   `click` callbacks, always blocks `javascript:`, `data:` and `vbscript:` URLs
   and caps input (50,000 characters), output (4,000,000) and edges (5,000).
   This is at least what `securityLevel: 'strict'` gave.
4. **Errors are text.** A Pele error keeps the source in its `<pre>` and shows
   the message in a React text node, never as HTML; the message may quote the
   source.
5. **Theme through CSS variables.** `markdown.css` maps the `--pele-*`
   variables onto the design tokens. The tokens already switch between light
   and dark, so diagrams follow a theme change without being re-rendered, and
   `mermaid-theme.ts` and the `useThemeMode` hook that fed it are deleted. The
   design system has one documented colour exception left (§11).
6. **Doc diagrams fixed.** The two sequence messages with a `;` in
   `docs/04-team-repository.md` and `docs/08-mcp-server.md` are reworded; all 23
   diagrams under `docs/` render.

## Consequences

**Good.**

- A page with one diagram downloads about 17 KB gzip of Pele entry plus the
  diagram type: about 67 KB gzip in total for a flowchart and 58 KB for a
  sequence diagram, against about 170 KB for `mermaid.core` alone before. The
  emitted JavaScript drops from about 1.84 MB to 1.18 MB gzip across all
  chunks; the initial bundle is unchanged.
- `cytoscape`, `d3`, `dagre`, `dompurify` and mermaid's KaTeX copy leave the
  bundle; the dependency tree gets smaller.
- No literal colours for diagrams and no re-render on theme change.

**Bad, and accepted.**

- **Pele's look, not GitHub's.** Pele has its own layout engine and visual
  style, so a diagram does not look like GitHub's (mermaid) rendering of the
  same file, and some layouts differ noticeably.
- **A young dependency.** Pele was first published on 2026-10-05. The exact pin
  and this ADR are the mitigation; an upgrade goes through a story.
- **Syntax gaps are possible.** Pele targets Mermaid's syntax but is a separate
  parser. A diagram it rejects degrades to its readable source with an error
  note, as before.
- **KaTeX stays.** `remark-math` still depends on `katex` 0.16, so its
  low-severity advisory remains until `remark-math` accepts `katex` 0.18.

## Alternatives considered

- **Keep mermaid and upgrade to 12.** Fixes the KaTeX advisory through mermaid
  but keeps the size, the baked colours and the re-render. The size was the
  main motivation.
- **Render diagrams server-side.** Browser-only mode has no server, and
  rendering must work in both modes (ADR-003, ADR-009).
- **`pele` (all types) or `pele/core` with explicit `register` calls.** The
  full entry ships every type to every page with a diagram; explicit
  registration means maintaining the type list by hand. `pele/lazy` loads only
  what a page needs.
- **Pele's `mount()`.** It reads fonts from CSS and redraws when web fonts
  load, but owns the element; `renderAsync` keeps React in charge of the DOM
  and the fallback state.
