---
id: GIT-US-0067
type: story
title: Retry policy, backoff and failure handling in the sync engine
status: backlog
priority: high
parent: GIT-EP-0015
milestone: GIT-M-0011
author: mcp
labels: [server, performance]
estimate: 5
created: 2026-09-13T13:13:09Z
updated: 2026-09-13T13:13:09Z
---

## Description

As a user syncing against a rate-limited, occasionally flaky third-party API, I want failed jobs to retry sensibly and give up visibly, so that a transient 429 fixes itself and a real failure is something I can read and act on rather than a silent drop.

Layer a retry policy onto the engine from the previous story: a handler error is classified as retryable or terminal, retryable errors return the job to the queue with an increasing delay, and a job that exhausts its attempts moves to a failed list that keeps its error and is not garbage-collected. The delay comes from exponential backoff with jitter, seeded on the shape already used in `internal/gitops/sync.go` (`defaultBackoff` `:755`, `sleep(ctx, d)` `:766`, the push-retry loop `:661-684`) — 500 ms, 1.5 s, 4 s, capped — except when the error carries a `Retry-After`, which always wins. `internal/youtrack` returns typed errors, so a 429 or 5xx is retryable while 401, 403 and 404 are terminal and must fail on the first attempt: retrying a bad token five times only gets the user locked out faster.

Each job carries an error record — attempt number, classification, the error text with any token redacted, and the time of the next attempt — so the queue table of the settings story and the REST API can show why something is stuck. A dead-letter list is bounded and explicit: entries stay until retried or cleared, never expire silently.

## Acceptance Criteria

- [ ] `RetryPolicy{MaxAttempts, Base, Max, Jitter}` is configurable, defaulting to 5 attempts with 500 ms / 1.5 s / 4 s growth capped at a maximum.
- [ ] A `Retry-After` carried by the error overrides the computed backoff, in both seconds and HTTP-date form.
- [ ] Errors are classified: 429 and 5xx and transport errors retry; 401, 403, 404 and validation errors fail immediately without consuming attempts.
- [ ] A job that exhausts its attempts lands in a bounded dead-letter list with its last error, attempt count and timestamps preserved.
- [ ] Every stored error string is redacted of credentials; a test asserts a token never reaches the error record.
- [ ] Retry delays are driven by the injectable clock, so `go test -race ./internal/syncengine/...` runs without wall-clock sleeps.
- [ ] A failing handler under concurrency never loses a job nor duplicates one; a table-driven test covers exhaustion, terminal failure and recovery on the second attempt.

## Notes

Two layers of retry existed in the reference YouTrack CLI and both are wanted here: the HTTP client retries a single request (that belongs to `internal/youtrack`), and the job queue retries a whole job. Do not duplicate the transport-level retry in the engine — classify what the client hands back.

`internal/gitops/sync.go:661-684` is the closest existing retry loop and the intended style reference.

Do NOT retry a job whose handler wrote a partial result without making the handler idempotent first — handlers are responsible for their own idempotence, and the importer story of GIT-EP-0012 relies on the `external` index for exactly that. Do NOT let the dead-letter list grow unbounded in memory.
