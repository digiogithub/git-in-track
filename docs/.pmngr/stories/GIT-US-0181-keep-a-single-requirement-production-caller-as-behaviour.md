---
id: GIT-US-0181
type: story
title: Keep a single-requirement production caller as behaviour evidence in tier 2
status: backlog
priority: medium
author: mcp
labels: [core, agent-ok]
created: 2026-09-29T22:56:37Z
updated: 2026-09-29T22:56:37Z
---

## Description

After GIT-US-0166 (#91), the benchmark's one true tier-2 behaviour hit (P3, `GIT-SP-0003.R3`) ranks as `test-only` (#107, §10.6). #91 intended that a production caller with a single requirement marker still turns a hit into `behaviour`.

## Acceptance Criteria

- [ ] A failing test reproduces the P3 case from the replay kit.
- [ ] Tier 2 classifies the P3 hit as `behaviour` again while keeping #91's shared-name and multi-requirement rules.
- [ ] The replay shows tier-2 strict precision not lower than before #91.
