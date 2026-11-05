---
id: GIT-US-0206
type: story
title: Selectable colour palettes for clarity and visibility, chosen in Settings
status: in_review
priority: medium
parent: GIT-EP-0033
author: mcp
assignees: [claude-code]
labels: [web, agent-ok]
estimate: 3
created: 2026-11-05T18:55:03Z
updated: 2026-11-05T19:07:07Z
---

## Description

The web app has one warm palette with light, dark and system modes (`web/src/app/theme.ts`, `web/src/index.css`, `public/theme-boot.js`). The maintainer wants extra colour schemes that improve clarity and visibility, selectable from Settings.

Add a second, independent dimension next to the mode: a **palette**, stamped as `data-palette` on `<html>`. Mode (light/dark/system) keeps working as today; every palette has a light and a dark variant.

Palettes:

- **Default** — today's warm palette, unchanged.
- **High contrast** — near-black on white (and white on near-black), stronger borders, focus rings and muted text meeting WCAG AAA (7:1) for body text and 3:1 for UI boundaries.
- **Cool** — crisp neutral blue-grey surfaces with a blue accent, for people who find the warm tones low-contrast.
- **Colour-blind safe** — status, priority, label and chart colours drawn from a palette distinguishable under deuteranopia/protanopia (e.g. blue/orange based), never relying on red vs green alone.

## Acceptance Criteria

- [ ] Settings page has an "Appearance" card with the mode choice (light/dark/system, reusing the existing preference) and a palette choice with a small swatch preview per palette.
- [ ] Palette persisted in `localStorage` (own key next to `gintrack:theme`), applied before first paint by `public/theme-boot.js` (no flash), synced across tabs like the theme.
- [ ] Palettes are pure CSS token overrides in `web/src/index.css` (`:root[data-palette=...]` combined with the light/dark/system rules); no component hard-codes colours for them. Status/priority badges and boards use tokens so the colour-blind palette reaches them.
- [ ] Unknown or corrupt stored values fall back to Default.
- [ ] Contrast of body text and muted text checked per palette and mode (test or documented table).
- [ ] Vitest coverage for the preference store, boot script behaviour and the Settings card.
- [ ] `docs/05-web-app.md` documents palettes; CHANGELOG `[Unreleased]` entry.
