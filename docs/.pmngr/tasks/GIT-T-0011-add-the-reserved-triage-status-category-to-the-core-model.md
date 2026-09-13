---
id: GIT-T-0011
type: task
title: Add the reserved triage status category to the core model and workflow
status: done
priority: medium
parent: GIT-US-0051
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:15:31Z
updated: 2026-09-13T14:07:41Z
started: 2026-09-13T14:07:35Z
closed: 2026-09-13T14:07:41Z
---

## Description

Add `CategoryTriage StatusCategory = "triage"` to the category constants in `internal/core/model.go:58-76` and accept it in `StatusCategory.Valid()`. Make the `project.yaml` workflow decoder (`internal/core/project.go:54-69`) and the `E-PROJ-STATUS-CATEGORY` diagnostic in `internal/core/index.go` treat it as known, and add a `Workflow.TriageStatus() Status` helper returning the first declared status in that category, or empty when the project declares none. Seed one `{id: triage, name: Triage, category: triage}` status in the default workflow written by `core.CreateProject` in `internal/core/scaffold.go`, and make sure it is not reachable from the initial status by an ordinary transition.

## Acceptance Criteria

- [x] `triage` is a valid category everywhere a category is parsed or validated, and the four existing categories behave exactly as before.
- [x] `Workflow.TriageStatus()` returns the declared triage status or empty, with a unit test for both.
- [x] A newly scaffolded project declares a `triage` status; an existing project without one still validates clean.
- [x] `go test -race ./internal/core/...` passes and `make wasm` builds.

## Notes

`CategoryTriage` plus `StatusCategories()` in `model.go`; `Workflow.TriageStatus()`,
`Workflow.TriageStatuses()` and `ProjectConfig.IsTriageStatus()` in the new `internal/core/inbox.go`.
The `project.yaml` decoder and `E-PROJ-STATUS-CATEGORY` needed no change: both go through
`StatusCategory.Valid()`. `DefaultWorkflow()` now declares `triage` first, ahead of `backlog`; the
default workflow declares no transitions at all, so `triage` is neither the initial status nor a
transition target, which the scaffolding test asserts explicitly.
