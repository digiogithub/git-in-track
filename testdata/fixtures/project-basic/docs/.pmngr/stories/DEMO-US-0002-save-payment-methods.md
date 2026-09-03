---
id: DEMO-US-0002
type: story
title: Save payment methods
status: todo
priority: medium
parent: DEMO-EP-0001
milestone: DEMO-M-0001
assignees: [marta]
author: marta
labels: [backend, payments]
estimate: 5
created: 2026-08-20T11:15:00Z
updated: 2026-08-30T16:20:10Z
links:
  - { kind: relates_to, target: DEMO-US-0001 }
custom:
  risk: low
---

## Description

As a returning shopper,
I want my card to be remembered as a provider token,
so that I do not retype it on every order.

## Acceptance Criteria

- [ ] Tokens are stored per customer, never card numbers.
- [ ] A shopper can delete a stored method from the account page.
- [ ] Deleting a method does not break historical orders.

## Notes

Blocked on the provider enabling tokenisation for our test account.
