---
id: GIT-US-0005
type: story
title: Set up the CI pipeline and the WASM build
status: done
priority: critical
parent: GIT-EP-0001
milestone: GIT-M-0001
author: team
labels: [ci, wasm]
estimate: 5
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T21:47:05Z
started: 2026-09-03T21:17:39Z
closed: 2026-09-03T21:47:05Z
links:
  - { kind: blocked_by, target: GIT-US-0001 }
---

## Description

As a maintainer, I want every push and pull request verified automatically and every tag
released automatically, so that `main` is always releasable and cutting a release is a
`git push` of a tag.

This implements `.github/workflows/ci.yml` and `.github/workflows/release.yml` and
`.goreleaser.yaml` exactly as specified in docs/09-ci-cd-and-releases.md, including the
Go module and npm caches, the `go vet` / race tests / golangci-lint jobs, the web
lint/typecheck/test/build job, and the final embedded build.

It also establishes the WASM target: `GOOS=js GOARCH=wasm go build -o web/public/core.wasm
./wasm`, the `wasm_exec.js` glue copied from the active `GOROOT`, and a smoke test proving
the browser can call into the core.

## Acceptance Criteria

- [ ] `ci.yml` runs the `go`, `web`, `wasm` and `build` jobs and caches Go modules and npm.
- [ ] CI fails on unformatted Go, on a lint warning, on a type error and on an untidy
      `go.mod`.
- [ ] `make wasm` produces `web/public/core.wasm` plus a matching `wasm_exec.js`.
- [ ] A browser smoke test loads `core.wasm` and gets a correct answer from the core.
- [ ] `release.yml` triggers on `v*`, builds web and WASM, then runs GoReleaser v2 with
      `permissions: contents: write`.
- [ ] A test tag produces six archives, `checksums.txt` and a generated changelog.
- [ ] The release notes state that binaries are unsigned and explain the Gatekeeper and
      SmartScreen bypasses.
- [ ] `make release-snapshot` reproduces the release build locally.
