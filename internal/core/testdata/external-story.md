---
id: ACME-US-0077
type: story
title: Mirror the tracker
status: todo
priority: high
created: 2026-09-01T09:00:00Z
updated: 2026-09-02T10:30:00Z
links:
  - { kind: blocked_by, target: ACME-US-0042 }
external:
  - system: YouTrack
    id: "  PRJ-42  "
    url: https://youtrack.example.com/issue/PRJ-42
    key: PRJ
    synced_at: 2026-09-02T10:29:00Z
  - system: youtrack
    id: PRJ-42
    url: https://youtrack.example.com/issue/PRJ-42-duplicate
  - { system: plane, id: 9f2b1c7d }
  - system: some-future-tracker.v2
    id: "abc/def"
labels: [core, sync]
---

## Description

An item that mirrors the same work in three systems.
