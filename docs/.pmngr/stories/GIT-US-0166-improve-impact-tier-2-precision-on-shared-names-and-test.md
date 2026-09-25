---
id: GIT-US-0166
type: story
title: Improve impact tier 2 precision on shared names and test callers
status: todo
priority: medium
parent: GIT-EP-0029
milestone: GIT-M-0015
author: claude
labels: [core, server, agent-ok]
estimate: 5
created: 2026-09-25T12:27:40Z
updated: 2026-09-25T12:27:40Z
links:
  - { kind: relates_to, target: GIT-US-0161 }
---

## Description

On the GIT-US-0161 PR set, 22 hits came from tier 2 alone. Only 1 was a behaviour hit (5 % strict precision, 45 % counting evidence), and those hits push the worst report to 1,393 tokens, 5.9× the folder read (docs/research/2026-09-25-spec-impact-benchmark.md §9.3, §9.4). The causes:

- Pando resolves callees by name, so `CommentKind.Valid` counts as a caller of the changed `LinkKind.Valid`, and a different `add` matches as well.
- A `call:` reason from a test caller is counted as behaviour rather than as test evidence.
- A production caller that carries several requirement markers flips `test-only` hits to `behaviour`. P1 `GIT-SP-0001.R2`, `R9` and `R10` flipped even though all three are evidence only.

## Acceptance Criteria

- [ ] Callers reached only through a name that several definitions share are dropped, or the callee is pinned (for example with `code_find_symbol`) before callers are taken.
- [ ] A `call:` reason whose caller is a test counts as test evidence, not behaviour.
- [ ] A production caller carrying several markers no longer turns a `test-only` hit into `behaviour` unless the caller's own requirement is the one changed.
- [ ] Golden and table tests cover each case from the benchmark (P1, P2, P4).
- [ ] docs/03 §21 or docs/21 explain the rules. `make test` and `make lint` pass.
