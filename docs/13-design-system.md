---
title: Design system
type: page
tags: [design, frontend, accessibility]
---

# 13 — Design system

The web app is something people keep open for eight hours. That single fact
decides almost everything below: the palette is soft ("warm paper"), no surface
is pure white or pure black, and the contrast that matters — text, control
edges, chart marks — is measured rather than eyeballed.

The system has one hard rule, and it is mechanical rather than aesthetic: **a
colour in a component is a bug**. Every value is a token in
[`web/src/index.css`](../web/src/index.css), exposed to Tailwind in
[`web/tailwind.config.ts`](../web/tailwind.config.ts) and checked by
`npm run tokens:check`. The two documented exceptions are listed in §11.

`npm run styleguide` opens a living gallery of everything described here,
rendered by the very components it documents, with the contrast ratios computed
from the tokens as they render.

---

## 1. Principles

1. **Soft grounds, measured ink.** Light is warm paper (`#F6F3EE`), dark is warm
   charcoal (`#191714`). Neither extreme is used, because a pure-white page at
   19:00 and a pure-black one at 09:00 are both fatiguing. Body text still lands
   at 13.6:1 (light) and 14.6:1 (dark).
2. **One accent, spent on orientation.** Copper marks where you are and what you
   can do — active navigation, links, focus, the single primary action of a
   screen. It is never a chart series, so a mark can never be mistaken for a
   link.
3. **Weight is the hierarchy.** A screen has one accent button, not five. Status
   is a tinted chip; the only solid chip is `critical`, which is also labelled —
   colour is never the sole signal.
4. **Elevation is a plane plus a hairline.** Four surfaces (page, sidebar, card,
   overlay) sit close together; a border and a soft, warm shadow do the rest.
5. **Density with air.** 36px controls, 32px table headers, 8px rhythm. Dense
   enough for a backlog of hundreds, spaced enough to scan.
6. **Motion under 200ms, and optional.** Only fades and a 2% scale on overlays.
   `prefers-reduced-motion` disables all of it globally.

---

## 2. The three theme states

| State | What is stamped on `<html>` | Who decides |
|---|---|---|
| `system` (default) | nothing | `prefers-color-scheme` |
| `light` | `data-theme="light"` | the person |
| `dark` | `data-theme="dark"` | the person |

The choice lives in `localStorage` under `gintrack:theme`
([`web/src/app/theme.ts`](../web/src/app/theme.ts)) and is applied by an inline
script in `index.html` **before first paint**, so a dark-mode user is never
flashed a bright page. The control is the three-way `ThemeToggle` in the sidebar
footer: a radio group rather than a cycling button, because "follow the system"
is a state a person has to be able to *see*.

Because tokens are bare `H S% L%` triplets (which is what lets Tailwind compose
`bg-accent/15` from them), the dark palette is written twice: once under
`@media (prefers-color-scheme: dark)` and once under `[data-theme="dark"]`.
`npm run tokens:check` fails if the two blocks ever drift.

> **Never use Tailwind's `dark:` variant in this codebase.** `darkMode` is bound
> to the attribute, so a `dark:` utility silently does nothing for the many
> people on `system`. Theming is a token swap; if a component needs a value that
> differs between themes, it needs a token.

---

## 3. Colour tokens

### Surfaces

| Token | Light | Dark |
|---|---|---|
| `--background` | `38 33 95` #F6F3EE | `36 11 9` #191714 |
| `--sidebar` | `39 31 93` #F3EFE8 | `33 12 11` #1F1C19 |
| `--surface` | `40 50 98` #FCFBF7 | `34 12 12` #221F1B |
| `--surface-muted` | `39 30 91` #EFEAE1 | `33 11 16` #2D2924 |
| `--elevated` | `0 0 100` #FFFFFF | `30 11 14` #282420 |
| `--code` | `39 30 93` #F3EFE8 | `30 10 13` #24211E |

### Ink

