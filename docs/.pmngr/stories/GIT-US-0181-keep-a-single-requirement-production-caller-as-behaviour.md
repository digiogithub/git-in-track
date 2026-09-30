---
id: GIT-US-0181
type: story
title: Keep a single-requirement production caller as behaviour evidence in tier 2
status: done
priority: medium
assignees: [claude]
author: mcp
labels: [core, agent-ok]
created: 2026-09-29T22:56:37Z
updated: 2026-09-30T12:11:55Z
started: 2026-09-30T11:52:04Z
closed: 2026-09-30T12:11:55Z
---

## Description

After GIT-US-0166 (#91), the benchmark's one true tier-2 behaviour hit (P3, `GIT-SP-0003.R3`) ranks as `test-only` (#107, §10.6). #91 intended that a production caller with a single requirement marker still turns a hit into `behaviour`.

## Acceptance Criteria

- [x] A failing test reproduces the P3 case from the replay kit.
- [x] Tier 2 classifies the P3 hit as `behaviour` again while keeping #91's shared-name and multi-requirement rules.
- [x] The replay shows tier-2 strict precision not lower than before #91.

## Notes

Fixed in PR #112. Root cause: the P3 caller `validateItemLinks` carries two markers (R1, R3), so #91 treated it as shared, and a test caller of the changed `Inverse` made the hit test-only. A test caller is now weaker evidence than a changed verifying test, and a production caller with exactly two requirements outranks it; callers with three or more keep #91's rule. The two-marker cut-off is a heuristic: allowing any shared caller also flipped P4 `GIT-SP-0001.R2` (four markers). Managed-Pando replay of tiers 1–2: P3 R3 is behaviour again, and nothing else changed.
