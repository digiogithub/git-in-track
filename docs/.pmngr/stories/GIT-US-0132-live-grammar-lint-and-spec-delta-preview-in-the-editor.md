---
id: GIT-US-0132
type: story
title: Live grammar lint and Spec Delta preview in the editor
status: done
priority: medium
parent: GIT-EP-0027
milestone: GIT-M-0015
author: claude
labels: [web, wasm, agent-ok]
estimate: 3
created: 2026-09-24T12:11:35Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0108 }
  - { kind: blocked_by, target: GIT-US-0109 }
---

## Description

As an author, I want lint findings underlined as I type a requirement or a Spec Delta, in both operating modes, because the linter runs in the WASM core.

## Acceptance Criteria

- [x] `wasm/main_js.go` exports the grammar linter and delta parser; the CodeMirror 6 editor shows findings as diagnostics (warning vs error per `project.yaml`).
- [x] In a story body, the `## Spec Delta` section shows a preview of ADDED/MODIFIED/REMOVED against the current spec.
- [x] Works identically in browser-only and companion mode; `make wasm` and `make wasm-smoke` pass.
- [x] Vitest tests with a mocked core; docs/05 updated.

## Notes

No duplication of lint rules in TypeScript.