| Token | Light | Dark |
|---|---|---|
| `--foreground` | `30 10 15` #2A2622 | `39 32 90` #EEE8DD |
| `--muted-foreground` | `36 10 38` #6B6357 | `36 12 61` #A79E90 |
| `--subtle-foreground` | `35 9 42` #756D61 | `37 9 52` #90877A |

### Interaction

| Token | Light | Dark |
|---|---|---|
| `--primary` | `30 11 18` #332E29 | `39 32 90` #EEE8DD |
| `--primary-foreground` | `40 50 98` #FCFBF7 | `36 11 9` #191714 |
| `--secondary` | `39 30 91` #EFEAE1 | `33 11 16` #2D2924 |
| `--accent` | `27 54 40` #9D602F | `27 70 64` #E39D63 |
| `--accent-hover` | `29 62 32` #84501F | `27 80 72` #F1B27E |
| `--accent-subtle` | `33 40 88` #EDE2D4 | `25 25 20` #403126 |
| `--accent-foreground` | `40 50 98` #FCFBF7 | `30 15 10` #1D1A16 |
| `--ring` | `27 54 40` #9D602F | `30 60 58` #D49454 |

### Semantic

| Token | Light | Dark |
|---|---|---|
| `--destructive` | `3 46 45` #A8433E | `5 62 66` #DE7C73 |
| `--success` | `150 37 29` #2F654A | `145 30 60` #7AB894 |
| `--warning` | `43 80 27` #7C5D0E | `42 61 60` #D7B25B |
| `--info` | `205 42 39` #3A6A8D | `204 44 66` #82B0CE |

### Lines

| Token | Light | Dark |
|---|---|---|
| `--border` | `40 24 85` #E2DCD0 | `35 12 19` #36312B |
| `--border-strong` | `40 21 78` #D3CBBB | `36 11 26` #4A443B |
| `--input` | `38 18 52` #9B8A6F | `38 11 40` #71695B |
| `--sidebar-border` | `40 22 84` #DFD9CD | `35 12 18` #332F28 |

### Status

| Token | Light | Dark |
|---|---|---|
| `--status-backlog` | `36 10 38` #6B6357 | `36 12 61` #A79E90 |
| `--status-todo` | `205 42 39` #3A6A8D | `205 46 70` #8FB8D6 |
| `--status-in-progress` | `43 80 27` #7C5D0E | `45 57 63` #D6BC6B |
| `--status-in-review` | `267 32 45` #6F4E97 | `268 46 75` #BDA2DD |
| `--status-done` | `150 37 29` #2F654A | `146 33 64` #85C19F |
| `--status-cancelled` | `35 9 50` #8B8174 | `36 12 55` #9A8F7E |

### Priority

| Token | Light | Dark |
|---|---|---|
| `--priority-critical` | `3 51 41` #9E3933 | `5 69 71` #E88B82 |
| `--priority-high` | `6 47 45` #A9483D | `4 52 75` #E0A39E |
| `--priority-medium` | `43 80 27` #7C5D0E | `45 57 63` #D6BC6B |
| `--priority-low` | `36 10 38` #6B6357 | `36 12 61` #A79E90 |

### Chart

| Token | Light | Dark |
|---|---|---|
| `--chart-todo` | `204 40 48` #4984AB | `205 48 55` #5595C3 |
| `--chart-progress` | `40 65 44` #B98927 | `40 55 55` #CBA14D |
| `--chart-done` | `149 30 24` #2B503D | `151 28 74` #AACFBD |
| `--chart-cancelled` | `39 12 50` #8F8470 | `37 8 45` #7C756A |
| `--chart-unknown` | `39 16 72` #C3BBAC | `35 10 33` #5D564C |
| `--chart-grid` | `40 24 85` #E2DCD0 | `35 12 19` #36312B |
| `--chart-ideal` | `36 10 38` #6B6357 | `36 12 61` #A79E90 |

---

