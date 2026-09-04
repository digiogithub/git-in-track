---
id: GIT-US-0029
type: story
title: Publish Homebrew, Scoop and Docker distributions
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: medium
parent: GIT-EP-0007
milestone: GIT-M-0007
estimate: 5
labels: [ci, cli]
links:
  - kind: blocked_by
    target: GIT-US-0005
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
- [ ] `scoop install gintrack` works after adding the bucket on Windows.
- [ ] Multi-arch images for amd64 and arm64 are published to GHCR with a `latest` manifest.
- [ ] The documented `docker run` command serves the UI against a mounted repository.
- [ ] `go install .../cmd/gintrack@latest` works and its limitation is documented.
- [ ] All channels are published automatically by the release workflow from one tag.
- [ ] Release permissions are least-privilege and tap tokens are stored as secrets.
- [ ] The README installation section covers every channel with a verified command.
