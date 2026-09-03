---
id: DEMO-US-0001
type: story
title: Guest checkout
status: in_progress
priority: high
parent: DEMO-EP-0001
milestone: DEMO-M-0001
assignees: [marta, jose]
author: jose
labels: [frontend]
estimate: 8
effort: 20
spent: 11.5
created: 2026-08-19T09:04:02Z
updated: 2026-09-01T10:45:12Z
started: 2026-08-28T08:10:00Z
due: 2026-09-15
links:
  - { kind: blocked_by, target: DEMO-T-0001 }
  - { kind: relates_to, target: DEMO-US-0002 }
custom:
  customer: northwind
  risk: medium
x-legacy-tracker: SHOP-4711
---

## Description

As a shopper without an account,
I want to complete an order with my email address alone,
so that I do not have to register before buying.

## Acceptance Criteria

- [x] The cart offers "continue as guest" next to "sign in".
- [x] An order confirmation is sent to the email address given at checkout.
- [ ] A guest order can be claimed later by registering with the same email.
- [ ] Address validation errors are shown inline, field by field.

## Technical Notes

The guest session is a signed, `SameSite=Lax` cookie that expires in 30 minutes.

## Notes

Northwind is the pilot customer for this flow.
