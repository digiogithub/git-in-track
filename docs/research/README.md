---
title: Research notes
type: page
tags: [research]
---

# Research notes

Code reviews of external systems, written when a feature was planned. Each page is a
snapshot of what the code looked like on the date in its name: file paths and line numbers
were verified then and may have moved since. They explain *why* the epics in
[11-roadmap.md §7](../11-roadmap.md) are shaped the way they are; the stories under
`docs/.pmngr/` are the authority for what gets built.

| Page | Source reviewed | Feeds |
| --- | --- | --- |
| [git-in-track technical map](./2026-09-13-git-in-track-integration-map.md) | this repository | `GIT-EP-0011` … `GIT-EP-0019` |
| [YouTrack REST API reference](./2026-09-13-youtrack-api-reference.md) | `youtrack-cli` + JetBrains devportal | `GIT-EP-0011` … `GIT-EP-0014` |
| [Plane intake, cycles and importers](./2026-09-13-plane-intake-cycles-importers.md) | Plane monorepo (`apps/api`, `web/`) | `GIT-EP-0012`, `GIT-EP-0016`, `GIT-EP-0017` |
| [Pando AG-UI, SDK and semantic search](./2026-09-13-pando-agui-sdk-search.md) | Pando (`internal/agui`, `internal/rag`, `sdk/`) | `GIT-EP-0018`, `GIT-EP-0019` |

Sections marked "from API knowledge, not from the CLI code" in the YouTrack page were not
verified against a live instance; confirm them before relying on them in a story.
