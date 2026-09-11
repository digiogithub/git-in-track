# ADR-030 — Feedback notes: a comment on an item, an anchored block inside a KB page

- **Status:** Accepted
- **Date:** 2026-09-11
- **Phase:** 5 (`GIT-EP-0006`)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-012](ADR-012-comments-as-separate-files.md), [ADR-028](ADR-028-retro-participants-name-themselves-and-comment-in-front-matter.md)

## Context

Reviewing a story or a design page means pointing at a sentence and saying something
about it. The web app had no way to do that: a comment was a free-text box under the
item, detached from the text it discussed, and a knowledge-base page had no comments at
all. Reviewers pasted quotes by hand, and an agent reading the result could not tell
which paragraph a remark was about, nor whether the paragraph had since been rewritten.

Separately, every comment the web app posted was attributed to the placeholder handle
`me`: the provider defaulted the author, and the companion wrote what it was given. The
product has no accounts (ADR-028), but a repository does know who its user is — git's
`user.name` and `user.email` — and that is the identity every commit of the same person
already carries.

## Decision

**1. A reviewer collects notes in feedback mode, and saves them at once.** The web app
lets the reader select text on an item's description or on a KB page, write a note about
it, and repeat. The notes are kept in the browser (`localStorage`, per item or page) until
they are saved, so a reload or a closed tab loses nothing.

**2. On an item, saved feedback is one ordinary comment.** Its body quotes each selected
passage with its line range and follows it with the note. No new file kind, no new front
matter on the item: the comment thread of ADR-012 already merges cleanly and is what
agents already read.

**3. On a KB page, saved feedback is a block at the end of the page itself.** A page has
no thread, and the feedback is most useful next to the text. The block is delimited by
HTML-comment markers, so renderers show only a `## Feedback` section; each note carries its
line range, its author and an **anchor** — the hash of the referenced lines — and repeats
those lines verbatim so a reader sees what was meant (docs/03 §14.4).

**4. Feedback dies with its text.** Every write of a page through the core prunes the
block: a note whose anchored text can no longer be found is removed, one whose text only
moved is re-pointed at its new lines. The rule lives in `internal/core`, so the browser
(WASM) and the companion apply the same one.

**5. A human write that names nobody takes the repository's git identity.** The companion
resolves `user.name`/`user.email` (or the configured overrides) of the repository an item
or page lives in; the comment's handle is derived from the name, and the new optional
front-matter keys `author_name` and `author_email` record the identity itself. The web app
stops sending `me`, and a `me` from an older build is treated as "nobody named".

## Consequences

- A page with feedback changes on disk, and so shows in `git diff` and in a commit made on
  save. That is intended: review is part of the page's history.
- Editing a page outside the app (an editor, another tool) does not prune until the next
  write through the core. A stale note is harmless — its anchor simply no longer matches —
  and disappears on that write.
- Line numbers are those of the page body, after the front matter. A client that computes
  them against the whole file would anchor to the wrong lines; the server refuses a range
  outside the content, but not one that is merely shifted.
- Comments gain two optional keys. Readers that do not know them preserve them as unknown
  keys, so older builds round-trip the files unchanged.
- In browser-only mode the identity comes from the repository's `.git/config` (the global
  git configuration is out of reach of a browser tab), then from the git settings of the
  workspace.
