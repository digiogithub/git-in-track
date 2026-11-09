---
id: GIT-US-0207
type: story
title: gintrack-hub skeleton binary serving one organisation from forge clones
status: done
priority: high
parent: GIT-EP-0034
author: claude-code
assignees: [claude-code]
labels: [server, git, agent-ok]
estimate: 8
created: 2026-11-08T17:59:20Z
updated: 2026-11-09T13:00:00Z
---

## Description

Add `cmd/gintrack-hub`, a separate binary in this module (ADR-043 §1). It reads a YAML configuration naming one organisation (a team repository) and its projects, each with a forge URL and a deploy-key path, clones them into a data directory, mounts them in one `vault.Workspace`, and serves the existing SPA, REST API and WebSocket events. It pulls periodically and pushes after each commit through `internal/gitops`. Rename `internal/server/hub.go` (the WebSocket event hub) to `events_hub.go` so "hub" means one thing.

## Acceptance Criteria

- [x] `gintrack-hub --config hub.yaml` clones missing repositories and opens existing ones; a bad configuration fails with a message naming the field.
- [x] The web app, REST API and WebSocket work against the hub exactly as against the companion (one shared bearer token until GIT-US-0208).
- [x] Periodic pull and push-after-commit are tested against a local bare repository; nothing pushes to a real forge in tests.
- [x] `internal/server/hub.go` renamed to `events_hub.go` with no behaviour change.
- [x] No hub-only code in `internal/core` or `internal/vault`; `make wasm` still builds.
- [x] docs/02 gains a short hub section; CHANGELOG `[Unreleased]` entry.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
