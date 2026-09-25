---
id: GIT-EP-0027
type: epic
title: Specs in the web app with coverage matrix and impact view
status: done
priority: high
milestone: GIT-M-0015
author: claude
labels: [web, server, wasm]
created: 2026-09-24T12:08:24Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Give humans the same view agents get. A spec HTTP API (companion) and CoreApi methods (browser-only), a `/p/$project/specs` tree where every requirement is its own row, a requirement detail page with a trace panel, the **mandatory coverage matrix** (requirement × tests, `untested` / `passing` / `failing` / `suspect`), an impact view for a branch or ref range (companion mode), and live grammar lint in the editor through WASM.

## Acceptance Criteria

- [x] The coverage matrix ships in this milestone.
- [x] Browser-only mode keeps authoring and lint; coverage and impact show `unavailable`.
- [x] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §4 and §7 (decision 5).
