---
id: GIT-US-0134
type: story
title: Install a spec impact git hook from the CLI
status: in_review
priority: low
parent: GIT-EP-0028
milestone: GIT-M-0015
author: claude
labels: [cli, git, agent-ok]
estimate: 3
created: 2026-09-24T12:11:59Z
updated: 2026-09-24T22:04:41Z
links:
  - { kind: blocked_by, target: GIT-US-0125 }
---

## Description

As a developer, I want the impact gate to run before I push, so I learn about failing or suspect requirements before CI does.

## Acceptance Criteria

- [x] `gintrack spec hook install|uninstall [--hook pre-push]` writes or removes a hook script that runs `gintrack spec impact --since <upstream> --fail-on failing,suspect`; an existing foreign hook is never overwritten without `--force`.
- [x] Works for git and system-git; for jj repositories the command explains the equivalent and exits cleanly.
- [x] Tests on a temporary repository; docs/07 documents the command.

## Notes

Decision 6 of GIT-T-0238.
