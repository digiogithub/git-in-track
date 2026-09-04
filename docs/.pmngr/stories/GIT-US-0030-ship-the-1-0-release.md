---
id: GIT-US-0030
type: story
title: Ship the 1.0 release
status: in_review
created: 2026-09-03T00:00:00Z
updated: 2026-09-04
author: team
priority: high
parent: GIT-EP-0007
milestone: GIT-M-0007
estimate: 3
labels: [ci, docs]
links:
  - kind: blocked_by
    target: GIT-US-0028
  - kind: blocked_by
    target: GIT-US-0029
---

## Description

As the project, I want a 1.0 that people can rely on, so that adopting git-in-track is a
reasonable decision for a team's real backlog rather than an experiment.

This is the release story: freeze the on-disk data model at `schemaVersion: 1`, publish the
compatibility promise, provide and test `gintrack migrate` from every 0.x layout, complete
the user documentation, run the release checklist from
docs/09-ci-cd-and-releases.md §9, and tag `v1.0.0`.

It also closes the dogfooding loop: this project's own backlog under `docs/.pmngr/` must
have been planned, run and retrospected in the tool, because if that was not comfortable,
we are not ready to ask anyone else to do it.

## Acceptance Criteria

- [x] The data model is frozen at `schemaVersion: 1` and the promise is documented.
- [ ] `gintrack migrate` upgrades every 0.x vault layout, with tests per version.
- [ ] The full release checklist is completed and recorded.
- [x] User documentation covers installation, both modes, teams, sync, MCP and metrics.
- [ ] `v1.0.0` publishes six archives, `checksums.txt` and a complete changelog.
- [ ] Every artifact is verified against its checksum on a clean machine per platform.
- [ ] The unsigned-binary bypass instructions are verified on macOS and Windows.
- [ ] This project's own backlog has been run in the tool through at least one full sprint.
- [ ] All Phase 0–6 milestones are closed with their stories `done` or `cancelled`.

## Notes

**The tag was deliberately not created.** This story prepares 1.0; cutting `v1.0.0` is a
maintainer action and was explicitly out of scope for the implementing agent. Nothing was
tagged, pushed or published, no workflow was triggered, and neither the tap, the bucket
nor GHCR was touched. The repository still carries **no tags at all**, which also means no
0.x release ever shipped.

**What landed.**

- `docs/12-release-readiness-1-0.md` — the evidence record: every milestone exit criterion
  and every vision goal marked satisfied / partial / not done, each with the test, file or
  observed command that proves it; §5 is the consolidated gap list; §6 is the maintainer's
  remaining work.
- `CHANGELOG.md` — the 1.0 release notes for users: what the product does, the
  compatibility promise, the known limitations, and the operational notes (metrics are
  rewritten by a rebase or squash; the container's bind address and volume mount; the two
  Actions secrets a maintainer must create first).
- `README.md` — project status, the phase table and the installation note now describe a
  prepared-but-untagged 1.0 instead of "Phase 6 planned"; a known-limitations pointer was
  added.
- `docs/09` §7/§10 — corrected `schemaVersion` to the real field name `schema`, recorded
  that no tag has ever been pushed, documented `CHANGELOG.md`'s role, and replaced the
  "until the first tag" phrasing with an accurate statement.
- `docs/07` §2.2/§2.3 — the package-manager section was stale (it promised a `brews:`
  formula, the wrong bucket repository, and "no Docker image for 1.0"); it now matches what
  ships. The non-existent `noembed` build tag was removed.
- `docs/01` §5 — a per-goal verdict pointing at doc 12; the status line no longer says
  "draft (Phase 0 planning)".
- `AGENTS.md` — the current-state paragraph no longer claims Phase 6 has not started, and
  now warns that several documented behaviours are not implemented.

`docs/11-roadmap.md` was **not** edited: `AGENTS.md` lists the roadmap as a human-only area
until 1.0. Its exit criteria are evaluated in doc 12 instead.

**Gate results** (2026-09-04, Linux/amd64, commit `c524914`): `make lint` 0 issues
(golangci-lint v2.5.0, ESLint, `tsc -b`, workflow YAML all clean); `make test` green — 515
Go test functions over 10 packages with `-race`, plus Vitest 54 files / 476 tests;
`make wasm` and `make wasm-smoke` (32/32 checks); `make build`; `goreleaser check` passes
with 2 `dockers:` deprecation warnings; `goreleaser release --snapshot --clean
--skip=publish,docker` produces 6 archives, `checksums.txt`, the cask and the Scoop
manifest, and all 6 checksums verify. The amd64 container image builds and answers
`gintrack version`; the **arm64 image fails on this host** for want of binfmt emulation
(`exec format error`) — install it with
`docker run --privileged --rm tonistiigi/binfmt --install arm64`, which is what the release
workflow does through `docker/setup-qemu-action@v3`.

**Acceptance criteria not met, and why.**

- *`gintrack migrate` upgrades every 0.x vault layout* — the command does not exist. It is
  specified as R-EVO-4 in `docs/03-data-model.md` §19 and must be built before any
  `schema: 2`. It is unnecessary today only because no 0.x release was ever published.
- *The full release checklist is completed and recorded* — the pre-tag half is completed and
  recorded in doc 12 §4. The rest cannot be done without tagging. `make lint test` was also
  not run on macOS, which the checklist requires.
- *`v1.0.0` publishes six archives, `checksums.txt` and a complete changelog* — requires the
  tag.
- *Every artifact verified against its checksum on a clean machine per platform* — requires
  published artifacts. Only the local snapshot archives were verified.
- *The unsigned-binary bypass instructions are verified on macOS and Windows* — no macOS or
  Windows host was available, and there is nothing published to download.
- *This project's own backlog has been run through at least one full sprint* — `docs/.pmngr/`
  drove every phase, but a complete plan → run → close → retro cycle is not recorded.
- *All Phase 0–6 milestones closed with their stories `done` or `cancelled`* — deliberately
  left alone. Most stories are `in_review`; only a human closes them.

**A note on `internal/core` coverage.** Milestone 1's exit criterion is ≥ 85 %; the package
measures 83.6 %. It is recorded as unmet in doc 12 §2 rather than quietly rounded up.
