---
id: GIT-EP-0032
type: epic
title: "gintrack update: verified self-update from GitHub Releases"
status: backlog
priority: medium
author: mcp
labels: [cli, server, web, docs]
created: 2026-09-30T19:15:45Z
updated: 2026-09-30T19:15:45Z
---

## Description

Add a `gintrack update` command that replaces the running binary with a newer official release from GitHub Releases (digiogithub/git-in-track), modelled on Pando's `pando update` (/www/MCP/Pando/pando cmd/update.go, internal/updatecheck) but fixing its weaknesses:
- Pando does no checksum or signature check.
- It can install pre-releases.
- It picks "latest" in API order instead of by highest version.
- Its download has no timeout or size cap.
- It overwrites package-manager installs.
- Its background notice cannot be turned off.

Maintainer decisions (2026-09-30):
1. **Notices:** `gintrack doctor` and the web UI show "update available", with the answer cached about 6 h. A notice at CLI start happens only with the opt-in `update.checkOnStart: true`, and never in JSON or MCP output. No telemetry.
2. **Install channels:** Homebrew, Scoop and Docker installs are refused, with a pointer to `brew upgrade` / `scoop update` / pulling the image. A source or dev build (`version=dev`, `builtBy=source`) only accepts an explicit version. `--force` overrides both.
3. **Safety:** by default only stable releases are considered, choosing the highest semver; `--prerelease` opts in. Every download is verified against the release's `checksums.txt` (sha256) and against the asset size, and the update aborts on any mismatch.
4. **Implementation:** the standard library only (net/http, archive/tar, archive/zip, crypto/sha256) and a minimal internal semver, with no new dependencies. The swap is atomic, with rollback, and handles a running .exe on Windows. After updating, a running `gintrack serve` or managed Pando is detected and the user is told to restart it; nothing restarts automatically.

The code lives in a native package such as `internal/selfupdate` plus `cmd/gintrack`, never in `internal/core` (the WASM rule). `release.yml` and `.goreleaser.yaml` stay human-only. The updater depends on the published asset names (`gintrack_<ver>_<os>_<arch>.tar.gz|zip` + `checksums.txt`) as a contract.

## Acceptance Criteria

- [ ] All child stories are done.
- [ ] `gintrack update` upgrades a release build on Linux, macOS and Windows end to end against a real GitHub release, and refuses package-manager installs.
- [ ] ADR-040 records the decisions; docs/07 and docs/09 describe the command and the asset contract.