## 4. What each colour is allowed to do

| Token | Used for | Never used for |
|---|---|---|
| `--accent` | links, focus ring, active nav, the one primary button, editor caret | chart series, status, a decorative wash |
| `--destructive` | errors, delete actions, `critical` priority | "attention" that is not an error |
| `--warning` | stale data, a state that will become a problem | validation errors |
| `--success` | a completed state, a confirmation | a generic "on" state |
| `--info` | neutral notices, `todo` status | links |
| `--muted-foreground` | secondary copy, table headers, icon-only chrome | anything below 14px that must be read carefully |
| `--subtle-foreground` | placeholders, meta labels, disabled affordances | body copy |

### Status, priority and charts are three different jobs

- **Status and priority chips** are read one at a time. Each is its own token
  tinted at 14–16% behind text of the same token: every chip clears 4.5:1
  against its own tint.
- **Chart series** are read *against each other*. They are a separate set,
  searched for maximum separation under protanopia, deuteranopia and tritanopia
  (worst-case ΔE ≥ 20 in both themes) while each still clears 3:1 on the card
  surface. `cancelled` is neutral by design and `unknown` is additionally
  hatched, so neither depends on colour alone.
- Changing a series colour means re-running `npm run tokens:check`, which
  re-derives every one of those numbers.

---

## 5. Elevation, radii and shadows

| Level | Surface | Border | Shadow | Example |
|---|---|---|---|---|
| 0 | `--background` | — | — | the page |
| 0 | `--sidebar` | `--sidebar-border` | — | navigation |
| 1 | `--surface` | `--border` | `shadow-card` | cards, the backlog panel |
| 1-inset | `--surface-muted` | `--input` | `shadow-xs` | fields, wells, code |
| 2 | `--elevated` | `--border` | `shadow-pop` | popovers, tooltips, dropdowns |
| 3 | `--elevated` | `--border` | `shadow-overlay` | dialogs, toasts, the save bar |

Shadows are warm-tinted in light (a neutral grey shadow on warm paper reads
dirty) and near-black in dark. There are four and only four: `shadow-xs`,
`shadow-card`, `shadow-pop`, `shadow-overlay`. Tailwind's own `shadow-sm/md/lg`
must not be used — they are cold and they ignore the theme.

Radii: `rounded-sm` 6px (chips, small buttons), `rounded-md` 8px (controls,
buttons, popovers), `rounded-lg` 12px (cards, dialogs, panels), `rounded-full`
(status chips, avatars). One step apart, so a chip inside a card never looks
rounder than the card.

---

## 6. Typography

| Role | Class | Size / weight |
|---|---|---|
| Page title | `.page-title` | 20px / 600, `-0.011em` |
| Section | `text-base font-semibold tracking-tight` | 16px / 600 |
| Body | `text-sm` | 14px / 400 |
| Secondary | `text-xs text-muted-foreground` | 12px / 400 |
| Meta label | `.section-label` | 11px / 500, uppercase, `0.08em` |
| Code | `font-mono` | 0.9em of its context |

One family (the system UI stack) and one mono stack, both self-hosted by virtue
of being system fonts — the app works offline, so there is no font CDN. Rendered
Markdown caps its *prose* at 78 characters (`markdown.css`) while tables,
diagrams and code keep the full width.

---

## 7. Components

**Buttons.** `accent` is the one thing the screen is for; `default` is a neutral
commit; `outline` and `ghost` are everything else; `destructive` is destructive;
`link` is a link. Sizes: `lg` 40px, default 36px, `sm` 32px, plus `icon` and
`icon-sm`, which always carry an `aria-label`.

**Badges.** Tint + same-hue text. `solid` exists for exactly one case (critical
priority) and is never used twice in the same row.

**Fields.** One skin, `fieldClasses` in
[`web/src/components/ui/field.ts`](../web/src/components/ui/field.ts): an inset
well, a boundary that clears 3:1 (WCAG 1.4.11), and the copper focus ring.
Anything that takes typed input imports it rather than re-describing it.

