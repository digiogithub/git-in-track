---
id: GIT-T-0074
type: task
title: Build the question dialog with a cancellable answer
status: in_review
priority: medium
parent: GIT-US-0061
milestone: GIT-M-0013
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:17:08Z
updated: 2026-09-15T16:30:38Z
started: 2026-09-15T16:13:35Z
---

## Description

Add `QuestionDialog.tsx` rendering the agent's question and its offered choices, if any, with a free-text field otherwise. Submitting resumes the run with the answer; cancelling resumes it with an explicit cancelled result so the transcript records that the agent asked and got nothing, rather than the turn ending mysteriously.

## Acceptance Criteria

- [ ] A question with choices renders them; one without renders a text field.
- [ ] Submitting delivers the answer and the run continues.
- [ ] Cancelling delivers a cancelled result and the transcript shows it.
- [ ] Vitest covers both shapes, submit and cancel.
