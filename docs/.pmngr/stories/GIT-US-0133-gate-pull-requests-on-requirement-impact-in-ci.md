---
id: GIT-US-0133
type: story
title: Gate pull requests on requirement impact in CI
status: in_review
priority: medium
parent: GIT-EP-0028
milestone: GIT-M-0015
author: claude
labels: [ci, cli, agent-ok]
estimate: 3
created: 2026-09-24T12:11:59Z
updated: 2026-09-24T21:15:21Z
links:
  - { kind: blocked_by, target: GIT-US-0115 }
  - { kind: blocked_by, target: GIT-US-0125 }
---

## Description

As a maintainer, I want every pull request checked for requirements it makes failing or suspect, so the agent's own impact check is rerun deterministically before merge.

## Acceptance Criteria

- [x] `.github/workflows/ci.yml` gains a job that runs the tests with JSON/JUnit output, ingests them and runs `gintrack spec impact --since origin/main --fail-on failing,suspect` (tiers 1–2 only, no Pando in CI).
- [x] The job writes the compact report to the job summary and fails with a readable list of offending requirements.
- [x] A `make spec-check` target runs the same locally; `make lint` validates the workflow YAML.
- [x] docs/09-ci-cd-and-releases.md documents the gate and how to acknowledge a suspect requirement (Spec Delta or re-verify).

## Notes

`release.yml` and `.goreleaser.yaml` are human-only and untouched.
