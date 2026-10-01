---
id: GIT-US-0199
type: story
title: "Bug: accepting or rejecting an inbox item fails when workflow.transitions omits triage"
status: done
priority: high
assignees: [claude-code]
author: mcp
labels: [bug, inbox, core]
created: 2026-09-30T21:27:14Z
updated: 2026-09-30T21:39:53Z
started: 2026-09-30T21:37:39Z
closed: 2026-09-30T21:39:53Z
---

## Description

In a project whose `project.yaml` declares `workflow.transitions` without a `triage:` key, every triage decision that changes status fails. Example through MCP `triage_inbox_item` (accept, status backlog):

`validation_failed triage GIT-T-0239: GIT-T-0239: transition triage -> backlog is not allowed`

`core.ValidateTransition` checked `Transitions[from]` for a triage-category `from`, found no key, and returned `W-WORKFLOW-TRANSITION`; `FileStore.checkTransition` escalated it to `TransitionError`. Accept and reject were therefore impossible, contradicting docs/03 §6.4 R-INBOX-7 ("work leaves triage by being accepted"). Tests missed it because `DefaultWorkflow` declares no transitions.

## Acceptance Criteria

- [x] A triage-category status with no key in `workflow.transitions` may move to any declared status.
- [x] An explicit `triage: [...]` entry is still enforced; moving into triage follows the normal rules.
- [x] Core and vault tests cover accept and reject on a project with declared transitions lacking a triage key.
- [x] docs/03 §6.1 and §6.4 state the rule; CHANGELOG has a Fixed entry.

## Notes

Workaround applied to this repository's own `project.yaml`: `triage: [backlog, todo, cancelled]`.
