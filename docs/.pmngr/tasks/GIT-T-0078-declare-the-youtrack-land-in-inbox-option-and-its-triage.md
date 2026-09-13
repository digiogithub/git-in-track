---
id: GIT-T-0078
type: task
title: Declare the YouTrack land_in_inbox option and its triage landing target
status: done
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [core, docs]
estimate: 2
created: 2026-09-13T13:17:12Z
updated: 2026-09-13T17:23:24Z
started: 2026-09-13T17:23:05Z
closed: 2026-09-13T17:23:24Z
---

## Description

Add `LandInInbox bool` to the `integrations.youtrack` block of `core.ProjectConfig` (`internal/core/project.go:23-40`) and expose a single helper the importer calls to decide its landing target, so that GIT-EP-0012 can consume it without this story depending on the importer. Document the key in `docs/03-data-model.md` §6. Remember that `ProjectConfig` has no `Extra` map, so an undeclared key is invisible to Go, and that `project.yaml` is never re-serialized wholesale — do not add a writer.

## Acceptance Criteria

- [x] `integrations.youtrack.land_in_inbox` decodes from `project.yaml` and defaults to false.
- [x] A helper returns the landing status (triage or the workflow initial) and errors when the project has no triage status but the option is on.
- [x] `docs/03-data-model.md` §6 documents the key, cross-referencing GIT-EP-0012.
- [x] `go test -race ./internal/core/...` covers both settings.

## Notes

**The file the description names is the wrong one, and the design moved under it.** `core.ProjectConfig` carries no `integrations` block at all: the `integrations.youtrack` block is modelled by `config.YouTrackLink` in `internal/config/projectlink.go`, deliberately, because the core struct models no unknown keys and `project.yaml` carries hand-written comments and sections this build does not know about, so the block is read and edited node by node. The field therefore landed on `config.YouTrackLink` as `LandInInbox bool` / `yaml:"land_in_inbox,omitempty"`. The criterion as written — the key decodes and defaults to false — is satisfied; only its stated location is not, and could not be.

No writer was added, as instructed. `SaveYouTrackLink` edits only the keys the settings screen owns and never deletes one it does not name, so a team that set `land_in_inbox: true` by hand keeps it across a connection save. `TestLoadYouTrackLinkLandInInbox` pins all three cases, the round trip included.

The helper is `core.InboxLandingStatus(cfg *ProjectConfig, landInInbox bool) (Status, error)` in `internal/core/inbox.go`, with the sentinel `core.ErrNoTriageStatus`. It lives in the core rather than in `internal/config` so that every entry point that can file work — the import of GIT-EP-0012, an agent's `create_inbox_item`, a web submission — answers "where does this land" the same way, and so that it compiles to WASM. Off returns the workflow's initial status; on returns the project's first triage status; on with no triage status is a refusal, never a quiet fall back to the backlog, which would be the one outcome the option exists to prevent. `TestInboxLandingStatus` is table-driven over both settings plus the refusal and the nil-config case.

`docs/03-data-model.md` §6 documents the key in the example block and the field table and adds **R-INT-7** stating the decision, the refusal, the shared helper and the read-never-written rule, cross-referencing §6.4 and GIT-EP-0012. `docs/07-cli-and-api.md` §4.17 previously implied the option already worked; it now says plainly that the importer does not read it yet and that wiring it into the import write path is GIT-EP-0012.
