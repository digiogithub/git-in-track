---
id: GIT-US-0057
type: story
title: Chat route and message UI in the shadcn design system
status: in_review
priority: high
parent: GIT-EP-0018
milestone: GIT-M-0013
author: mcp
labels: [web, docs]
estimate: 8
created: 2026-09-13T13:12:23Z
updated: 2026-09-15T16:00:48Z
started: 2026-09-15T15:45:56Z
---

## Description

As a user of the companion, I want an `/agent` page where I can hold a conversation about my backlog, so that I can ask questions in natural language without leaving git-in-track.

Add one `createRoute` for `/agent` to `web/src/app/router.tsx` plus the matching entry in the tree array and one sidebar item in `web/src/app/layout/AppShell.tsx:38-53`, gated on `capabilities.agent` so browser-only mode never shows it (`AppShell.tsx:442-445` is the pattern: branch on capabilities, never on provider kind). Build the view in `web/src/features/agent/` over the store from the AG-UI client story: `AgentPage.tsx` (three-column shell), `MessageList.tsx`, `MessageBubble.tsx`, `ToolCallCard.tsx`, `Composer.tsx`.

Assistant text renders through the existing Markdown pipeline (`web/src/markdown/`, public surface `index.ts` only) so sanitisation still runs last — agent output is untrusted content exactly like repository text. Streaming text appends without re-parsing the whole document on every delta. Tool calls render as collapsible cards showing name, pretty-printed arguments and result; reasoning blocks are collapsed by default. The composer has send, a stop button bound to run cancellation, and Enter/Shift+Enter handling. A thread list persists `{id, title, updatedAt}` in `localStorage` under `gintrack:agent:threads` — derived data only, matching `web/src/app/ui-prefs.ts`.

## Acceptance Criteria

- [ ] `/agent` renders in companion mode and is absent from the router and the sidebar when `capabilities.agent` is false.
- [ ] Assistant messages render Markdown through `web/src/markdown/` with sanitisation intact; raw HTML in agent output is not executed.
- [ ] Text streams in visibly and the list auto-scrolls only while the user is at the bottom.
- [ ] Tool calls appear as collapsible cards with arguments and results; reasoning is collapsed by default.
- [ ] Stop cancels the in-flight run and the UI settles in a cancelled state with the partial message kept.
- [ ] Switching threads restores that thread's messages; the thread list survives a reload.
- [ ] Only components from `web/src/components/ui/` and design tokens are used — `npm run tokens:check` passes.
- [ ] The page is keyboard navigable and the message list has an appropriate live region.
- [ ] Vitest covers streaming render, stop, thread switching and the capability gate.

## Notes

Design-system constraints: `web/src/components/ui/` has 18 primitives and **no** `Popover`, `DropdownMenu`, `Sheet`, `Tabs` or `Accordion` (report §4.12) — build collapsibles from `<details>` or a small local component rather than adding Radix packages. Tailwind colours must be `hsl(var(--token))` aliases; `web/scripts/check-design-tokens.mjs` enforces it.

Useful visual reference only: Pando's `web-ui/src/components/chat/MessageBubble.tsx` (870 lines, markdown + tool calls + diffs). It is React 19, Tailwind 4, FontAwesome and `react-i18next`, and speaks Pando's own session model rather than AG-UI — read it, do not copy it in.

There is no i18n in this app yet (report §4.11); write hardcoded English strings like the rest of `web/src`.
