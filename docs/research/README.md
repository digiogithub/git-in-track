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
| [Pando gap analysis: AG-UI server](./2026-09-13-pando-gap-agui-server.md) | Pando `internal/agui`, `internal/api`, `cmd/agui_serve.go` | `PANDO-EP-0002` … `PANDO-EP-0004`, `GIT-US-0049` |
| [Pando gap analysis: TypeScript SDK and AG-UI client](./2026-09-13-pando-gap-sdk-client.md) | Pando `sdk/typescript`, `examples/copilotkit` | `PANDO-EP-0001`, `GIT-US-0053` |
| [Pando gap analysis: search fidelity and plan fit](./2026-09-13-pando-gap-search-and-fit.md) | Pando `internal/rag`, `internal/mesnada/server`; GIT stories 0049–0091 | `PANDO-EP-0005` … `PANDO-EP-0007`, `GIT-EP-0019` |

The three Pando gap-analysis pages were written on the evening of 2026-09-13 against Pando `d805b77a`; their "Proposed backlog items" became the epics of the `PANDO` project (milestones `PANDO-M-0001`, `PANDO-M-0002`), and their "Decisions for the user" were settled the same day: one `agui-serve` per repository, `[AGUI] Tools` allow-list first and named profiles later, thread lifecycle funded in Pando, and `@pando-ai/sdk/agui` as a direct dependency.

Sections marked "from API knowledge, not from the CLI code" in the YouTrack page were not
verified against a live instance; confirm them before relying on them in a story.
