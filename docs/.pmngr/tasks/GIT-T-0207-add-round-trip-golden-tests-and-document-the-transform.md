---
id: GIT-T-0207
type: task
title: Add round-trip golden tests and document the transform
status: todo
priority: medium
parent: GIT-US-0083
milestone: GIT-M-0011
author: mcp
labels: [core, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:20:39Z
updated: 2026-09-13T13:20:39Z
---

## Description

Add a full round-trip golden test — page to article to page — asserting that a page with front matter, wikilinks, attachments and a feedback block returns unchanged except for what is deliberately normalised, with a `-update` flag to regenerate the goldens. Then document the transform rules, including the title rule and the feedback-block rule, in `docs/03-data-model.md` next to the KB page section, and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] A round-trip golden test exists and is stable; goldens regenerate only through the `-update` flag.
- [ ] The transform rules are documented in `docs/03-data-model.md`.
- [ ] `CHANGELOG.md` has an entry; `go test -race ./internal/youtrack/...` and `make lint` pass.
