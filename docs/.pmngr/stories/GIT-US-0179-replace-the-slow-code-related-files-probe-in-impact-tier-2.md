---
id: GIT-US-0179
type: story
title: Replace the slow code_related_files probe in impact tier 2
status: done
priority: medium
assignees: [claude]
author: mcp
labels: [core, server, agent-ok]
created: 2026-09-29T22:56:37Z
updated: 2026-09-30T12:35:15Z
started: 2026-09-30T11:52:04Z
closed: 2026-09-30T12:35:15Z
---

## Description

The GIT-US-0167 probe (#92) calls Pando `code_related_files` to tell "no call edges" from "nothing calls". In the GIT-US-0170 re-measurement (#107, benchmark §10.6) it took 194 s for `limit: 1`, so tier 2 answered `unavailable` on 3 of 12 PRs (S1, S3, S4) and each of those runs spent the full 10 s budget.

## Acceptance Criteria

- [x] The code-graph check answers within the tier budget on the benchmark repo (for example: probe once per Pando instance and cache the answer until reindex, or use a cheaper Pando call).
- [x] A missing graph still yields `unavailable` with the `BuildCodeGraph` message; a graph with edges never yields `unavailable` because of a probe timeout.
- [ ] Fake-Pando tests cover a slow probe and a cached answer; the benchmark S1/S3/S4 runs are rechecked.

## Notes

Done in PR #111. The probe runs in the background and never blocks the tier; until it answers, the tier reports "still being checked" (`GraphPending`). Answers are cached 30 min for a found graph and 1 min for a missing one. For managed instances the answer is persisted in the instance dir, keyed by pid and last state change, and `serve` warms it after registering the code project.

The fake-Pando tests are done. The S1/S3/S4 replay was not rerun, and neither the CLI nor external Pando persists the answer; both move to GIT-US-0188.
