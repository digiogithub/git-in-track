---
id: GIT-US-0001
type: story
title: Scaffold the monorepo and build toolchain
status: done
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T21:17:39Z
closed: 2026-09-03T21:17:39Z
started: 2026-09-03T20:42:39Z
author: team
priority: critical
parent: GIT-EP-0001
milestone: GIT-M-0001
estimate: 3
labels: [core, ci, good-first-issue]
links: []
---

## Description

As a contributor, I want a repository that already has the agreed layout, module path and
build entry points, so that I can add code without inventing structure and without every
newcomer choosing a different convention.

This creates the monorepo skeleton described in the architecture brief: `cmd/gintrack`,
`internal/core`, `internal/server`, `internal/watcher`, `internal/gitops`, `internal/mcp`,
`wasm/`, `web/`, `docs/`, plus `go.mod`, `Makefile` and `.goreleaser.yaml`. Each Go package
gets a `doc.go` stating its responsibility and its dependency rules, so the layout is
self-documenting from the first commit.

The web workspace is scaffolded with Vite, React 18, TypeScript, Tailwind and shadcn/ui,
with ESLint, Prettier and a strict `tsconfig.json`. `gintrack version` is the first working
command, wired to the ldflags the release pipeline sets.

## Acceptance Criteria

- [ ] `go.mod` declares `github.com/digiogithub/git-in-track` and Go 1.23.
- [ ] Every directory from the brief's layout exists with a `doc.go` or a placeholder.
- [ ] `cmd/gintrack` builds and `gintrack version` prints version, commit and build date.
- [ ] `web/` builds with `npm ci && npm run build` and produces `web/dist`.
- [ ] `Makefile` implements `deps`, `web`, `wasm`, `build`, `test`, `lint`, `run`,
      `release-snapshot`, and `make help` lists them.
- [ ] `.golangci.yaml`, `eslint.config.js`, `.prettierrc` and `tsconfig.json` are present
      and match docs/10-development-guidelines.md.
- [ ] `README.md`, `LICENSE` (MIT), `CHANGELOG.md` and `.gitignore` exist.
