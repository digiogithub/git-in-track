---
id: GIT-US-0196
type: story
title: Align .goreleaser.yaml with the real release assets
status: backlog
priority: low
author: mcp
labels: [ci]
created: 2026-09-30T19:16:24Z
updated: 2026-09-30T19:16:24Z
---

## Description

Human-only area (release pipeline). `.goreleaser.yaml` is used only by `make release-check` / `release-snapshot`. It still declares darwin archives as tar.gz (:44-50), but `release.yml` publishes darwin as a signed and notarized zip, and the GHCR, Homebrew and Scoop sections describe channels that have been paused since ADR-029. GIT-EP-0032 makes the asset names a contract for `gintrack update`, so the snapshot config should produce the same layout.

## Acceptance Criteria

- [ ] `.goreleaser.yaml` snapshot archives match release.yml: linux tar.gz, darwin and windows zip, a flat layout and `checksums.txt`.
- [ ] Paused channels are commented out or marked, consistent with docs/09 §10.
- [ ] `make release-check` passes.

## Notes

Found during the GIT-EP-0032 analysis (2026-09-30). Not `agent-ok`: a human changes release config until the maintainer says otherwise.
