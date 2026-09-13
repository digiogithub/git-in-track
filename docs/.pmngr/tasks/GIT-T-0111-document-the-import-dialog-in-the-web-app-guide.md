---
id: GIT-T-0111
type: task
title: Document the import dialog in the web app guide
status: done
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, docs, agent-ok]
estimate: 1
created: 2026-09-13T13:18:01Z
updated: 2026-09-13T16:19:17Z
started: 2026-09-13T16:19:04Z
closed: 2026-09-13T16:19:17Z
---

## Description

Document the "Import from YouTrack" flow in `docs/05-web-app.md` — where the entry lives, what each option does, how preview differs from run, and what the progress and summary show — and note in `docs/02-architecture.md` that the feature is companion-only behind `features.youtrack`. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] `docs/05-web-app.md` describes the dialog, its options and the preview-versus-run distinction.
- [x] The companion-only gating is stated in `docs/02-architecture.md`.
- [ ] `CHANGELOG.md` has an entry.

## Notes

`docs/05-web-app.md` §3.1 gains **Import from YouTrack**: the toolbar entry and the two capability
flags that gate it, the pick / preview / run / summary flow, the depth stepper and the three
switches, the deliberately disabled "land in Inbox" control, and the per-issue summary.

`docs/02-architecture.md` §2 gains the companion-only paragraph — why `youtrackSupported` and
`youtrack` are two different questions and why the settings card and the import button are gated
differently — and §3.1 a "External trackers: none" row for browser-only mode.

`CHANGELOG.md` was not touched: it belongs to another agent in this wave, so that criterion is left
unticked.

Two claims from the earlier note were corrected against the code rather than copied. The REST
route **always** queues and answers `202` with a job id (`handleYouTrackImport`); the dialog's
inline-result branch exists but no runtime takes it today, and the documentation now says so. And
the options panel has five controls, not six as its own doc comment claims — a stepper, three
switches and the disabled Inbox checkbox.
