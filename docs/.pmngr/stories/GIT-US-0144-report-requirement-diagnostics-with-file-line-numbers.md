---
id: GIT-US-0144
type: story
title: Report requirement diagnostics with file line numbers
status: backlog
priority: low
parent: GIT-EP-0022
milestone: GIT-M-0015
author: mcp
labels: [core, good-first-issue, agent-ok]
estimate: 1
created: 2026-09-24T15:29:50Z
updated: 2026-09-24T15:29:50Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
---

## Description

Requirement-block diagnostics from GIT-US-0105 report line numbers relative to the body, not to the file. `doctor`, the CLI and the web editor then point at the wrong line in any spec with front matter.

Blocked by GIT-US-0105.

## Acceptance Criteria

- [ ] Every `E-REQ-*`, `W-REQ-*` and requirement-level link diagnostic carries a line number relative to the file, offset by the front matter.
- [ ] Golden tests assert the file line for a spec with multi-line front matter.
- [ ] `make test` and `make wasm` pass.

## Notes

Flagged by the GIT-US-0105 implementer.
