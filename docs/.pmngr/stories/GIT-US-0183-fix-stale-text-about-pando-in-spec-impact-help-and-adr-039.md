---
id: GIT-US-0183
type: story
title: Fix stale text about Pando in spec impact help and ADR-039
status: done
priority: low
assignees: [claude]
author: mcp
labels: [cli, docs, agent-ok, good-first-issue]
created: 2026-09-29T22:56:37Z
updated: 2026-09-30T13:06:59Z
started: 2026-09-30T12:58:26Z
closed: 2026-09-30T13:06:59Z
---

## Description

`gintrack spec impact --help` still says the CLI has no Pando client, but since GIT-US-0176 it discovers a managed instance. ADR-039's status line says "nothing implemented" although GIT-US-0173..0177 are merged, and its context still quotes the old CORS `*` claim fixed by GIT-US-0172 (#107 §10.6).

## Acceptance Criteria

- [x] `spec impact --help` describes how tiers 2–3 find Pando (external config or a running managed instance).
- [x] ADR-039 gets a dated implementation note (status stays `proposed` until the maintainer accepts it) and a note that the CORS claim is historical.
- [x] `make lint` and `make test` pass.

## Notes

The ADR half was done in #109: ADR-039 was accepted on 2026-09-30, with an implementation note and a historical CORS note. The help text was fixed in #116, pinned by `cmd/gintrack/spec_impact_help_test.go`. No other stale `spec` help texts were found.
