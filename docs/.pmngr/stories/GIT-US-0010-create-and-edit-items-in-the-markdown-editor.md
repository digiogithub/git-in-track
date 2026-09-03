---
id: GIT-US-0010
type: story
title: Create and edit items in the Markdown editor
status: in_progress
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T21:47:06Z
started: 2026-09-03T21:47:06Z
author: team
priority: critical
parent: GIT-EP-0002
milestone: GIT-M-0002
estimate: 8
labels: [web, core]
links:
  - kind: blocked_by
    target: GIT-US-0009
  - kind: blocked_by
    target: GIT-US-0004
---

## Description

As a project member, I want to create and edit epics, stories, tasks and milestones from
the UI, so that the browser is a complete replacement for hand-editing Markdown files.

Front matter is edited through a form driven by `project.yaml` (status picker limited to
allowed transitions, priority, labels, assignees, parent, milestone, estimate), and the
body is edited in CodeMirror 6 with Markdown syntax support, wikilink autocomplete and a
live preview. New items get an ID from the allocator and a slugged file name.

Writes go through the core's canonical serialiser straight to the working tree, with the
`updated` field maintained automatically. Acceptance-criteria checkboxes can be ticked from
the detail view without opening the editor.

## Acceptance Criteria

- [ ] Creating an epic, story, task or milestone produces a correctly named, valid file.
- [ ] The status picker offers only transitions allowed by the workflow.
- [ ] Body editing in CodeMirror supports Markdown, wikilink autocomplete and preview.
- [ ] `updated` is set on every write; `created` and `author` are never overwritten.
- [ ] Files are written through the canonical serialiser and validate cleanly.
- [ ] Task-list checkboxes can be toggled from the detail view and persist to disk.
- [ ] Concurrent external modification is detected by `rev` and the user is warned before
      overwriting.
- [ ] Unsaved changes survive a tab reload via a local draft, and are clearly marked.
- [ ] Deleting an item warns about inbound references and never leaves a dangling parent.
