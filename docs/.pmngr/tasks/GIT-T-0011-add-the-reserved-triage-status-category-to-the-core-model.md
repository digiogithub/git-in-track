---
id: GIT-T-0011
type: task
title: Add the reserved triage status category to the core model and workflow
status: todo
priority: medium
parent: GIT-US-0051
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:15:31Z
updated: 2026-09-13T13:15:31Z
---

## Description

Add `CategoryTriage StatusCategory = "triage"` to the category constants in `internal/core/model.go:58-76` and accept it in `StatusCategory.Valid()`. Make the `project.yaml` workflow decoder (`internal/core/project.go:54-69`) and the `E-PROJ-STATUS-CATEGORY` diagnostic in `internal/core/index.go` treat it as known, and add a `Workflow.TriageStatus() Status` helper returning the first declared status in that category, or empty when the project declares none. Seed one `{id: triage, name: Triage, category: triage}` status in the default workflow written by `core.CreateProject` in `internal/core/scaffold.go`, and make sure it is not reachable from the initial status by an ordinary transition.

## Acceptance Criteria

- [ ] `triage` is a valid category everywhere a category is parsed or validated, and the four existing categories behave exactly as before.
- [ ] `Workflow.TriageStatus()` returns the declared triage status or empty, with a unit test for both.
- [ ] A newly scaffolded project declares a `triage` status; an existing project without one still validates clean.
- [ ] `go test -race ./internal/core/...` passes and `make wasm` builds.
