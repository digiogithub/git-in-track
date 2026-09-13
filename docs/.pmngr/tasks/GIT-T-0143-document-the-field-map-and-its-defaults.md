---
id: GIT-T-0143
type: task
title: Document the field map and its defaults
status: done
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:18:46Z
updated: 2026-09-13T16:20:13Z
started: 2026-09-13T16:19:56Z
closed: 2026-09-13T16:20:13Z
---

## Description

Document the `integrations.youtrack.field_map` shape and its built-in defaults in `docs/03-data-model.md` §6, the discovery endpoint in `docs/07-cli-and-api.md`, and the settings section in `docs/05-web-app.md`. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] The `field_map` shape and defaults are documented in `docs/03-data-model.md` §6.
- [x] The endpoint and the settings section are documented in `docs/07-cli-and-api.md` and `docs/05-web-app.md`.
- [ ] `CHANGELOG.md` has an entry and `make lint` passes.

## Notes

`docs/03-data-model.md` §6.5 now says what `field_map` does and — the part that was missing — what
it does **not** do: it renames the custom fields the importer reads, it does not translate the
values inside them, and the built-in value maps are in the new §12.6 so the two are not duplicated.
A `comment_template` row and **R-INT-6** were added at the same time, because that key was in the
config struct and in no document.

The honest finding, verified against `config.FieldMapKeys` and `mapping.WithFieldNames`: **six of
the nine keys reach the mapper** (`status`, `priority`, `type`, `assignee`, `estimate`,
`milestone`). `labels`, `due` and `sprint` are accepted, validated, stored and consumed by nothing.
**R-INT-5** records that an unknown key is refused while an unused one is accepted in silence, and
§6.5 says plainly that the three unused keys are a promise the document is making rather than a
behaviour it is describing.

`docs/07-cli-and-api.md` §5.5 already documented `GET /api/v1/youtrack/fields` (landed with wave 2)
and is another agent's file this wave. `docs/05-web-app.md` §3.1 gains **YouTrack connection** and
**The field map** — including the two rules that make the table honest: a default is proposed and
never applied silently, and a mapping pointing at a field the instance no longer has is a warning
rather than a deletion.

`CHANGELOG.md` was not touched — another agent owns it this wave — so that criterion is unticked.
`make lint` does not lint Markdown; its current failures are in other agents' in-flight files.
