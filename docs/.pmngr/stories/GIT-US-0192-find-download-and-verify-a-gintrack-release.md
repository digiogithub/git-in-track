---
id: GIT-US-0192
type: story
title: Find, download and verify a gintrack release
status: backlog
priority: high
parent: GIT-EP-0032
author: mcp
labels: [cli, agent-ok]
created: 2026-09-30T19:16:15Z
updated: 2026-09-30T19:16:15Z
---

## Description

A native package `internal/selfupdate` (stdlib only, per ADR-040) that resolves a release and produces a verified binary on disk, without touching the installed one.

## Acceptance Criteria

- [ ] Release lookup via the GitHub REST API (`/repos/digiogithub/git-in-track/releases`, paginated). "Latest" is the highest stable semver; `--prerelease` includes pre-releases, and drafts are never used. An explicit version accepts `vX.Y.Z` or `X.Y.Z`. `GITHUB_TOKEN` is used when set; a rate-limit response gives a clear error naming the token.
- [ ] A minimal semver compare (major.minor.patch plus a pre-release tag) with table tests. Dev and dirty builds are recognised as non-comparable.
- [ ] Asset selection for the current GOOS/GOARCH follows the docs/09 contract. A missing asset gives a clear error that lists what is available.
- [ ] Downloads use a dedicated `http.Client` with timeouts and the proxy from the environment, stream to a temp file with a size cap, and are checked against the asset size and the `checksums.txt` sha256. Any mismatch aborts. The archive is extracted by exact entry name (`gintrack` / `gintrack.exe`), never "the largest file".
- [ ] Tests run against an `httptest` fake GitHub (releases, pagination, pre-releases, missing asset, bad checksum, truncated download, rate limit), with no network access.
