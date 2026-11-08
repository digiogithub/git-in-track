---
type: memory
id: ACME-MEM-0042
status: proposed
title: Customer X is invoiced quarterly
memory:
  x-confidence: high
  source: {ref: "memsync/inst-1", kind: agent}
  version: 17
  local_id: project/acme/customer-x-invoicing
  key: customer-x-invoicing
  content_hash: 3e45cd15f5884da1a829d36bf54cb58faa8c885ab965fbca92a7d79eabfe6092
  valid_from: 2026-11-08T10:00:00Z
author: pando-ana
labels: [billing]
created: 2026-11-08T10:00:00Z
updated: 2026-11-08T10:00:00Z
links:
  - {kind: supersedes, target: ACME-MEM-0017}
inbox:
  source: memsync
  received: 2026-11-08T10:00:00Z
---

Customer X receives one invoice per quarter, issued on the first working day.

## Evidence

Confirmed by Ana in the 2026-11-07 billing call.