**Table.** 32px header in small caps, 36px rows, hairline separators, a hover
that tints. No zebra striping: with separators this soft it would add a second,
competing rhythm. The backlog grid is wrapped in a card so it reads as one
object.

**Banners.** A strip under the top of the content column, tinted by tone, with
an icon and an optional dismiss. Never a floating card: a banner belongs to the
page it is explaining.

**Empty states.** `.empty-state` — a dashed well with one sentence. Loading
states stay plain text, because a dashed box that appears for 200ms is noise.

**Overlays.** Dialog, tooltip, popover and toast all use `--elevated`, a border,
one of the two upper shadows, and a fade of ≤170ms.

---

## 8. Iconography

[Lucide](https://lucide.dev), 1.5px stroke, at three sizes only: **14px**
(`h-3.5 w-3.5`) inside a chip or a dense row, **16px** (`h-4 w-4`) next to text,
**20px** (`h-5 w-5`) for a lone affordance. Icons are `aria-hidden` when a label
is next to them and carry an `aria-label` when they are alone. They inherit
`currentColor`; an icon is never given a colour of its own except through the
component's own state (an active nav item tints its icon with the accent).

The brand mark is a commit graph — two commits and a merge point — drawn in
`currentColor` with the accent on the merge node
([`logo.tsx`](../web/src/components/ui/logo.tsx)); the favicon is the same shape.

---

## 9. Motion

`--duration-fast` 110ms for hovers and colour changes, `--duration` 170ms for
overlays, `--ease-out` for everything. Three keyframes exist: `fade-in`,
`scale-in` (overlays, from 98%), `slide-in` (toasts). Progress bars animate over
500ms because a bar that snaps reads as a glitch. All of it is disabled under
`prefers-reduced-motion: reduce`.

---

## 10. The accessibility contract

Enforced by `npm run tokens:check` (62 assertions today):

- every text token clears **4.5:1** on every surface it is used on, in both
  themes;
- `--input`, the boundary of a control, clears **3:1** against the page and the
  card;
- every chart series clears **3:1** on the card surface and stays **ΔE ≥ 20**
  from every other series under normal vision and all three dichromacies;
- the two dark blocks are identical.

Beyond colour: one focus treatment everywhere (a 2px copper ring, offset), a
skip link, `role`/`aria-*` on every custom control (the switch is a
`role="switch"` button, the theme control a `radiogroup`), and no state that is
communicated by colour alone.

---

## 11. Documented exceptions

Two places cannot read a CSS variable, and both are marked in the code:

1. **Mermaid** bakes colours into the SVG it renders, so
   [`mermaid-theme.ts`](../web/src/markdown/mermaid-theme.ts) holds literal hex
   values for both themes. Keep it in step with §3.
2. **The `<select>` chevron** is a `background-image` data URI, which cannot
   inherit `currentColor`, and a `dark:` utility would not fire for people on
   `system`. It is a single mid warm neutral measured at 3.5:1 on the light
   field and 4.0:1 on the dark one.

Syntax highlighting is Shiki's `vitesse-light` / `vitesse-dark` pair (chosen for
being low-saturation and warm); only the *text* colours come from it, while the
code block's background is the `--code` token. The theme names are allow-listed
in [`sanitize.ts`](../web/src/markdown/sanitize.ts) — change one and you must
change the other.

---

## 12. Changing something

1. Edit the token in `web/src/index.css` — **both** dark blocks.
2. Run `npm run tokens:check`. It prints every ratio and every ΔE, and fails on
   the ones that regressed.
3. Run `npm run styleguide` and look at the page in light, dark and system.
4. If you added a token, expose it in `tailwind.config.ts` under a name that
   says what it is *for*, not what it *looks like*.
5. If a component needed a hardcoded colour, the system is missing a token: add
   the token instead, and say what it is allowed to do in §4.
