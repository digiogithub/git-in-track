---
id: GIT-US-0065
type: story
title: Field map configuration in the YouTrack settings card
status: done
priority: medium
parent: GIT-EP-0012
milestone: GIT-M-0011
author: mcp
labels: [web, server, docs]
estimate: 5
created: 2026-09-13T13:13:03Z
updated: 2026-09-13T17:07:51Z
started: 2026-09-13T17:07:43Z
closed: 2026-09-13T17:07:51Z
---

## Description

As a project administrator, I want to map YouTrack fields onto git-in-track fields in the settings UI, so that an import produces the statuses, priorities and types my project actually uses instead of a hard-coded guess.

Extend the YouTrack settings card created in GIT-EP-0011 with a field-map section backed by `integrations.youtrack.field_map` in `docs/.pmngr/project.yaml`. It maps YouTrack `State` values to `project.yaml` workflow status ids, `Priority` values to `critical|high|medium|low`, and `Type` values to `epic|story|task|milestone`, plus the names of the fields that carry estimation and assignee (they are not always called `Estimation` and `Assignee`). Defaults are proposed on first open by matching case-insensitively on name and on the `isResolved` flag of state values, so the common instance needs no hand editing.

Discovery drives the form: `GET /api/v1/youtrack/fields` on the companion calls `GET /api/admin/projects/{id}/customFieldSettings?fields=field(name,fieldType(id)),bundle(id,$type),canBeEmpty` and then the matching bundle endpoint for enum, state and version values, and returns `{fields: [{name, type, values[]}]}`. The card renders one row per mapped value with a native `<select>` of local targets, an "unmapped" state that is a visible warning rather than a silent default, and a save that persists through the project config writer. Because `core.ProjectConfig` has no `Extra` map and the file is never fully re-serialised, the write must be a surgical YAML-node edit in the style of `setYAMLPath` (`internal/core/allocator.go:545`), preserving comments and every unknown key.

## Acceptance Criteria

- [ ] `GET /api/v1/youtrack/fields` returns the project's custom fields with their enum, state and version values.
- [ ] The settings card renders State, Priority and Type mapping rows plus estimation and assignee field-name pickers.
- [ ] Sensible defaults are proposed on first open by name and `isResolved` matching; unmapped values are shown as warnings.
- [ ] Saving writes `integrations.youtrack.field_map` into `project.yaml` with a surgical YAML edit that preserves comments and unknown keys.
- [ ] Mapping changes take effect on the next import without a restart.
- [ ] `docs/03-data-model.md` §6 documents the `field_map` shape and `docs/07-cli-and-api.md` documents the endpoint.
- [ ] `go test -race ./internal/core/... ./internal/server/...` covers the surgical write and the discovery projection; Vitest covers the form and the unmapped warning.

## Notes

Depends on GIT-EP-0011 for the settings card, the client and the `integrations.youtrack` block, and on the mapping story of this epic for the consumer of `field_map`.

Endpoints: custom field settings and the enum, state and version bundle value endpoints are rows 4 to 7 of the scratchpad YouTrack report §8. `internal/server/git.go:374-393` (`handleGitSettingsPatch`, including the `persisted` flag) is the settings-patch shape to copy. `web/src/components/ui/select.tsx` is a styled native `<select>` — there is no Radix select.

Do NOT store the field map in the machine-local config: it is project-scoped, non-secret and belongs in the committed `project.yaml`. Do NOT re-serialise `project.yaml` wholesale — unknown keys are dropped by the decoder.
