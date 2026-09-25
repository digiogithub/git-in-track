---
id: GIT-US-0163
type: story
title: Run impact tiers 2 and 3 outside the vault lock to fix the Pando deadlock
status: in_progress
priority: critical
parent: GIT-EP-0029
milestone: GIT-M-0015
assignees: [claude]
author: claude
labels: [server, core, agent-ok]
estimate: 3
created: 2026-09-25T12:27:40Z
updated: 2026-09-25T12:27:40Z
started: 2026-09-25T12:27:40Z
links:
  - { kind: relates_to, target: GIT-US-0161 }
---

## Description

The GIT-US-0161 benchmark re-run (docs/research/2026-09-25-spec-impact-benchmark.md §9.5) found that impact tier 3 deadlocks whenever `search.pando` is configured. `Vault.Dispatch` holds the vault mutex for `impact.query` and `impact.report`. Tier 3 calls the Pando searcher, which re-enters the same vault to resolve its candidates (`Vault.Page` in the code leg, `Vault.Item` and `Vault.Requirement` in the knowledge-base leg), and `sync.Mutex` is not re-entrant.

- **CLI.** `gintrack spec impact` with the default tiers crashed on every benchmark PR with `fatal error: all goroutines are asleep - deadlock!` (exit 2).
- **MCP.** `spec_impact` over stdio with the default tiers never answered, and it held the vault lock the whole time.
- **Companion.** `gintrack serve` takes the same `Dispatch` path.

Tier 2's Pando calls also run under the lock for up to their 10 s budget. All three tiers are the default, so this blocks the 2.1.0 release for every session with Pando configured.

## Acceptance Criteria

- [ ] A failing test first: `impact.query` and `impact.report` with a semantic searcher that resolves its candidates through the vault. It deadlocks or times out today.
- [ ] The impact backend's Pando calls (tier 2 call graph and tier 3 semantic search) run without the vault mutex held. They use an index snapshot taken under the lock, or a lock-free resolver. The vault lock is never held across a network call.
- [ ] `gintrack spec impact` (default tiers), stdio `spec_impact` and the companion's impact endpoint answer with Pando configured. Tests with the fake Pando server cover all three.
- [ ] No regression under `-race`. `make test`, `make lint` and `make wasm` pass.
- [ ] CHANGELOG records the fix, and docs/07 or docs/21 say which calls run outside the lock, if they describe locking.
