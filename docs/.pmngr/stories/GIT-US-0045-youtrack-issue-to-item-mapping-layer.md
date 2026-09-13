---
id: GIT-US-0045
type: story
title: YouTrack issue to item mapping layer
status: backlog
priority: high
parent: GIT-EP-0012
milestone: GIT-M-0011
author: mcp
labels: [core, server]
estimate: 8
created: 2026-09-13T13:11:09Z
updated: 2026-09-13T13:11:09Z
---

## Description

As a developer integrating YouTrack, I want a pure, well-tested mapping package that turns a YouTrack issue payload into a `core.ItemDraft` or `core.ItemPatch`, so that every import surface (vault, REST, MCP, CLI) shares one deterministic translation and no business logic leaks into the transport layers.

Add `internal/youtrack/mapping` (native-only, outside the WASM-compiled `internal/core`, next to the client from GIT-EP-0011). It takes the decoded issue struct produced by the client — `id`, `idReadable`, `summary`, `description`, `created`, `updated`, `resolved`, `project`, `reporter`, `customFields`, `links`, `tags`, `comments`, `attachments` — plus the project's `integrations.youtrack.field_map` and the `core.ProjectConfig` workflow, and returns a draft plus the derived relationships. Type comes from the `Type` custom field (Epic to `epic`, User Story to `story`, Task and Bug to `task`); a value from the Version bundle (`Fix versions`) maps to `milestone`. `State` maps to a `project.yaml` status id through `field_map`, `Priority` to `critical|high|medium|low`, the `Estimation` period (`presentation` like `3d 4h`, or `minutes`) to `estimate` points, `Assignee` (`login`) to `assignees[]`, `tags[].name` to `labels[]`. Custom field values are reduced with the documented order `name` then `login`/`fullName` then `localizedName` then `presentation` then `idReadable` then `id`, and the raw `$type` is kept so a value is never silently mis-typed.

The description is normalised Markdown: YouTrack auto-links bare issue ids (`ACME-42`) so they must not be re-linked, `{color:red}…{color}` and `{width=300px}` extensions are stripped or passed through explicitly, and attachment image embeds `![alt](file.png)` are rewritten to `.pmngr/attachments/<ITEM-ID>/file.png`. Links are read from the issue payload: `linkType.name == "Subtask"` with `direction: OUTWARD` yields children, `INWARD` yields the parent; `Relates`, `Depend` and `Duplicate` map onto the five kinds `core.LinkKind.Valid()` accepts (`internal/core/model.go:116-124`) and everything else is dropped with a warning. Entries whose `issues` array is empty are filtered out first. Comments become `core.Comment` drafts carrying the original author, `created` and the YouTrack comment id. Every produced draft carries `external: [{system: youtrack, id: <idReadable>, url: <base>/issue/<idReadable>}]`.

## Acceptance Criteria

- [ ] `internal/youtrack/mapping` exposes `IssueToDraft`, `IssueToPatch` and `CommentsToDrafts` with no dependency on `internal/server`, `internal/vault` or `net/http`.
- [ ] Type mapping covers Epic, User Story, Task, Bug and a Version-bundle value to `milestone`, with a configurable override from `field_map`.
- [ ] `State`, `Priority`, `Estimation`, `Assignee` and `tags` map to `status`, `priority`, `estimate`, `assignees` and `labels`; unknown values fall back to defaults and are reported, never dropped silently.
- [ ] Description normalisation leaves auto-linked issue ids untouched and rewrites attachment image refs to the local attachments path.
- [ ] Link entries with an empty `issues` array are filtered; Subtask OUTWARD yields children and INWARD yields the parent; unsupported link types are reported, not invented.
- [ ] Every draft carries `external: [{system: youtrack, id, url}]`.
- [ ] Table-driven tests with fixture JSON under `internal/youtrack/mapping/testdata/` cover each field kind and the link-shape gotchas; `go test -race ./internal/youtrack/...` passes.

## Notes

Depends on GIT-EP-0011 for the `external` front-matter field (`core.Item`, `SerializeItem`, `ItemDraft`/`ItemPatch`) and for the typed client and `project.yaml` `integrations.youtrack.field_map`. Do not re-plan either here.

Reference shapes: the issue JSON example and the `$type` table in the scratchpad YouTrack report §3.1, §3.5, §3.7 and §9. `customFields(...)` must be requested with `$type` (`customFields(id,name,$type,value(id,name,$type,login,fullName,presentation,minutes,isResolved))`) — the reference CLI omits it and is lossy. Most `links[]` entries come back with `"issues": []` and must be filtered.

Do NOT put HTTP calls in this package — it maps already-decoded payloads. Do NOT add an `external` link kind to `core.LinkKind`; the five existing kinds are the whole set.
