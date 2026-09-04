---
id: DEMO-T-0001
type: task
title: Add address validation
status: in_review
priority: high
parent: DEMO-US-0001
assignees: [jose]
author: marta
labels: [backend]
effort: 6
spent: 5
created: 2026-08-27T11:20:40Z
updated: 2026-09-02T16:03:19Z
started: 2026-08-29T07:55:00Z
links:
  - { kind: blocks, target: DEMO-US-0001 }
---

## Description

Validate postal addresses against the provider's API before an order is placed,
with a 10 minute cache and a bounded retry.

## Acceptance Criteria

- [x] Unknown provider fields are ignored.
- [ ] Unit tests cover the happy path, HTTP 500 and a malformed response.

## Notes

The provider rate-limits us to 10 requests per second.
