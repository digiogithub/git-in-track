---
id: GIT-T-0125
type: task
title: Document the job journal in docs/07
status: todo
priority: medium
parent: GIT-US-0070
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:18:23Z
updated: 2026-09-13T13:18:23Z
---

## Description

Document the journal in `docs/07-cli-and-api.md`: its location under the cache directory, its JSON shape, the retention window, the fact that it is derived data safe to delete at any time, and that it never contains credentials. Note the handler idempotence contract that replay implies. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The location, format, retention and delete-safety are documented.
- [ ] The handler idempotence requirement is stated explicitly.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.
