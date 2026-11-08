---
id: GIT-US-0219
type: story
title: AgentPage test finds two New conversation buttons intermittently
status: backlog
priority: low
author: claude-code
labels: [web, agent-ok]
estimate: 1
created: 2026-11-09T05:30:00Z
updated: 2026-11-09T05:30:00Z
---

## Description

Bug, flaky test. `make test-web` failed once on 2026-11-09 in
`web/src/features/agent/ui/AgentPage.test.tsx` › "the conversation list" › "starts a new
conversation and switches back to a stored one" with
`TestingLibraryElementError: Found multiple elements with the role "button" and name "New conversation"`.
A rerun of the same tree passed. Nothing under `web/` had changed (the run was on the gintrack-hub
branch, Go-only changes), so the cause is timing in the test or the component, not those changes.

## Acceptance Criteria

- [ ] The test addresses one "New conversation" button unambiguously (or the component renders one), and passes 50 runs in a row (`npx vitest run --repeat 50` or equivalent).
- [ ] If the component can render two such buttons at once, that is fixed or explained.

## Notes

Recorded at the supervisor's request while running the Sluice gates for GIT-US-0214.
