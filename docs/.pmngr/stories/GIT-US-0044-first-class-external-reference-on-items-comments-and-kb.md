---
id: GIT-US-0044
type: story
title: First-class `external` reference on items, comments and KB pages
status: done
priority: high
parent: GIT-EP-0011
milestone: GIT-M-0011
author: mcp
labels: [core, wasm, docs]
estimate: 8
created: 2026-09-13T13:10:47Z
updated: 2026-09-13T14:03:54Z
started: 2026-09-13T14:03:48Z
closed: 2026-09-13T14:03:54Z
---

## Description

As a maintainer whose backlog mirrors an external tracker, I want items, comments and knowledge-base pages to carry a first-class `external` reference, so that every imported or pushed artifact records exactly where it came from and can be matched again on re-import without guessing from the title.

The shape agreed with the product owner is `external: [{system: youtrack, id: PRJ-42, url: https://…, key?: …, synced_at?: …}]`, a list so one item can be linked to more than one system later. Add an `External` struct to `internal/core/model.go` beside `Link` (`:145`) and a `External []External` field to `Item` (`:324`), `Comment` (`:377`) and the KB page type in `internal/core/kb.go`. Wire it through the front-matter reader and writer: `ParseItem` (`internal/core/frontmatter.go:189`), `ParseComment` (`:289`), `SerializeItem` (`:362`), `SerializeComment` (`:404`), and place the key in the canonical key order documented in `docs/03-data-model.md` §3.2 so round-tripping a file is byte-stable.

Writes must follow the existing sparse-patch discipline: `ItemDraft` (`internal/core/store.go:118`) accepts the full list at creation, `ItemPatch` (`:154`) gets `AddExternal` / `RemoveExternal` set operations mirroring `labels` and `links` so two clients writing different systems do not clobber each other. The index (`internal/core/index.go`) keeps a lookup from `system` + external `id` to `core.ItemID`, exposed both as a `Filter` field (`internal/core/query.go:33`) and as a direct lookup, because the importer of GIT-EP-0012 needs an O(1) idempotence check. Everything here lives in `internal/core`, so it must compile under `GOOS=js GOARCH=wasm`.

## Acceptance Criteria

- [x] `core.External{System, ID, URL, Key, SyncedAt}` exists and is carried by `Item`, `Comment` and KB pages.
- [x] `ParseItem`/`ParseComment` read `external:` and reject malformed entries (missing `system` or `id`) through `internal/core/validate.go`.
- [x] `SerializeItem`/`SerializeComment` emit `external` at its documented position in the canonical key order; parse→serialize round-trip is byte-identical on the golden files.
- [x] `ItemDraft.External` and `ItemPatch.AddExternal`/`RemoveExternal` work, with set semantics identical to `labels`/`links` and no clobbering on concurrent patches.
- [x] `Index` resolves `(system, externalID) → ItemID`, and `core.Filter` can select items by external system and id.
- [x] Unknown `system` values are accepted and preserved (forward compatibility); no enum is hard-coded in the parser.
- [x] `docs/03-data-model.md` field table, §3.2 key order and §18 JSON Schema are updated in the same change.
- [x] `docs/adr/ADR-031-external-references.md` is written and accepted, with its negative consequences section filled in.
- [x] `go test -race ./internal/core/...` passes and `make wasm` still builds.

## Notes

Existing code to follow: `Link`/`LinkKind` in `internal/core/model.go:104-145` is the closest precedent for a repeated structured front-matter field; `applyPatch` (`internal/core/store.go:754`) shows how add/remove sets are applied; golden files live under `internal/core/testdata/` with a `-update` flag (see AGENTS.md "Testing expectations").

Hard constraints: `internal/core/doc.go:11-12` forbids `os`, `os/exec`, `syscall`, `syscall/js`, `net/http`, fsnotify and go-git in this package — nothing about YouTrack HTTP belongs here. Every write is rev-checked (`docs/03` R-REV-3c), so a surface must never split a multi-field write into two.

Do NOT model this as a `custom_fields` entry or stuff it into `links`: the product owner has decided it is a first-class field. Do NOT rewrite `project.yaml` wholesale anywhere in this story — `ProjectConfig` drops unknown keys (`internal/core/project.go:23-40`).
