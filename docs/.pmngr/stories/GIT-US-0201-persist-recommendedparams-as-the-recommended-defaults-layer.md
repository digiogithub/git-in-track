---
id: GIT-US-0201
type: story
title: Persist RecommendedParams as the recommended-defaults layer on pull (ALK-T-0073)
status: triage
priority: medium
author: mcp
created: 2026-10-16T23:39:49Z
updated: 2026-10-16T23:39:49Z
inbox:
  status: pending
  source: mcp
  received: 2026-10-16T23:39:49Z
---

RecommendedParams (alkahest-hf, serde JSON) and HfClient::recommended_params exist. Storing the result next to the model manifest and layering it below user overrides needs alkahest-store refs/manifests (ALK-US-0011/0017). ALK-T-0073 left open for that wiring.
