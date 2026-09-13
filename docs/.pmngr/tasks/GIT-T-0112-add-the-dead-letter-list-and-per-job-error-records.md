---
id: GIT-T-0112
type: task
title: Add the dead-letter list and per-job error records
status: done
priority: medium
parent: GIT-US-0067
milestone: GIT-M-0011
author: mcp
labels: [server, security, agent-ok]
estimate: 3
created: 2026-09-13T13:18:03Z
updated: 2026-09-13T14:14:35Z
started: 2026-09-13T14:14:08Z
closed: 2026-09-13T14:14:35Z
---

## Description

Keep jobs that exhaust their attempts in a bounded dead-letter list, each with an error record holding the attempt number, the classification, the redacted error text and the timestamps. Expose it for the REST API and the queue table, with an explicit clear operation; entries never expire silently. Redact credentials on the way in, so a token that leaked into an error string never reaches the record or the journal.

## Acceptance Criteria

- [x] Exhausted jobs land in the dead-letter list with their full error record and are retryable from there.
- [x] The list is bounded, and the oldest entries are dropped with a counted, logged eviction rather than silently.
- [x] A test asserts no credential ever reaches an error record.
- [x] `go test -race ./internal/syncengine/...` covers exhaustion, retry from dead letter and clearing.

## Notes

`DeadLetter()`, `RetryDeadLetter(id)` and `ClearDeadLetter()` are the surface; `Snapshot().DeadLetterEvicted` counts evictions. `RetryDeadLetter` gives the job a fresh attempt budget and keeps its last error on the record so the UI can still show what happened.
