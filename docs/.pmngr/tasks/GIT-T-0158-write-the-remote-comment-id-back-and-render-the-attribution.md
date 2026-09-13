---
id: GIT-T-0158
type: task
title: Write the remote comment id back and render the attribution line
status: done
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:08Z
updated: 2026-09-13T16:00:21Z
started: 2026-09-13T15:59:51Z
closed: 2026-09-13T16:00:21Z
---

## Description

On a successful create, write the returned YouTrack comment id and url into the comment's `external` block through the vault, quoting the comment's rev so a concurrent edit is not clobbered. Add the attribution renderer: a small template with the author and the gintrack item id, configurable per project, defaulting to a single trailing line. The item id must not be wrapped in a link since YouTrack auto-links bare ids.

## Acceptance Criteria

- [x] The remote comment id and url are written back rev-guarded; a stale rev is reported, not forced.
- [x] The attribution line renders from a project-configurable template with author and item id.
- [x] The item id is emitted bare so YouTrack's auto-linking is not double-wrapped.
- [x] `go test -race ./internal/server/...` covers the write-back, the stale-rev path and template rendering.

## Notes

**The write-back does not go through the vault, because there is no vault method
that rewrites a comment's front matter.** `comment.add` appends and nothing
updates, and `internal/core` and `internal/vault` belong to other agents this
wave. `writeCommentExternal` in `internal/server/youtrackcomments.go` therefore
does the file-level equivalent that AGENTS.md documents: read, hash, parse with
`core.ParseComment`, upsert the `external` entry, re-read and compare the hash
immediately before writing, and refuse a file that moved in between. A stale rev
is **retryable**, not forced — the next attempt pushes the text that is actually
there, which is what a comment edited mid-flight wants. The index is rebuilt and
commit-on-save is told about the one file that moved.

Replacing that function with a `comment.update` vault method is the whole of the
change when one exists; it is the only place in this package that writes a
repository file directly.

The template is `integrations.youtrack.comment_template` in project.yaml — a new
key on `config.YouTrackLink` — rendered with `text/template` against
`{Author, AuthorName, ItemID, IssueID, CommentRef}`. The default is
`_{{.Author}} · git-in-track {{.ItemID}}_` on its own line under a rule, with
the item id **bare**: YouTrack auto-links what it recognises, and a git-in-track
id is not one of those, so a Markdown link around it would be a dead link. A
template that does not parse or does not render fails the job terminally naming
the key, because retrying a broken template five times helps nobody.
