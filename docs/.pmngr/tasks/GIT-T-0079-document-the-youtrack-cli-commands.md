---
id: GIT-T-0079
type: task
title: Document the youtrack CLI commands
status: todo
priority: medium
parent: GIT-US-0058
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:17:15Z
updated: 2026-09-13T13:17:15Z
---

## Description

Document `gintrack youtrack connect` and `gintrack youtrack status` in `docs/07-cli-and-api.md` with their flags, the token source precedence, the exit codes and an example of connecting from a script using the environment variable. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Both commands are documented with flags, precedence and exit codes.
- [ ] The example uses `GINTRACK_YOUTRACK_TOKEN` and never shows a token being passed on a command line.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.
