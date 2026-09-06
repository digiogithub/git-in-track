# ADR-022 — The backend interface is VCS-neutral: a line of work, an integration, three sides

- **Status:** Accepted
- **Date:** 2026-09-06
- **Phase:** Post-1.0 (Jujutsu support, `GIT-EP-0010`)
- **Related:** [ADR-006](ADR-006-isomorphic-git-vs-go-git.md), [ADR-021](ADR-021-jujutsu-is-a-first-class-vcs-kind.md)
- **Implements:** `GIT-US-0039` — Generalize the backend interface beyond git

## Context

[ADR-021](ADR-021-jujutsu-is-a-first-class-vcs-kind.md) made a Jujutsu
repository safe by refusing every git write behind it, and left the `Backend`
interface untouched on purpose. The epic asks for full compatibility, writes
included, and the jj backend that will deliver it (`GIT-US-0040`/`GIT-US-0041`)
cannot be written against that interface without lying, because four of its
concepts are git's rather than version control's.

**A staging area.** `Commit` was specified as "stage exactly these paths and
commit them", and the capability that gates it was called `PathspecCommit`. jj
has no index: `jj commit -m … -- <paths>` and `jj squash -- <paths>` act on the
working-copy commit directly. Staging was never what the product needed; it
needed *this commit covers exactly these paths and nothing else*.

**Operation state.** `SyncStatus.Operation` was a `MERGE_HEAD`/`rebase-merge`
marker; `ResolvePath` refused while it was empty; `Abort` and `Continue` were
named after `--abort` and `--continue`. In jj nothing is ever half-finished: a
`jj rebase` always completes, conflicts are recorded *inside* the resulting
commits, and `jj undo`/`jj op restore` takes a whole operation back out of the
operation log. A jj backend asked for `Operation` could only invent one.

**Index conflict stages.** `ConflictFile` was specified as "read stages 1, 2 and
3". A colocated jj repository does export those stages, a non-colocated one has
no git index at all, and jj's materialized conflict markers (`%%%%%%%`,
`+++++++`) are not git's — so a caller that parses the working file cannot
assume the dialect either.

**Branches.** `Status.Branch` plus a `Detached` flag, and a push of
`HEAD:refs/heads/<branch>`, assume a checked-out branch that moves on commit. jj
has bookmarks, which do not move on commit, and its `HEAD` equivalent is `@-`.

## Decision

**`Backend` is expressed only in concepts that exist in every VCS we drive.
Where git and jj genuinely differ, the difference is data a backend reports, not
an assumption a caller makes.**

- **A commit covers exactly a set of paths.** `Commit` is specified that way;
  how the backend limits it is its own business — git stages a pathspec, a VCS
  with no index names the same files on its commit command. The capability is
  `ScopedCommit`. `Status.Staged` survives as "what a VCS with a staging area
  has already recorded", documented as empty where there is none.
- **An unsettled integration is a value, not a marker.**
  `Integration{Operation, Unfinished, Undo, Resume}` says whether the repository
  is between two settled states, how it is taken back (`abort` or
  `operation_log`) and how it is carried forward (`continue`, or nothing at
  all). `Unfinished` — not "a marker file exists" — is the precondition of
  `ResolvePath` and of the sync preflight, and it is a question jj can answer.
- **`Abort`/`Continue` become `Undo`/`Resume`**, named after what they mean
  instead of after git's flags, so `jj undo` is a legitimate implementation of
  undo rather than an approximation of an abort. A backend whose `Resume` is
  `ResumeNone` refuses `Resume` explicitly instead of pretending.
- **A conflict is three sides, produced however the backend can.** Both git
  backends still read index stages 1/2/3; a jj backend will materialize them
  from the conflict inside the commit. `ConflictVersions.Markers` names the
  dialect the working file is written in, so no reader assumes git's. `Ours` is
  always the user's own side, whatever the VCS calls it.
- **`Line` replaces the branch.** `Line{Name, Anonymous, Kind, PushTarget}` is
  the current line of work and where publishing it goes: `Kind: branch` with the
  branch as `PushTarget` in git, `Kind: working-copy` for jj's `@` with a
  bookmark as `PushTarget`, `Anonymous` for a detached HEAD. `PushRequest.Target`
  replaces `PushRequest.Branch`; building `HEAD:refs/heads/<branch>` stays inside
  the git backends, where it belongs.
- **The published JSON does not move.** `Line` and `Integration` are embedded, so
  `branch`, `detached` and `operation` are exactly where GIT-US-0021 put them,
  `pathspecCommit` keeps its key, and `lineKind`, `pushTarget`, `unfinished`,
  `undo`, `resume` and `markers` are additive. No `gitops` error code changed. A
  test marshals the types and asserts the old keys.

## Consequences

**Positive**

- The jj backend of `GIT-US-0040` can be written without a single fake: no
  invented `MERGE_HEAD`, no pretend index, no branch that is really a bookmark.
- The neutral fields are strictly more informative for git too. The sync
  preflight now names the undo command of the VCS that reported the integration,
  so a jj repository can never be told to run `git rebase --abort`.
- A refactor with no user-visible surface: both git backends keep every
  capability, the HTTP contract is byte-identical, and the existing suite passes
  unchanged in meaning.

**Negative**

- Two embedded structs mean promoted field access (`status.Operation`,
  `status.Name`), which reads well but hides where a field comes from; the
  interface documentation has to carry that weight.
- `Status.Staged` is still a staging-area word in a neutral interface. Removing
  it would cost the git surface real information for no gain today, so it stays,
  documented as optional.
- Every implementation of `Backend` outside this repository — there are none
  today, but the interface is exported — has to rename two methods.

## Alternatives considered

- **Leave `Backend` alone and let the jj backend fake git's shapes.** It would
  invent an operation name to satisfy `ResolvePath`, report a bookmark as a
  branch, and answer `Continue` with a lie. Rejected: the epic's audit exists
  because git-shaped assumptions misread jj, and this would move them one layer
  down instead of removing them.
- **A second interface, `VCSBackend`, alongside the git one.** Two interfaces
  means two sync pipelines, two servers and two web contracts, all of which must
  agree. Rejected: the pipeline is the same pipeline.
- **Change the JSON to the neutral names too** (`line`, `integration` objects).
  Cleaner on the wire, and a breaking change to a published API for a refactor
  that is supposed to change nothing a user sees. Rejected; the Go names carry
  the truth and the wire keeps its compatibility, with the mapping documented in
  `docs/07-cli-and-api.md` §6.4.
- **Keep `Abort`/`Continue` and merely redocument them.** The names are the
  assumption: `--abort` implies something half-done to abandon, which is exactly
  what jj never has. Rejected.
