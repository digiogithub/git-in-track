---
links:
  - {target: ACME-SP-0003.R2, kind: implements}
  - kind: modifies
    target: ACME-SP-0003.R10
    note: tightens the allocation rule
  - { kind: implements, target: WEB/WEB-SP-0001.R4 }
  - { kind: relates_to, target: ACME-SP-0003 }
  - { kind: duplicated_by, target: ACME-US-0050 }
title: Allocate ids by index scan
id: ACME-US-0044
type: story
status: in_progress
created: 2026-09-24T12:00:00Z
updated: 2026-09-24T13:00:00Z
---

## Description

Implement the allocation requirement.
