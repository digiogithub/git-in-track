---
id: GIT-US-0095
type: story
title: Retire the corpus exporter and its configuration surface
status: backlog
priority: high
parent: GIT-EP-0020
milestone: GIT-M-0013
author: mcp
labels: [core, server, web]
created: 2026-09-16T12:45:58Z
updated: 2026-09-16T12:45:58Z
---

## Description

As a maintainer, I want the exported corpus gone, so that there is one copy of the backlog and the knowledge base — the committed files — and no second tree to keep in sync, explain or forget to delete.

Remove `internal/pandosync` (`doc.go`, `document.go`, `exporter.go`, `paths.go` and their tests), the hub subscription in `internal/server/pandosync_hub.go`, and the `startCorpusSync`/`armSync`/`syncNow` path it hangs from in `Server.Start`.

Remove the configuration surface with it: `search.pando.corpusDir` (`internal/config/config.go:493-495`), its absolute-path validation (`internal/config/validate.go:198`), `SearchCorpusDir` (`internal/server/server.go:147-151`, `cmd/gintrack/serve.go:199-203`), `corpusBase`/`corpusRoot()` and the `corpusDir` view and patch fields in `internal/server/search_settings.go`, the `corpusDir` field in the REST payload of `GET`/`PATCH /api/v1/search/settings`, and the corpus-directory input and per-repo corpus rows in `web/src/features/settings/PandoSearchCard.tsx` with their provider types and fakes.

`POST /api/v1/search/reindex` keeps working but loses its export half: it calls Pando's REST reindex and nothing else.

Deleting the exporter is also what removes a feedback loop that an in-repo corpus would have created — `internal/server/watch.go:231` publishes `item.changed`/`file.changed` on any repository write, and the exporter consumes that hub.

## Acceptance Criteria

- [ ] `internal/pandosync` and `internal/server/pandosync_hub.go` are deleted; nothing imports them.
- [ ] `search.pando.corpusDir` is gone from the config struct, the YAML, the validator, the REST payload and the TypeScript provider types; a configuration file that still sets it is refused with a message naming the epic rather than ignored silently.
- [ ] The web settings card no longer offers a corpus directory, and its per-repo rows describe what Pando indexes instead.
- [ ] `POST /api/v1/search/reindex` triggers Pando's KB reindex and reports its result honestly, with no export step.
- [ ] A corpus directory left behind by a previous version is not deleted by gintrack; the release notes say it is safe to remove by hand.
- [ ] `go test -race ./...` and the web suite pass with no reference to the corpus remaining outside documentation that describes the history.

## Notes

`refuseRepository` in `exporter.go`, and `TestNewRefusesACorpusInsideARepository` (`internal/pandosync/exporter_test.go:620`), disappear with the package rather than being inverted.

Tests that will need reworking or deleting: `internal/pandosync/*_test.go` entirely, `internal/server/search_settings_test.go:108-119`, `internal/config/pando_test.go:37,62,335-339`.

Do NOT delete the search backend or the Pando REST client (`internal/pando/rest.go`); only the exporter and the corpus configuration. The comment at `internal/pando/rest.go:27` describing the resync path "for a corpus exported with KBWatch off" needs rewriting, not the code.
