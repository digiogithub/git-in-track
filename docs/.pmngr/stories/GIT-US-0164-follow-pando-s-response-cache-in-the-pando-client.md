---
id: GIT-US-0164
type: story
title: Follow Pando's response cache in the Pando client
status: todo
priority: high
parent: GIT-EP-0029
milestone: GIT-M-0015
author: claude
labels: [server, agent-ok]
estimate: 3
created: 2026-09-25T12:27:40Z
updated: 2026-09-25T12:27:40Z
links:
  - { kind: relates_to, target: GIT-US-0161 }
---

## Description

Pando's MCP server replaces any tool result over 15,000 bytes or 300 lines with a `[Response cached: … cache_id …]` header and a preview. The full result has to be paged with `cache_read`, and no Pando setting turns this off. Tier 3 asks for 16 chunks, and on every PR of the GIT-US-0161 benchmark the result was 16–23 KB. `internal/pando` cannot decode the stub, so tier 3 reports `unavailable` (docs/research/2026-09-25-spec-impact-benchmark.md §9.5, item 2). Any `search_semantic` page that large fails the same way.

The `unavailable` message also quotes Pando's random `cache_id`. So impact reports are not byte-for-byte deterministic (§9.6), and the tier line costs about 99 tokens.

## Acceptance Criteria

- [ ] A failing test with the fake Pando server returning a cached-response stub.
- [ ] `internal/pando` detects the `[Response cached …]` stub and pages the full result with `cache_read`, or keeps requests under the cache threshold. A result it still cannot decode is reported as `unavailable` with a short, fixed reason.
- [ ] Tier messages never embed a Pando `cache_id` or any other random value. Two runs against one index give byte-identical reports.
- [ ] `search_semantic` and workspace search benefit from the same fix, and a test covers each.
- [ ] docs/21 describes the behaviour. `make test` and `make lint` pass.
