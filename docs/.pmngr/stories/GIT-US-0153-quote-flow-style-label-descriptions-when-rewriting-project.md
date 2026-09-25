---
id: GIT-US-0153
type: story
title: Quote flow-style label descriptions when rewriting project.yaml
status: done
priority: high
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-24T22:25:19Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
---

## Description

`docs/.pmngr/project.yaml` on main contains corrupted label entries:

```yaml
- {name: core, color: "#4f46e5", description: Shared Go core (model, parser: '', index): ''}
- {name: good-first-issue, color: "#16a34a", description: Small, well-scoped: '', good for newcomers: ''}
```

The original descriptions, "Shared Go core (model, parser, index)" and "Small, well-scoped, good for newcomers", held commas inside a flow mapping. Some writer re-emitted them unquoted, and YAML now reads them as extra keys. Found during GIT-US-0142 (PR #65). A likely suspect is the rewrite that updates `id_allocation.counters` on every create through MCP. If so, any project whose flow-style values contain `,` or `: ` gets corrupted over time.

## Acceptance Criteria

- [x] A failing test reproduces the corruption through the real write path, for example creating an item through the vault on a project whose label descriptions contain commas.
- [x] The writer quotes flow-style scalars that need it, or preserves the original node style. Round-tripping `project.yaml` is byte-stable apart from the counters.
- [x] `gintrack doctor` warns about label entries with unexpected keys.
- [x] This repository's `project.yaml` descriptions are repaired, as a separate data fix in the same PR.
- [x] `make test` and `make lint` pass.
