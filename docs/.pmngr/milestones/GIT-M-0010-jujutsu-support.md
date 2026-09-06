---
id: GIT-M-0010
type: milestone
title: Jujutsu (jj) support
status: in_progress
priority: high
author: team
created: 2026-09-06T00:00:00Z
updated: 2026-09-06T00:00:00Z
---

## Description

Post-1.0 milestone for `GIT-EP-0010`: a Jujutsu repository is a first-class
repository, read and written through jj rather than behind its back.

## Exit criteria

- [ ] A jj repository is detected, colocated or not, and never reported as a
      detached-HEAD git repository.
- [ ] Commit on save in a jj repository goes through jj and leaves no orphaned
      working-copy commit; a scripted test proves the change graph stays intact.
- [ ] Status, dirty set, bookmark and ahead/behind are read with jj and match
      what `jj st` and `jj log` report.
- [ ] Fetch, integrate and push work through jj, and a conflict can be resolved
      from the UI without a terminal.
- [ ] The `Backend` interface carries no git-only concept that jj must fake.
