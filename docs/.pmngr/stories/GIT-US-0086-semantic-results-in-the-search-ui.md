---
id: GIT-US-0086
type: story
title: Semantic results in the search UI
status: done
priority: medium
parent: GIT-EP-0019
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 5
created: 2026-09-13T13:14:38Z
updated: 2026-09-15T16:44:53Z
started: 2026-09-15T15:50:45Z
closed: 2026-09-15T16:44:53Z
---

## Description

As a user, I want semantic matches shown as their own labelled section with the passage that matched, so that I can tell a guess apart from an exact hit and judge it without opening the item.

Extend `web/src/features/workspace/WorkspaceSearch.tsx` (a debounced panel with `useDeferredValue` and a two-character minimum, `:11-14`) so results render in two groups: exact matches first, then a "Related by meaning" section when the companion reports `fullTextSearch: 'pando'`. Each semantic hit shows the item or page title, a "why matched" snippet — the chunk Pando returned, with the query terms it does contain highlighted — and a subdued relevance indicator. A toggle lets the user turn the semantic section off; the preference lives in `web/src/app/ui-prefs.ts` with the other per-user layout settings.

The data comes through the provider seam, never a direct fetch: widen `SearchHit` and the `search(query)` method on `DataProvider` (`web/src/api/provider.ts:940`) with an origin discriminator and an optional snippet, implement it in `companion-provider.ts`, return only exact hits from `browser-provider.ts`, and cover both shapes in `fake-provider.ts`. Snippets are repository content and agent-adjacent output, so they render as escaped text or through the sanitising Markdown pipeline — never as raw HTML.

## Acceptance Criteria

- [ ] Exact and semantic results render as distinct, labelled groups, exact first.
- [ ] Each semantic hit shows a snippet and a relevance indicator; matched terms inside the snippet are highlighted.
- [ ] The semantic section is absent entirely when `fullTextSearch` is not `'pando'`, with no empty heading left behind.
- [ ] The toggle hides and restores the semantic section and its state survives a reload.
- [ ] `SearchHit` carries the origin and optional snippet, and all four providers compile against the widened type.
- [ ] Snippet text is escaped or sanitised; raw HTML in a snippet is not executed.
- [ ] Long snippets are clamped with an accessible expand control rather than overflowing the panel.
- [ ] Vitest covers grouping, the capability gate, the toggle and snippet escaping.

## Notes

`WorkspaceSearch.tsx` is a search panel, not a command palette, and the app has no `cmdk`, no shadcn `Command` and no `Popover` (report §4.8, §4.12) — do not introduce one here. The nearest reusable interaction pattern is the hand-rolled ARIA combobox in `web/src/components/editor/ItemPicker.tsx:29-163`.

Feature code must not call `fetch('/api')` directly and must not import the WASM bridge (`web/src/api/provider.ts:6`, `docs/05-web-app.md` §4). Every new backend capability is a `DataProvider` method implemented in all four providers.

Strings are hardcoded English: there is no i18n in this app yet.
