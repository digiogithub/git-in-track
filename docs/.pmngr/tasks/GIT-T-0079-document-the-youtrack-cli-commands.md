---
id: GIT-T-0079
type: task
title: Document the youtrack CLI commands
status: done
priority: medium
parent: GIT-US-0058
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:17:15Z
updated: 2026-09-13T14:43:33Z
started: 2026-09-13T14:43:14Z
closed: 2026-09-13T14:43:33Z
---

## Description

Document `gintrack youtrack connect` and `gintrack youtrack status` in `docs/07-cli-and-api.md` with their flags, the token source precedence, the exit codes and an example of connecting from a script using the environment variable. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Both commands are documented with flags, precedence and exit codes.
- [x] The example uses `GINTRACK_YOUTRACK_TOKEN` and never shows a token being passed on a command line.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

`docs/07-cli-and-api.md` §4.15. The scripted example exports `GINTRACK_YOUTRACK_TOKEN` from a secret store, with a note that a token on a command line lands in the shell history and in every process listing on the machine; the second example pipes it in. Exit codes 0/1/2/3/4 are spelled out, including which of 401, 403 and 404 a failed probe reports.

`make lint` passes with 0 issues. The box stays unticked for `CHANGELOG.md` only: that file was owned by another agent this wave and is off limits to this one.
