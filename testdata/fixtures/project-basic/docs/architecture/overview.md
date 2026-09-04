# Architecture overview

The demo shop is a three-tier application: a React storefront, a Go API and a
PostgreSQL database. Payments are delegated to an external provider, which is
why [[DEMO-US-0002]] is scoped to tokens and never to card numbers.

## Components

| Component | Responsibility |
| --- | --- |
| storefront | Rendering, cart state, checkout flow |
| api | Orders, pricing, inventory |
| payments | Provider tokens, refunds |

## Notes

This page exists so that the fixture has more than one knowledge-base page.
