---
id: GIT-T-0219
type: task
title: Add the kb_sync setting and document the KB sync UI
status: done
priority: medium
parent: GIT-US-0093
milestone: GIT-M-0011
assignees: [claude]
author: mcp
labels: [web, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:21:42Z
updated: 2026-09-13T17:03:19Z
started: 2026-09-13T16:47:40Z
closed: 2026-09-13T17:03:19Z
---

## Description

Add `kb_sync: manual | on_write` and the direction `push | pull | both` to the YouTrack settings card, persisted through the project settings patch endpoint, with a short explanation that `on_write` enqueues a publish whenever a page is saved. Document the KB sync surface in `docs/05-web-app.md` and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Both settings persist through the settings endpoint and the KB toolbar reflects them.
- [x] The explanation of `on_write` is explicit about the consequence.
- [ ] `docs/05-web-app.md` and `CHANGELOG.md` are updated; Vitest covers the controls.

## Notes

The two selects landed with the settings card in the previous wave. What this
pass added is the half the story called out as a seam: `KbSyncToolbar` now reads
the project's `kb_sync` through `features/settings/youtrack-queries.ts` instead
of assuming `manual`. Under `on_write` the manual publish relabels to **Publish
now** and the toolbar says that saving a page already queues a publish, so the
button no longer describes the setting's work as its own. It is not disabled:
publishing this instant is still wanted, most obviously after a failed automatic
job. The direction is part of the reading — an `on_write` project whose direction
is `pull` publishes nothing on save, so the manual wording stays. Two Vitest
cases cover both readings.

`docs/05-web-app.md` §3.1 now documents the toolbar, the badge, the folder
confirmation, the conflict notice and this relabelling.

**The last box is left unticked for `CHANGELOG.md` only.** That file belongs to
another agent in this wave and is explicitly outside this agent's file list, so
the entry is owed by whoever owns it. Everything else in that criterion —
`docs/05-web-app.md` and the Vitest coverage — is done.
