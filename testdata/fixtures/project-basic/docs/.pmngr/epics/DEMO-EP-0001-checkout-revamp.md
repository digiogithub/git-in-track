---
id: DEMO-EP-0001
type: epic
title: Checkout revamp
status: in_progress
priority: high
milestone: DEMO-M-0001
assignees: [jose]
author: jose
labels: [frontend, payments]
estimate: 13
created: 2026-07-14T08:02:11Z
updated: 2026-09-01T10:12:44Z
started: 2026-08-04T07:31:00Z
due: 2026-10-31
links:
  - { kind: relates_to, target: DEMO-M-0001, note: the beta cannot ship without it }
custom:
  risk: high
---

## Description

Rebuild the checkout flow so that a shopper can pay without creating an account
and so that returning shoppers can reuse a stored payment method.

## Goals

- A guest can complete an order in under two minutes.
- Payment credentials never touch our servers.

## Out of Scope

- Subscriptions and recurring billing.

## Notes

See [[architecture/overview]] for the component split.
