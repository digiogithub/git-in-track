---
id: GIT-T-0111
type: task
title: Document the import dialog in the web app guide
status: todo
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, docs, agent-ok]
estimate: 1
created: 2026-09-13T13:18:01Z
updated: 2026-09-13T13:18:01Z
---

## Description

Document the "Import from YouTrack" flow in `docs/05-web-app.md` — where the entry lives, what each option does, how preview differs from run, and what the progress and summary show — and note in `docs/02-architecture.md` that the feature is companion-only behind `features.youtrack`. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] `docs/05-web-app.md` describes the dialog, its options and the preview-versus-run distinction.
- [ ] The companion-only gating is stated in `docs/02-architecture.md`.
- [ ] `CHANGELOG.md` has an entry.
