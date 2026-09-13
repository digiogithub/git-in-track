---
id: GIT-US-0052
type: story
title: REST endpoints for YouTrack settings and connection test
status: done
priority: high
parent: GIT-EP-0011
milestone: GIT-M-0011
author: mcp
labels: [server, security, docs]
estimate: 5
created: 2026-09-13T13:11:54Z
updated: 2026-09-13T14:44:53Z
started: 2026-09-13T14:44:40Z
closed: 2026-09-13T14:44:53Z
---

## Description

As the web app, I want a small REST surface for reading and writing the YouTrack connection, testing it and listing YouTrack projects and custom fields, so that the browser never talks to YouTrack itself and the token never leaves the companion.

Add `p.Route("/youtrack", s.mountYouTrack)` in `internal/server/api.go` beside the existing subtrees (`:94-96`) and a new `internal/server/youtrack.go` holding a `youtrackState` modelled on `gitState` (`internal/server/git.go`). Endpoints: `GET /api/v1/youtrack/settings` returns `{url, project, fieldMap, pushComments, kbSync, hasToken, tokenSource, persisted}` and never the token itself; `PATCH /api/v1/youtrack/settings` accepts a sparse body, writes the committed half to `project.yaml` and the token to the machine-local config, and answers with `persisted bool` exactly as `handleGitSettingsPatch` does (`git.go:374-393`); `POST /api/v1/youtrack/test` calls `youtrack.Client.Me` and returns the resolved login and full name, or a problem document that distinguishes a bad token from a wrong base URL; `GET /api/v1/youtrack/projects?q=` proxies the project list for the settings autosuggest; `GET /api/v1/youtrack/fields?project=` returns the custom field settings so the field mapping UI can offer real names instead of free text.

Everything sits inside the authenticated `api.Group` behind `s.bearerAuth` (`internal/server/server.go:467`). Errors are RFC 7807 problem+json (`internal/server/problem.go`) with new codes `youtrack_unauthorized`, `youtrack_unreachable` and `youtrack_not_configured`. Because these endpoints reach a third-party host, they must accept an explicit timeout and honour request cancellation, and the handler must never echo the token in a problem `detail`.

## Acceptance Criteria

- [x] `GET|PATCH /api/v1/youtrack/settings`, `POST /api/v1/youtrack/test`, `GET /api/v1/youtrack/projects?q=` and `GET /api/v1/youtrack/fields?project=` are mounted under `/api/v1` and require the bearer token.
- [x] No response, log line or problem document ever contains the token; `hasToken` and `tokenSource` (`env` | `file` | `none`) are reported instead.
- [x] `PATCH` returns `persisted: false` when the server was started without `ConfigPath`, matching the git settings contract.
- [x] `POST /api/v1/youtrack/test` distinguishes 401, 403 and 404-from-a-missing-context-path and returns a distinct problem code for each.
- [x] `GET /api/v1/youtrack/projects` returns at most the configured page of projects and is safe to call on every keystroke (debounced client side, rate-limited server side).
- [x] `docs/07-cli-and-api.md` documents all five endpoints with request and response examples.
- [x] `go test -race ./internal/server/...` covers the handlers against an `httptest` YouTrack stub, including the token-redaction assertion.

## Notes

Shape to copy: `mountGit` (`internal/server/git.go:358`) and `mountSync` (`internal/server/sync.go:94`) are the two closest mounts; `handleGitSettingsPatch` (`git.go:374-393`) is the settings contract including `persisted`. Handlers stay thin — parse, then call into `youtrackState` — as `s.call` (`internal/server/api.go:134-156`) does for vault methods.

`maxRequestBody` is 1 MiB (`api.go:14`) and the router applies a 30 s timeout except on streams (`server.go:355`); a YouTrack call must finish well inside that or be moved to the sync engine of GIT-EP-0015.

Do NOT expose a generic YouTrack pass-through endpoint: only the specific calls the UI needs, for the same reason ADR-025 refuses to generalise the CORS proxy. Do NOT return the token even to an authenticated caller — there is no use case, and it would end up in a browser devtools log.

### What the web wave writes against

Every endpoint takes `?key=<gintrackProjectKey>`, optional when the companion serves exactly one project and a `400 invalid_request` when it serves several. `tokenSource` has a fourth value beyond the three above, `flag`, because the CLI can supply a token for one process.

Five problem codes, not three: `youtrack_not_configured` (409), and `youtrack_unauthorized`, `youtrack_forbidden`, `youtrack_not_found`, `youtrack_unreachable`, all **502**. An upstream failure is deliberately not answered with the status YouTrack returned — a 401 here would tell the browser its own session had expired — so the `code` carries the distinction.

`PATCH` is sparse with pointer fields, so clearing a value is expressible; `token` is write-only and an empty string forgets the stored credential. A patch that would not load back is refused before anything is written. `persisted` reports the token half only: the `project.yaml` half is a file by definition.

`/fields` also returns `gintrackFields`, the left-hand side of a mapping, so the settings card need not hard-code it.
