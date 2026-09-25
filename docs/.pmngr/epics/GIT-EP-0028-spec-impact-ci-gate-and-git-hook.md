---
id: GIT-EP-0028
type: epic
title: Spec impact CI gate and git hook
status: done
priority: medium
milestone: GIT-M-0015
author: claude
labels: [ci, cli]
created: 2026-09-24T12:08:29Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Rerun the impact query deterministically outside the agent: `gintrack spec impact --since <ref> --fail-on failing,suspect` as a pull-request check in `.github/workflows/ci.yml`, and as an optional local git hook installed by the CLI.

## Acceptance Criteria

- [x] A PR that makes a traced requirement failing or suspect fails the check with a readable report.
- [x] The git hook can be installed and removed from the CLI.
- [x] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §7 (decision 6). `release.yml` and `.goreleaser.yaml` are human-only and out of scope.
