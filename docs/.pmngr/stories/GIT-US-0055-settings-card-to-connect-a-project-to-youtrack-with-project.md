---
id: GIT-US-0055
type: story
title: Settings card to connect a project to YouTrack, with project autosuggest
status: done
priority: medium
parent: GIT-EP-0011
milestone: GIT-M-0011
author: mcp
labels: [web]
estimate: 8
created: 2026-09-13T13:12:14Z
updated: 2026-09-13T15:05:45Z
started: 2026-09-13T15:05:24Z
closed: 2026-09-13T15:05:45Z
---

## Description

As a user, I want a Settings card where I paste my YouTrack URL and token, press "Test connection" and pick my YouTrack project from a type-ahead list, so that connecting a project takes under a minute and I can see immediately whether the credentials work.

Add `YouTrackCard` to `web/src/features/settings/` and compose it into `SettingsPage.tsx:40-136` next to `GitSettingsCard` and `SyncProxyCard`. Follow the card pattern already used there: plain `useState` with controlled inputs, a `useEffect` load and an imperative save — there is no react-hook-form and no zod-in-forms in this codebase, and the only zod usage is for router search params. The card shows the instance URL, a password-type token input that renders as a placeholder once a token is stored (the API only reports `hasToken` and `tokenSource`), a "Test connection" button that shows the resolved YouTrack user or a precise error, a project combobox, a field-mapping section fed by `GET /api/v1/youtrack/fields`, and toggles for comment push and KB sync.

Feature code must not call `fetch('/api/…')` directly: add `getYouTrackSettings`, `updateYouTrackSettings`, `testYouTrackConnection`, `listYouTrackProjects` and `listYouTrackFields` to the `DataProvider` interface (`web/src/api/provider.ts`) and implement them in all four providers — companion, browser, fake and the interface itself. In browser-only mode they are unavailable: the card renders behind the `youtrack` capability, which `toCapabilities` (`web/src/api/companion-provider.ts:751-766`) maps from `features.youtrack`, and the UI branches on the capability, never on the provider kind (`AppShell.tsx:442-445`).

The project picker needs a combobox, and the repository has exactly one: the hand-rolled ARIA typeahead in `web/src/components/editor/ItemPicker.tsx:29-163` (`role="combobox"`, `aria-expanded`, `aria-controls`, `aria-autocomplete="list"`, a `role="listbox"` popup, a 200 ms debounce and a 150 ms blur grace). There is no `cmdk` and no shadcn `Command`. Extract the generic mechanics into `web/src/components/ui/combobox.tsx` and refactor `ItemPicker` to use it, so the YouTrack picker and the existing parent/milestone picker stay one implementation.

## Acceptance Criteria

- [x] A generic `Combobox` lives in `web/src/components/ui/` and `ItemPicker.tsx` is refactored onto it with no change in its existing behaviour or ARIA attributes.
- [x] `YouTrackCard` renders in `SettingsPage` only when the `youtrack` capability is true, and is absent in browser-only mode.
- [x] The token input never displays a stored token; it shows whether one is stored and where it came from (`env` or config file).
- [x] "Test connection" shows the YouTrack login and full name on success, and a distinct, actionable message for a bad token, a missing permission and a wrong base URL.
- [x] The project combobox debounces at 200 ms, is keyboard navigable (arrows, Enter, Escape) and is backed by TanStack Query.
- [x] The five new methods exist on the `DataProvider` interface and in the companion, browser and fake providers; browser and fake fail with a clear "not available in this mode" error.
- [x] Saving shows a toast that says whether the change was persisted to disk or only applied to the running process.
- [x] Vitest covers the card (load, test, save, capability gating) and the extracted `Combobox`, testing behaviour rather than implementation.

## Notes

Reference cards: `GitSettingsCard.tsx:47,62-66`, `SyncProxyCard.tsx:44-72`, `McpToolsCard.tsx:34,55`. `TeamProjectsCard.tsx:101-110` is the only existing card using TanStack Query and is the model for the query-backed parts here.

Any Markdown coming back from YouTrack is untrusted third-party content and goes through `web/src/markdown/sanitize.ts` with rehype-sanitize last — relevant when this card previews anything fetched.

Do NOT add `cmdk` or any other combobox dependency: AGENTS.md requires justifying new dependencies and the existing `ItemPicker` already implements the accessible behaviour. Do NOT store the token in `localStorage` or `sessionStorage`; it is written to the companion and read back only as a boolean.
