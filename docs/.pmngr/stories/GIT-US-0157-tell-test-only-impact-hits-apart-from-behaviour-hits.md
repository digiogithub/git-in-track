---
id: GIT-US-0157
type: story
title: Tell test-only impact hits apart from behaviour hits
status: backlog
priority: medium
parent: GIT-EP-0025
author: mcp
labels: [core, mcp, agent-ok]
estimate: 3
created: 2026-09-24T23:18:50Z
updated: 2026-09-24T23:18:50Z
---

## Description

The GIT-US-0137 benchmark (PR #76) measured tier-1 precision at 52% for behaviour hits, or 83% when counting hits reached only through a changed verifying test. An agent should be able to tell "the code behind this requirement changed" apart from "only a test that verifies it changed".

## Acceptance Criteria

- [ ] Impact hits carry a kind, `behaviour` or `test-only`, derived from whether a changed symbol comes from an `Implements`/`trace.code` edge or only from a `Verifies`/`trace.tests` edge.
- [ ] The renderer ranks `test-only` below behaviour hits, and `--fail-on` can exclude them. The default stays unchanged.
- [ ] Golden tests are updated. docs/03 §21.11 and docs/08 are updated.
