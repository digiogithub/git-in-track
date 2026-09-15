---
id: GIT-T-0122
type: task
title: Write the corpus document serializer
status: done
priority: medium
parent: GIT-US-0073
milestone: GIT-M-0013
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:16Z
updated: 2026-09-15T16:44:14Z
started: 2026-09-15T15:16:21Z
closed: 2026-09-15T16:44:14Z
---

## Description

Add the serializer that turns one `core.Item` or one KB page into a corpus Markdown document: YAML front matter with `id`, `type`, `status`, `milestone`, `parent`, `project`, `updated` and a `tags` list holding the item id plus its labels, followed by the body verbatim so wikilinks survive. `tags` and `aliases` are the only front-matter keys Pando reads specially; everything else becomes searchable metadata. Keep the output byte-stable for unchanged input so the incremental exporter can skip writes.

## Acceptance Criteria

- [ ] An item and a KB page each serialise to a document with the specified front matter and an unmodified body.
- [ ] The same input produces byte-identical output on repeated calls.
- [ ] `go test -race` covers both types, an item with no labels and one with a multi-line body, using golden files.
