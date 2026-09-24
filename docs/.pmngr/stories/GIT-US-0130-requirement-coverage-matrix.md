---
id: GIT-US-0130
type: story
title: Requirement coverage matrix
status: in_review
priority: high
parent: GIT-EP-0027
milestone: GIT-M-0015
author: claude
labels: [web, agent-ok]
estimate: 5
created: 2026-09-24T12:11:34Z
updated: 2026-09-24T21:34:34Z
links:
  - { kind: blocked_by, target: GIT-US-0116 }
  - { kind: blocked_by, target: GIT-US-0127 }
---

## Description

As a maintainer, I want a matrix of requirements × tests with the status of each, so I see at a glance what is untested, failing or suspect. Mandatory for the milestone (decision 5 of GIT-T-0238).

## Acceptance Criteria

- [x] A coverage view (`/p/$project/specs/coverage`) renders requirements as rows and tests as columns (grouped by file), each cell showing the last result; each row shows the computed status `untested`/`passing`/`failing`/`suspect`.
- [x] Summary counts per status and per spec; filters by spec and status; rows link to the requirement detail.
- [x] Large matrices stay usable (virtualised rows, sticky headers) and it works at phone width with horizontal scroll inside the matrix only.
- [x] Browser-only mode shows `unavailable` with a hint to run the companion.
- [x] Vitest tests for status rendering and filters; docs/05 updated with a screenshot description.

## Notes

Data from `GET .../coverage`.
