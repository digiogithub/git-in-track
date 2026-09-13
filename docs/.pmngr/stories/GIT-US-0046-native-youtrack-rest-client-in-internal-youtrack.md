---
id: GIT-US-0046
type: story
title: Native YouTrack REST client in internal/youtrack
status: done
priority: high
parent: GIT-EP-0011
milestone: GIT-M-0011
author: mcp
labels: [server, security, docs]
estimate: 13
created: 2026-09-13T13:11:10Z
updated: 2026-09-13T14:09:56Z
started: 2026-09-13T14:09:30Z
closed: 2026-09-13T14:09:56Z
---

## Description

As a developer building the YouTrack features, I want a small, typed, well-behaved Go client for the YouTrack REST API, so that every later story (import, comment push, KB sync) talks to YouTrack through one place that already handles auth, paging, rate limiting and retries.

Create the native-only package `internal/youtrack/` — it cannot live in `internal/core`, which must compile to WASM and forbids `net/http` (`internal/core/doc.go:11-12`). Model it on `internal/gitops` (native side-effect package with injected seams) and on the defensive HTTP posture of `internal/server/cors_proxy.go`. Surface: `Client`, `Options{BaseURL, Token, HTTPClient, Rate, MaxRetries, Now}`, and methods `Me(ctx)`, `Projects(ctx, query)`, `Project(ctx, key)`, `CustomFieldSettings(ctx, projectID)`, `SearchIssues(ctx, query, page)`, `Issue(ctx, id)`, `IssueLinks(ctx, id)`, `Comments(ctx, id, page)`, `AddComment(ctx, id, text)`, `Article(ctx, id)`, `CreateArticle(ctx, …)`, `UpdateArticle(ctx, …)`, `ChildArticles(ctx, id)`.

Behaviour that the reference implementation proved matters. Auth is `Authorization: Bearer perm:…` plus `Accept: application/json`; the `perm:` prefix is part of the token string. The base URL may carry a context path (`https://yt.example.com/youtrack`), so build request URLs by string concatenation — `strings.TrimRight(base, "/") + "/api/issues"` — never `url.ResolveReference`, which silently drops the context path. Every list endpoint returns a bare JSON array, never an envelope. Paging uses `$top`/`$skip` with `$top` between 100 and 200 and an explicit `order by:` in the query, without which YouTrack re-orders between pages and rows are lost or duplicated; a short page means exhaustion. A single shared token-bucket limiter at 5 req/s (`golang.org/x/time/rate` style, or a hand-rolled next-slot bucket) throttles all concurrent callers. Retry 429, 500, 502, 503, 504 and transport errors up to 4 times, honouring `Retry-After` in both seconds and HTTP-date form, otherwise `base * 2^attempt + jitter` from 500 ms; drain the response body before retrying so the connection is reused. Errors are typed so callers can distinguish 401 (bad token), 403 (missing permission) and 404 (usually a base URL missing its context path) — that distinction is what makes the "Connect YouTrack" error message useful.

## Acceptance Criteria

- [x] `internal/youtrack.Client` exists with the methods above; `net/http` transport is injectable so tests use `httptest.Server`.
- [x] Base URLs with a context path produce correct request URLs, proved by a test pinning `https://yt.example.com/youtrack/api/users/me`.
- [x] A shared limiter caps outbound requests at a configurable rate, default 5 req/s, and is honoured across concurrent goroutines.
- [x] Retries cover 429 and 5xx and transport errors, respect `Retry-After` in seconds and HTTP-date form, and stop after `MaxRetries` (default 4); non-429 4xx fails on the first attempt.
- [x] Paging helpers pair `$skip` with `order by:` and stop on a short page; `$top` defaults to 100.
- [x] Typed errors `ErrUnauthorized` (401), `ErrForbidden` (403) and `ErrNotFound` (404) are returned and never contain the token.
- [x] The token never appears in any error, log line or `String()` output; a test asserts this.
- [x] `go test -race ./internal/youtrack/...` passes with `httptest`-backed fixtures and no live network access.
- [x] `make lint` passes, including `bodyclose`, `noctx` and `wrapcheck`.

## Notes

Endpoints used (see the YouTrack endpoint cheat-sheet in the planning reports): `GET /api/users/me?fields=id,login,fullName,email`, `GET /api/admin/projects?fields=id,shortName,name,archived&query=&$top=&$skip=`, `GET /api/admin/projects/{id}/customFieldSettings?fields=field(name,fieldType(id)),bundle(id),canBeEmpty`, `GET /api/issues?query=&$top=&$skip=`, `GET /api/issues/{id}`, `GET /api/issues/{id}/links`, `GET|POST /api/issues/{id}/comments`, `GET /api/articles/{id}`, `POST /api/articles`, `POST /api/articles/{id}`, `GET /api/articles/{id}/childArticles`. YouTrack has no PUT: updates are `POST` to the resource with a partial body.

Note the comments endpoint takes no `$top` in the reference CLI and truncates silently — always send one here.

Do NOT route YouTrack traffic through the CORS proxy: ADR-025 (`internal/server/cors_proxy.go`) allows only three git smart-HTTP paths and refuses private addresses by design. All YouTrack HTTP is done by the companion. Do NOT add a YouTrack SDK dependency; the standard library is enough and AGENTS.md requires justifying any new dependency.

The last criterion was verified as `golangci-lint v2.13.2 run ./internal/youtrack/...` (0 issues) rather than a whole-module `make lint`, because another package was mid-edit by a parallel agent during the pass.
