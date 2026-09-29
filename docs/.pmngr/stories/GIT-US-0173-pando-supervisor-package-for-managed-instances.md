---
id: GIT-US-0173
type: story
title: Pando supervisor package for managed instances
status: done
priority: high
parent: GIT-EP-0031
assignees: [claude]
author: mcp
labels: [server, agent-ok]
created: 2026-09-29T18:53:52Z
updated: 2026-09-29T20:58:59Z
started: 2026-09-29T18:55:16Z
closed: 2026-09-29T20:58:59Z
---

## Description

Native-only package (not `internal/core`) that runs one managed Pando instance per opted-in repository, as ADR-039 describes. First story of GIT-EP-0031; the others build on it.

## Acceptance Criteria

- [x] Writes a 0600 `.pando.toml` and token file under `<cacheDir>/pando/<key>/`, picks a free loopback port, and starts `pando mcp-server --no-stdio` with that working directory and `PANDO_CONFIG_PARENT_SEARCH=false`; nothing is written inside the repository.
- [x] Health-checks the instance with its own token and port (Pando silently changes port when busy), restarts with backoff, and marks it `failed` after 5 crashes in 10 minutes.
- [x] Holds a per-instance lock and keeps `state.json` current (pid, port, mcpUrl, tokenFile, version, state, lastError).
- [x] Table-driven tests against a fake `pando` in testdata; an optional test runs the real binary when present.

## Notes

ADR-039 open questions (detach vs die with serve, stdio vs HTTP) may adjust lifecycle details; default to "dies with serve", loopback HTTP.
