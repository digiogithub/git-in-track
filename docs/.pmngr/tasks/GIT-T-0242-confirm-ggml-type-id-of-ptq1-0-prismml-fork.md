---
id: GIT-T-0242
type: task
title: Confirm ggml type id of PTQ1_0 (PrismML fork)
status: triage
priority: medium
author: mcp
labels: [gguf]
created: 2026-10-16T23:10:04Z
updated: 2026-10-16T23:10:04Z
inbox:
  status: pending
  source: mcp
  received: 2026-10-16T23:10:04Z
---

alkahest-gguf uses 143 as a placeholder for PTQ1_0 (GgmlType::PTQ1_0_ID); PQ2_0=142 and Q2_0=42 are confirmed (PQ2_0 against a real Bonsai file). Block geometry of PTQ1_0 (256 elems, 56 bytes) is an estimate from 1.75 bpw. Confirm against the PrismML llama.cpp fork source or a PTQ1_0 GGUF (e.g. prism-ml/Ternary-Bonsai-2-27B-gguf).
