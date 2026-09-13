---
id: GIT-T-0127
type: task
title: Document the import tool and command
status: todo
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:18:23Z
updated: 2026-09-13T13:18:23Z
---

## Description

Document `import_youtrack_issues` in the `docs/08-mcp-server.md` §4 tool table and `gintrack youtrack import` in `docs/07-cli-and-api.md` §4, with an example invocation for each. While in `docs/07`, fix the known drift at lines 896 to 916, which lists 12 tool names and says "six write tools" when there are 13 and 7. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Both surfaces are documented with examples in their respective sections.
- [ ] The tool count drift in `docs/07-cli-and-api.md` is corrected.
- [ ] `CHANGELOG.md` has an entry and `make lint` passes.
