---
id: GIT-T-0127
type: task
title: Document the import tool and command
status: done
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:18:23Z
updated: 2026-09-13T16:40:03Z
started: 2026-09-13T16:39:50Z
closed: 2026-09-13T16:40:03Z
---

## Description

Document `import_youtrack_issues` in the `docs/08-mcp-server.md` §4 tool table and `gintrack youtrack import` in `docs/07-cli-and-api.md` §4, with an example invocation for each. While in `docs/07`, fix the known drift at lines 896 to 916, which lists 12 tool names and says "six write tools" when there are 13 and 7. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Both surfaces are documented with examples in their respective sections.
- [x] The tool count drift in `docs/07-cli-and-api.md` is corrected.
- [x] `CHANGELOG.md` has an entry and `make lint` passes.

## Notes

`docs/08` was already done by the previous wave. This closes the `docs/07` half: §4.15 gained the `import` subsection with a dry run and a real run, and the `gintrack mcp --list-tools` block now prints all twenty-two names with the correct split — seven read-only, fifteen writes — in both places that stated it.

The import REST route was also documented as **always asynchronous**: there is no request that makes `POST /api/v1/youtrack/import` answer a finished import, so a client needs no branch for a synchronous result.
