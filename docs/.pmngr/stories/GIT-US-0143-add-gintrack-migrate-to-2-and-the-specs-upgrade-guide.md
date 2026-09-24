---
id: GIT-US-0143
type: story
title: Add gintrack migrate --to 2 and the specs upgrade guide
status: in_review
priority: medium
parent: GIT-EP-0022
milestone: GIT-M-0015
author: mcp
labels: [cli, docs, agent-ok]
estimate: 3
created: 2026-09-24T15:29:50Z
updated: 2026-09-24T22:31:00Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
---

## Description

ADR-037 raises the project schema to 2 implicitly on the first spec construct, and binaries up to 2.0.1 may still write to a schema-2 project. Teams need two things:
- an explicit way to upgrade: `gintrack migrate --to 2`, which is listed as not implemented in the Known limitations;
- an upgrade guide beyond the CHANGELOG entry, covering binaries, the web build and CI.

Blocked by GIT-US-0105.

## Acceptance Criteria

- [x] `gintrack migrate --to 2` raises `schema` in one reviewable write. It supports `--dry-run` and `--json` and is idempotent.
- [x] Downgrading is refused with a clear message.
- [x] An upgrade guide under docs/ explains: which projects need schema 2, what older binaries do, and the order to upgrade the binaries, the web app and CI before creating the first spec.
- [x] The CHANGELOG "Known limitations" and docs/07 are updated.
- [x] Tests are table-driven. `make test` and `make lint` pass.

## Notes

Flagged as unowned by the GIT-US-0105 implementer.
