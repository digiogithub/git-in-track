# ADR-009 — React 18 + Vite + TypeScript as the front-end stack

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 1 (Browser-only MVP)
- **Related:** [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-005](ADR-005-companion-cli-go-embed.md)

## Context

The web app is the product for most users. It has to render a knowledge base, edit
Markdown, drive drag-and-drop boards, stream progress from a WASM worker, and behave
identically whether its data comes from that worker or from a local REST/WS API
([ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-005](ADR-005-companion-cli-go-embed.md)).

It also has to be built as a static bundle that can be embedded in a Go binary with
`go:embed`, so no server-side rendering runtime is available or wanted.

Selection criteria, in order: availability of mature libraries for the hard parts
(rich Markdown editing, accessible drag-and-drop, accessible primitives), the size of
the contributor pool for an open-source project, build output suitable for embedding,
and TypeScript quality of the ecosystem.

## Decision

**React 18 + Vite + TypeScript (strict), with a fixed set of libraries chosen once
and documented here.**

| Concern | Choice | Why |
|---------|--------|-----|
| Framework | React 18 | Largest contributor pool; the libraries we need for editing, dnd and primitives target React first |
| Build | Vite | Fast dev server, first-class WASM and Web Worker handling, static output for `go:embed` |
| Language | TypeScript, `strict: true` | The bridge protocol and the model are typed end to end |
| Routing | TanStack Router | Type-safe routes and search params; the board and KB rely on URL state |
| Server state | TanStack Query | Caching, invalidation and background refresh; targeted invalidation on `items.changed` events |
| Client state | Zustand | Small, unopinionated, no boilerplate for editor buffers and UI state |
| Styling | Tailwind CSS | Consistent spacing and theming without a bespoke design system |
| Components | shadcn/ui on Radix | Accessible dialogs, menus, popovers; source-in-repo so we can adapt them |
| Editor | CodeMirror 6 | Markdown editing with real accessibility, small bundle, extensible |
| Markdown rendering | unified / remark / rehype (`remark-gfm`, `remark-frontmatter`, `remark-wiki-link`, `rehype-sanitize`, Mermaid) | Composable pipeline; sanitisation is a security requirement |
| Drag and drop | dnd-kit | Keyboard-operable with live-region announcements — a hard accessibility requirement for boards |
| Tests | Vitest + Testing Library + Playwright | Unit, component and cross-mode end-to-end coverage |

Additional rules:

- **No model logic in TypeScript.** Parsing, validation, ID allocation and querying
  are core operations, always. TypeScript types for the model are generated from the
  Go definitions so they cannot drift.
- **All data access goes through the `DataSource` interface.** Feature code never
  knows whether it is talking to WASM or to the companion.
- **No UI string literals in components.** Copy lives in a typed catalogue with
  stable keys (see the i18n section of the architecture document).
- **No server-side rendering, no meta-framework.** The output is a static bundle.

## Consequences

**Positive**

- Every hard part of the UI has a mature, actively maintained, accessible library —
  we implement product, not primitives.
- The largest possible pool of potential contributors for an open-source tool.
- Vite's WASM and worker support makes the bridge a configuration detail rather than a
  build project, and its static output drops straight into `go:embed`.
- Strict TypeScript across a generated model plus a typed bridge catches whole classes
  of protocol errors at compile time.

**Negative**

- **Bundle weight.** React plus the editor, Mermaid and the Markdown pipeline is
  substantial *before* the ~3 MB WASM core. Route-level code splitting and lazy
  loading of Mermaid and CodeMirror are mandatory, not optional.
- **Dependency surface.** Many packages, therefore supply-chain risk and upgrade
  work. Mitigated by pinned versions, a lockfile, Dependabot and CI audit.
- **React's concurrent rendering can surprise.** Streaming worker progress and
  high-frequency WS events need throttling and stable keys to avoid render storms.
- **Ecosystem churn.** The stack will look dated in three years; the `DataSource`
  boundary is what would make a future replacement tractable.
- **Contributors need both Go and TypeScript** to work across the seam, though each
  side is independently approachable.

## Alternatives considered

- **Svelte or SolidJS.** Smaller bundles, excellent ergonomics, less runtime overhead.
  Rejected because the specific libraries we depend on for accessible drag-and-drop
  and accessible component primitives are weaker or absent, and the contributor pool
  is smaller for an open-source project that needs outside help.
- **Vue 3.** A strong all-round option with a large community; rejected mainly on
  ecosystem depth for our exact needs (shadcn/Radix equivalents, dnd-kit equivalents)
  and on the balance of contributor familiarity in the target audience.
- **Next.js or Remix.** Meta-frameworks optimised for server rendering and hosted
  deployment — precisely what a local-first, statically embedded app does not need.
  They would add a runtime and a build model we would fight. Rejected.
- **Plain TypeScript with web components.** Minimal dependencies and maximum
  longevity, at the cost of building routing, state, editing and accessible dnd
  ourselves. Rejected on delivery risk.
- **A Go-based UI framework compiled to WASM (go-app, Vugu).** Would unify the
  language, but sacrifices the entire mature editor/dnd/Markdown ecosystem and the
  contributor pool, and makes the WASM bundle carry the UI too. Rejected.
