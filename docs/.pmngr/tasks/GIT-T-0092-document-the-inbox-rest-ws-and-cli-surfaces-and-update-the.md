---
id: GIT-T-0092
type: task
title: Document the inbox REST, WS and CLI surfaces and update the CHANGELOG
status: done
priority: medium
parent: GIT-US-0071
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:17:34Z
updated: 2026-09-13T17:22:44Z
started: 2026-09-13T17:22:14Z
closed: 2026-09-13T17:22:44Z
---

## Description

Document `GET /api/v1/inbox`, `POST /api/v1/items/{id}/triage` and the `inbox.changed` WS topic in `docs/07-cli-and-api.md` (§5 for the endpoints, the WS contract section at line 2353 for the topic), and record the epic in `CHANGELOG.md` under Unreleased — the `triage` category, the `inbox` block, the new endpoints, tools and commands, plus the note that a project created before this change has no triage status and therefore no inbox until one is added to its workflow.

## Acceptance Criteria

- [x] Both endpoints and the WS topic are documented with their request and response shapes and their error codes.
- [x] The CHANGELOG entry includes the "existing projects have no triage status yet" note.
- [x] `make lint` passes.

## Notes

Verified rather than assumed. `docs/07-cli-and-api.md` §5.5 "The inbox (GIT-US-0056, ADR-033)" carries `GET /api/v1/inbox` with its full query vocabulary and response — including that `counts` and `pending` are computed over the whole queue and that an expired snooze counts as pending again — and `POST /api/v1/items/{id}/triage` with its four actions, the `If-Match` precondition, `precondition_required` (428), `412` carrying `currentRev`, `invalid_request` for a workflow status passed as a triage state, and `no_triage_status` (409). The `inbox.changed` topic is in the §5.6 contract section with its payload. The `gintrack inbox` tree is §4.17.

Two things were added in this pass.

First, **the scope of the exclusion is now written down**, because it was only in a test. A paragraph in §5.5 states that board views, sprint views, sprint candidates and sprint metrics exclude triage items unconditionally and that `Index.Query` excludes them by default — and that **search deliberately does not**. `Index.Search` takes no `Filter`; excluding there would make a submission unfindable from every surface at once, contradicting ADR-033's rule that an item is real from the moment of submission. A search hit carries no estimate, no status category and no column, so nothing reaches a planning number through it. `TestSearchStillFindsATriageItem` pins the behaviour and the doc says changing it means changing ADR-033 first. This is why GIT-T-0083's first criterion stays unticked: it is accurate, not outstanding.

Second, the migration note was made explicit in both places. `CHANGELOG.md` now says in so many words that a project created before this change has no triage status and therefore no inbox until one is added, with no migration; §5.5 repeats it for a reader coming from the API.
