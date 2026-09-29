---
id: GIT-US-0160
type: story
title: Print compact JSON from spec impact so tokens match the report
status: done
priority: low
parent: GIT-EP-0026
milestone: GIT-M-0015
assignees: [claude]
author: mcp
labels: [cli, agent-ok, good-first-issue]
estimate: 1
created: 2026-09-24T23:18:51Z
updated: 2026-09-29T18:43:46Z
started: 2026-09-25T11:15:00Z
closed: 2026-09-29T18:43:46Z
---

## Description

The GIT-US-0137 benchmark (PR #76) found that `gintrack spec impact --format json` prints indented JSON, about 45% more than the `tokens` value the report states. That value is estimated on compact JSON.

## Acceptance Criteria

- [x] `--format json` prints compact JSON by default, with `--pretty` for indented output. Alternatively, `tokens` reflects what is actually printed; document which.
- [x] A test asserts that the printed size is within the reported estimate.
- [x] docs/07 is updated.
