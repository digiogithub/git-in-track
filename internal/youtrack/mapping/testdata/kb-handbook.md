---
title: Release handbook
tags:
  - process
external:
  - system: youtrack
    id: ACME-A-7
    url: https://yt.example.com/youtrack/article/ACME-A-7
  - system: notion
    id: 41f0c8
---

# Release handbook

How we ship, in order. See [[architecture/overview]] and
[[architecture/overview|the architecture overview]] for the shape of the system,
and [[operations/runbook#Rollback]] for what to do when it goes wrong.

The [[planning/backlog-grooming]] page is not published yet, and [[GIT-US-0042]]
is a backlog item rather than a page.

![The release train](./images/train.png)

The signed checklist is [in the release pack](../assets/release-pack.pdf).

Bare issue ids such as ACME-42 are left alone: YouTrack auto-links them itself.

```md
This fenced sample documents the syntax: [[architecture/overview]] and
![alt](./images/train.png) must both survive untouched.
```

So must an inline `[[architecture/overview]]` code span.

<!-- gintrack:feedback:begin -->

---

## Feedback

<!-- gintrack:feedback:note id="fb-1a2b3c4d" anchor="sha256:1111111111111111" lines="3-3" author="Jose F. Rives Lirola" created="2026-09-13T10:00:00Z" -->
### Jose F. Rives Lirola feedback: fb-1a2b3c4d

> Line 3:
>
> How we ship, in order. See [[architecture/overview]] and

Selected: “How we ship, in order”

This paragraph should name the release manager.

<!-- gintrack:feedback:end -->
