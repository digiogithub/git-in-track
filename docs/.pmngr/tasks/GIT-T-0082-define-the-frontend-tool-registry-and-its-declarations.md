---
id: GIT-T-0082
type: task
title: Define the frontend tool registry and its declarations
status: todo
priority: medium
parent: GIT-US-0064
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:17:18Z
updated: 2026-09-13T13:17:18Z
---

## Description

Create `web/src/features/agent/tools/registry.ts` holding, per tool, a name, a description, a JSON-schema parameter object, a zod schema for runtime validation and an executor signature. Export `toolDeclarations()` producing the array sent as `RunAgentInput.tools`, built once so it stays byte-stable across turns — Pando keys its agent pool by agent name plus a hash of the declared toolset, and a shifting list would rebuild the agent every turn.

## Acceptance Criteria

- [ ] The registry exposes declarations and executors behind one typed contract.
- [ ] `toolDeclarations()` returns an identical array on repeated calls.
- [ ] Every declared parameter schema has a matching zod validator.
- [ ] Vitest covers declaration stability and schema/validator agreement.
