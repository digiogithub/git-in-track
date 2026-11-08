---
id: ACME-MEM-0042
type: memory
title: Customer X is invoiced quarterly
status: proposed
author: pando-ana
labels: [billing]
created: 2026-11-08T10:00:00Z
updated: 2026-11-08T10:00:00Z
links:
  - { kind: supersedes, target: ACME-MEM-0017 }
inbox:
  source: memsync
  received: 2026-11-08T10:00:00Z
memory:
  key: customer-x-invoicing
  version: 17
  content_hash: 3e45cd15f5884da1a829d36bf54cb58faa8c885ab965fbca92a7d79eabfe6092
  local_id: project/acme/customer-x-invoicing
  valid_from: 2026-11-08T10:00:00Z
  source: {kind: agent, ref: memsync/inst-1}
  x-confidence: high
---

Customer X receives one invoice per quarter, issued on the first working day.

## Evidence

Confirmed by Ana in the 2026-11-07 billing call.
