---
id: GIT-T-0142
type: task
title: Add search.pando configuration with a loopback guard
status: done
priority: medium
parent: GIT-US-0077
milestone: GIT-M-0013
author: mcp
labels: [server, security, agent-ok]
estimate: 2
created: 2026-09-13T13:18:46Z
updated: 2026-09-15T16:59:56Z
started: 2026-09-15T15:20:11Z
closed: 2026-09-15T16:59:56Z
---

## Description

Add a `Search` section with a nested `Pando` block (`url`, `project_id`) to `internal/config/config.go`, wired through `config.Resolve` and validated in `internal/config/validate.go`. Because Pando's MCP HTTP transport has no authentication, validation rejects any host that is not loopback unless an explicit opt-in field is set. Default `project_id` to the value Pando itself would derive from the repository path by its own sanitisation rule: keep alphanumerics, underscore and hyphen, map slashes, backslashes, spaces and dots to underscores.

## Acceptance Criteria

- [ ] The section round-trips through parse and save and is validated.
- [ ] A non-loopback URL is rejected without the opt-in and accepted with it.
- [ ] The derived default `project_id` matches Pando's sanitisation for several sample paths.
- [ ] `go test -race ./internal/config/...` covers all three.
