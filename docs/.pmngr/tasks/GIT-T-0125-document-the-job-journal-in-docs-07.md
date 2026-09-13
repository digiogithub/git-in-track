---
id: GIT-T-0125
type: task
title: Document the job journal in docs/07
status: done
priority: medium
parent: GIT-US-0070
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:18:23Z
updated: 2026-09-13T17:22:28Z
started: 2026-09-13T17:22:03Z
closed: 2026-09-13T17:22:28Z
---

## Description

Document the journal in `docs/07-cli-and-api.md`: its location under the cache directory, its JSON shape, the retention window, the fact that it is derived data safe to delete at any time, and that it never contains credentials. Note the handler idempotence contract that replay implies. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] The location, format, retention and delete-safety are documented.
- [x] The handler idempotence requirement is stated explicitly.
- [x] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

Landed as a new `##### The journal (GIT-US-0070)` subsection of `docs/07-cli-and-api.md` §4.1, under the background job engine. It states the location (`jobs.json` inside the configured `index.cacheDir`, no file at all without one), the atomic write and the 500 ms coalescing window, a worked JSON example of the whole document, the `version` field and what an unknown or corrupt version does (moved aside, never fatal), the retention window (`sync.engine.retention`, a week by default), and that the file is derived data safe to delete at any time with only queued work — never user data — lost.

One correction to what the CHANGELOG claimed: the journal **does** store each job's payload, because replaying a job means running it with the arguments it was given. What it never stores is item content or a credential — a token is read at dispatch time from the `0600` machine configuration (ADR-032) and error messages are redacted before being recorded. The docs and the CHANGELOG entry now both say this accurately rather than implying the payload is dropped.

The idempotence contract is stated explicitly in its own paragraph, tied to the reason: a job that was running when the process died is re-queued with its attempt count intact, because the engine cannot know whether the handler finished.
