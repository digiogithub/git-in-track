---
id: GIT-EP-0013
type: epic
title: Push comments and feedback to YouTrack
status: done
priority: medium
milestone: GIT-M-0011
author: mcp
labels: [core, server, web, mcp]
created: 2026-09-13T13:07:34Z
updated: 2026-09-15T19:37:29Z
started: 2026-09-15T19:37:09Z
closed: 2026-09-15T19:37:29Z
---

## Description

When an item is linked to a YouTrack issue, a comment or a feedback note (which is a comment, ADR-030) written in git-in-track can be published to that issue. Manually from the comment's menu ("Send to YouTrack") or automatically for every new comment when the project opts in. The push goes through the sync engine, never inline in the write path, and the comment records the YouTrack comment id so it is never sent twice and so a later edit can update it.

## Acceptance Criteria

- [ ] Comment front matter accepts `external` with the YouTrack comment id; documented.
- [ ] "Send to YouTrack" action on a comment and on a feedback note; shows pending / sent / failed state.
- [ ] Project setting `push_comments: manual | auto`; auto enqueues on every comment write (vault `comment.add` seam and MCP `AfterWrite`).
- [ ] Body is sent as Markdown with a trailing attribution line (author, gintrack item id) configurable by template.
- [ ] Editing an already pushed comment updates the YouTrack comment; deleting does not delete remotely.
- [ ] MCP tool `push_comment_to_youtrack`; CLI `gintrack youtrack push-comments <item>`.

## Notes

YouTrack: `POST /api/issues/{id}/comments {text}`, edit `POST .../comments/{cid}`.
