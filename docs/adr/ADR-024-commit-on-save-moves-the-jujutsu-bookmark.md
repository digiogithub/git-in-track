# ADR-024 — Commit on save in Jujutsu records the paths and fast-forwards the bookmark

- **Status:** Accepted
- **Date:** 2026-09-06
- **Phase:** Post-1.0 (Jujutsu support, `GIT-EP-0010`)
- **Related:** [ADR-021](ADR-021-jujutsu-is-a-first-class-vcs-kind.md), [ADR-022](ADR-022-a-vcs-neutral-backend-interface.md), [ADR-023](ADR-023-jujutsu-history-from-the-git-object-store.md)
- **Implements:** `GIT-US-0041` — Write to a Jujutsu repository through jj

## Context

`GIT-US-0041` writes to a Jujutsu repository through jj. Two of jj's rules have
no git equivalent, and both of them decide what "commit on save" can mean.

**The working copy is a commit.** There is no index, so there is no staging step
that a commit can be scoped by. `CommitRequest.Paths` is nevertheless a hard
contract (AC 5 of `GIT-US-0020`): a commit covers those paths and nothing else,
because commit on save batches per backlog item and the user may be editing
several files at once, only some of them ours.

**A bookmark does not move when a commit is made.** `jj commit` records the
working-copy commit and starts a new one on top; every bookmark stays exactly
where it was. This is the point of bookmarks in jj — they are not branches — and
it is also the trap. The epic's audit found that a `git commit` behind a jj
workspace lands on `@-`, moves no bookmark, and is abandoned as an orphan by the
next jj command. Recording a commit with `jj commit` and stopping there avoids
the orphan but keeps the second half of the damage: the work is reachable from
`@` and from nothing else, so `jj git push` never publishes it, and a user who
runs `jj new` later leaves it behind with no name at all. A product that commits
on every save would build up a chain of such commits silently.

Three candidates were considered for the recording itself:

| Candidate | What it does | Why not |
|---|---|---|
| `jj commit -m … -- <paths>` | splits `@`: the named paths become the new commit, the rest stays in a fresh working copy | **chosen** |
| `jj squash -- <paths>` | moves the named paths into `@-`, amending a commit the user (or a bookmark, or a remote) may already have | rewrites history that is not ours to rewrite; a pushed `@-` becomes a divergent commit |
| `jj describe` + `jj new` | describes the whole working copy and starts a new one | cannot scope to paths at all: every unrelated edit in the tree is captured |

## Decision

**Commit on save in a Jujutsu repository is `jj commit -m <message> -- <paths>`,
followed by a fast-forward `jj bookmark move <bookmark> --to <new commit>`.**

- The paths are the whole of the commit. Everything else the user has edited
  stays in the new working-copy commit, untouched and uncommitted.
- The bookmark that is moved is the one the read half already reports as the
  line of work: the nearest bookmark among the ancestors of `@`
  (`heads(::@ & bookmarks())`). Because it is an ancestor, the move is always a
  fast-forward, and `jj bookmark move` is called **without**
  `--allow-backwards`: if it is ever not a fast-forward, jj refuses and we
  report it rather than forcing it.
- A line of work with no bookmark at all is left alone. That is a legitimate jj
  state (an anonymous branch); the commit is still reachable from `@`, and
  `Push` says plainly that jj publishes bookmarks and that one has to be
  created.
- Nothing else moves. No remote-tracking bookmark, no other bookmark, no
  `@`-rewrite beyond the split jj itself performs.

The same rule governs the merge strategy of `Integrate`: the merge commit is
created with `jj new --no-edit` and the bookmark is fast-forwarded onto it,
rather than the bookmark being re-pointed sideways.

## Consequences

- Commit on save produces work that `jj git push` publishes, which is the
  property the whole epic exists to guarantee. A graph-integrity test asserts it
  against a real repository: no change id disappears, `@` remains a commit of
  its own descending from the new commit, the graph holds no revision unreachable
  from `@` or a bookmark, the bookmark advanced exactly one commit, and a
  dry-run `jj git push` names that commit.
- Commit on save is **two** operations in jj's operation log — the commit and
  the bookmark move — so taking it back by hand is two `jj undo`s, or one
  `jj op restore`. That is visible to the user and is documented rather than
  hidden.
- The bookmark moves without the user asking, which is not what bare `jj commit`
  does. It is the honest reading of what "commit on save" was configured to
  mean: a user who does not want their bookmark to track their saves turns
  commit on save off, which is its default.
- A repository whose bookmark someone else moved between our read and our write
  gets a refused move and an error naming the exact `jj bookmark move` to run.
  The commit is already recorded and is never lost.
