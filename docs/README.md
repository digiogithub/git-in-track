---
title: git-in-track documentation
type: page
tags: [index, planning]
---

# git-in-track documentation

This folder is the knowledge base of the git-in-track project itself. It is
also the project's *documentation folder* in git-in-track terms: the backlog
that drives development lives in [`.pmngr/`](./.pmngr/) using the exact data
model described in [03-data-model.md](./03-data-model.md).

## Reading order

| # | Document | What it covers |
|---|----------|----------------|
| 01 | [Vision and scope](./01-vision-and-scope.md) | Problem, goals, non-goals, personas, user journeys, positioning |
| 02 | [Architecture](./02-architecture.md) | Components, operating modes, shared Go core + WASM, indexing, security |
| 03 | [Data model](./03-data-model.md) | `.pmngr/` layout, front matter spec for epics/stories/tasks/milestones/comments |
| 04 | [Team repository](./04-team-repository.md) | `team.yaml`, boards, sprints, retrospectives, index snapshots, remote references |
| 05 | [Web app](./05-web-app.md) | React + Vite frontend, browser-only mode, WASM bridge, Markdown pipeline, screens |
| 06 | [Git sync](./06-git-sync.md) | Write path, sync, conflicts, credentials, real-time propagation |
| 07 | [CLI and API](./07-cli-and-api.md) | `gintrack` commands, configuration, REST + WebSocket API, package design |
| 08 | [MCP server](./08-mcp-server.md) | Tools, resources and prompts for AI agents; agent-optimized reading |
| 09 | [CI/CD and releases](./09-ci-cd-and-releases.md) | GitHub Actions on tags, GoReleaser, unsigned multi-platform artifacts |
| 10 | [Development guidelines](./10-development-guidelines.md) | Coding standards, commits, testing, local workflow, definition of done |
| 11 | [Roadmap](./11-roadmap.md) | Phases, milestones, epics, stories, estimates, risks |
| — | [ADRs](./adr/README.md) | Architecture decision records |

## Conventions

- All documentation and code comments are written in English.
- Diagrams use Mermaid fenced blocks so they render in the git-in-track KB
  viewer and on GitHub.
- Documents carry YAML front matter (`title`, `type`, `tags`) so the indexer
  can treat them like any other KB page.
- Changes to the data model require an update to `03-data-model.md` and a
  new ADR in `adr/`.
