---
id: GIT-EP-0028
type: epic
title: Spec impact CI gate and git hook
status: backlog
priority: medium
milestone: GIT-M-0015
author: claude
labels: [ci, cli]
created: 2026-09-24T12:08:29Z
updated: 2026-09-24T12:08:29Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Rerun the impact query deterministically outside the agent: `gintrack spec impact --since <ref> --fail-on failing,suspect` as a pull-request check in `.github/workflows/ci.yml`, and as an optional local git hook installed by the CLI.

## Acceptance Criteria

- [ ] A PR that makes a traced requirement failing or suspect fails the check with a readable report.
- [ ] The git hook can be installed and removed from the CLI.
- [ ] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §7 (decision 6). `release.yml` and `.goreleaser.yaml` are human-only and out of scope.
