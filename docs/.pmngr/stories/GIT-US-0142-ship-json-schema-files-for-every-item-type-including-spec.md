---
id: GIT-US-0142
type: story
title: Ship JSON Schema files for every item type including spec
status: done
priority: medium
parent: GIT-EP-0022
milestone: GIT-M-0015
author: mcp
labels: [core, docs, agent-ok]
estimate: 3
created: 2026-09-24T15:29:50Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
  - { kind: blocked_by, target: GIT-US-0106 }
---

## Description

docs/03 §18 describes JSON Schema files under `internal/core/schema/`, and its outline already lists `spec.schema.json`. The folder does not exist for any item type. GIT-US-0105 left its JSON Schema acceptance criterion open for this reason.

Blocked by GIT-US-0105 and GIT-US-0106 (the link kinds must be final).

## Acceptance Criteria

- [x] `internal/core/schema/` holds one schema per item type (epic, story, task, milestone, spec), plus project.yaml, generated from or checked against the Go model.
- [x] `spec.schema.json` covers the `requirements:` map (`status`, `trace`, `verified`, `links`).
- [x] A test fails when the Go model and the schemas drift.
- [x] GIT-US-0105's JSON Schema criterion is ticked, referencing this story.
- [x] docs/03 §18 matches what ships. `make test`, `make lint` and `make wasm` pass.

## Notes

Flagged as unowned by the GIT-US-0105 implementer.
