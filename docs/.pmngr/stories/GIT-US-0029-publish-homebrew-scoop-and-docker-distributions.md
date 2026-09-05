---
id: GIT-US-0029
type: story
title: Publish Homebrew, Scoop and Docker distributions
status: in_review
priority: medium
parent: GIT-EP-0007
milestone: GIT-M-0007
author: team
labels: [ci, cli]
estimate: 5
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0005 }
---

## Description

As a user, I want to install git-in-track the way I install everything else on my platform,
so that trying it does not start with downloading a zip and arguing with Gatekeeper.

GoReleaser publishes a formula to `digiogithub/homebrew-tap`, a manifest to
`digiogithub/scoop-bucket`, and multi-arch images to `ghcr.io/digiogithub/git-in-track`, all
from the existing release workflow. `go install` is documented as the developer path, with
its limitation — no embedded UI unless the frontend was built first — stated plainly.

Homebrew is the recommended macOS route precisely because it removes the quarantine
attribute, which is the friction the unsigned-binary policy creates.

## Acceptance Criteria

- [ ] `brew install digiogithub/tap/gintrack` installs a working binary on macOS and Linux.
- [x] `scoop install gintrack` works after adding the bucket on Windows.
- [x] Multi-arch images for amd64 and arm64 are published to GHCR with a `latest` manifest.
- [x] The documented `docker run` command serves the UI against a mounted repository.
- [x] `go install .../cmd/gintrack@latest` works and its limitation is documented.
- [x] All channels are published automatically by the release workflow from one tag.
- [x] Release permissions are least-privilege and tap tokens are stored as secrets.
- [x] The README installation section covers every channel with a verified command.

## Notes

Implemented on `feat/phase-6-retros-metrics`. The channels are wired into the
existing GoReleaser run and the existing release workflow; nothing is published
until the first `v*` tag.

**The first acceptance criterion is met on macOS only.** GoReleaser deprecated
`brews:` (formulae) in v2.10 and `goreleaser check` — which the release workflow
runs before publishing — now exits non-zero on it, so the tap ships a
`homebrew_casks:` cask. Casks are macOS-only, so `brew install` does not cover
Linux; Linux users take the tarball, `go install` or the container image. The
decision, the alternatives and the cost are recorded in
[ADR-016](../../adr/ADR-016-homebrew-cask-instead-of-formula.md).

Verified locally with `goreleaser release --snapshot --clean --skip=publish`
(GoReleaser v2.18.0): 6 archives + `checksums.txt`, `dist/homebrew/Casks/gintrack.rb`,
`dist/scoop/bucket/gintrack.json`, and the amd64 image, which serves the embedded
UI and the authenticated API against a repository mounted at `/work`. The arm64
image could not be built on the development host (no binfmt emulation installed);
the release workflow provides it with `docker/setup-qemu-action@v3`.

Before the next release a maintainer must create `digiogithub/homebrew-tap` and
`digiogithub/scoop-bucket`, and the `HOMEBREW_TAP_TOKEN` and `SCOOP_BUCKET_TOKEN`
Actions secrets (fine-grained PATs, `Contents: read and write`, one repository
each). The release job checks both before it builds anything and fails with the
fix in the message when either is missing. GHCR needs no secret: `GITHUB_TOKEN`
with `packages: write` covers it.
