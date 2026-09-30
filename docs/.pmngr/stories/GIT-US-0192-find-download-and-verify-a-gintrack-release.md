---
id: GIT-US-0192
type: story
title: Find, download and verify a gintrack release
status: done
priority: high
parent: GIT-EP-0032
assignees: [claude]
author: mcp
labels: [cli, agent-ok]
created: 2026-09-30T19:16:15Z
updated: 2026-09-30T19:39:08Z
started: 2026-09-30T19:25:29Z
closed: 2026-09-30T19:39:08Z
---

## Description

A native package `internal/selfupdate` (stdlib only, per ADR-040) that resolves a release and produces a verified binary on disk, without touching the installed one.

## Acceptance Criteria

- [x] Release lookup via the GitHub REST API (`/repos/digiogithub/git-in-track/releases`, paginated). "Latest" is the highest stable semver; `--prerelease` includes pre-releases, and drafts are never used. An explicit version accepts `vX.Y.Z` or `X.Y.Z`. `GITHUB_TOKEN` is used when set; a rate-limit response gives a clear error naming the token.
- [x] A minimal semver compare (major.minor.patch plus a pre-release tag) with table tests. Dev and dirty builds are recognised as non-comparable.
- [x] Asset selection for the current GOOS/GOARCH follows the docs/09 contract. A missing asset gives a clear error that lists what is available.
- [x] Downloads use a dedicated `http.Client` with timeouts and the proxy from the environment, stream to a temp file with a size cap, and are checked against the asset size and the `checksums.txt` sha256. Any mismatch aborts. The archive is extracted by exact entry name (`gintrack` / `gintrack.exe`), never "the largest file".
- [x] Tests run against an `httptest` fake GitHub (releases, pagination, pre-releases, missing asset, bad checksum, truncated download, rate limit), with no network access.

## Notes

Done in PR #125, with 85.8% coverage. The v2.2.0 assets and the `checksums.txt` format were checked against the real release; `Download` itself was not run over the real network.

API: `ParseSemver`, `New(Options)`, `Latest`, `ByVersion`, `SelectAsset`, `Download`, and the typed errors `RateLimitError`, `ChecksumMismatchError`, `SizeError`, `ErrChecksumMissing` and `ErrNotFound`.
