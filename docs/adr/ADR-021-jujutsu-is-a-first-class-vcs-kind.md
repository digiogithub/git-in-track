# ADR-021 — Jujutsu is a first-class VCS kind, and git never writes behind it

- **Status:** Accepted
- **Date:** 2026-09-06
- **Phase:** Post-1.0 (Jujutsu support, `GIT-EP-0010`)
- **Related:** [ADR-006](ADR-006-isomorphic-git-vs-go-git.md), [ADR-017](ADR-017-metrics-history-from-git-not-a-stored-time-series.md)
- **Implements:** `GIT-US-0038` — Detect a Jujutsu repository and stop writing behind it

## Context

Jujutsu stores its commits in a git repository. That made the product *look*
compatible: `config.Detect` reported `Git: true` for a colocated jj workspace,
`gitops.Open` opened it, and history and metrics were correct, because jj
exports every commit as a real git commit with its author date.

Everything on the write side was wrong, and one part of it was destructive.

In a jj workspace the working copy is itself a commit, `@`, and git's `HEAD`
sits at its parent, `@-`. Verified in a throwaway repository: `git add -- p &&
git commit --only -- p` lands on `@-`, moves no bookmark, and on the next `jj`
command jj prints "Reset the working copy parent to the new Git HEAD",
re-parents `@` onto the new commit and **abandons the previous working-copy
commit as an orphan**. The commit is unreachable from any bookmark, so `jj git
push` would never publish it. Commit on save being off by default is the only
reason this never bit anyone.

The same mismatch produced three lies. `git rev-parse --abbrev-ref HEAD` answers
the literal `HEAD`, so the status reported a detached HEAD and the sync panel
painted a **destructive** badge over jj's healthy steady state. jj keeps git's
index synchronized with `@` while `HEAD` is at `@-`, so the working copy could
read as fully staged and therefore permanently dirty. And sync's preflight
refused every jj repository with "detached HEAD: check out the branch you want to
sync first" — advice for a repository that has no checked-out branch by design.

Meanwhile a jj workspace whose git store lives inside `.jj/` was refused at
registration with `not a git repository`, although its backlog is ordinary
Markdown the product can read perfectly well.

## Decision

**Jujutsu is a version-control kind of its own, detected before anything else
happens, and no git write may reach a repository that is not detected as git.**

- **The vocabulary lives in `internal/core`** — `VCS` (`git`, `jj`, `none`),
  `VCSLayout` (`colocated`, `internal`), the labels, the one summary sentence
  `managed by Jujutsu — reads work, writes go through jj`, and the wording of a
  refusal. It is pure and compiles to WASM, so the CLI, the API and the web app
  cannot drift apart on what they call a repository.
- **The plumbing lives in `internal/gitops`.** `DetectVCS` recognizes the `.jj`
  marker and resolves the store from `.jj/repo/store/git_target`, which
  distinguishes a colocated workspace (the store is the workspace's own `.git`)
  from one with an internal store (including the legacy `.jj/repo/store/git`).
  The `.jj` marker wins over `.git`.
- **`gitops.Open` wraps a jj working tree in a guard.** Reads pass through;
  `Commit`, `Fetch`, `Integrate`, `Push`, `Abort`, `Continue` and `ResolvePath`
  are answered by the guard itself with `vcs_jujutsu_write_refused` and a message
  naming the `jj` command that does the same thing safely. The refusal happens
  before a git process is started, so it is a property of the layering rather
  than a check someone can forget to call. The sync preflight refuses the whole
  run, dry runs included.
- **The guard corrects the status rather than hiding it.** The branch is `@`,
  `detached` is false, and the index column is dropped from the dirty set. The
  sync state is `jujutsu`, which takes precedence over every other state, and the
  panel renders it as an ordinary, non-destructive fact with Preview and Sync
  disabled.
- **A jj repository registers like any other, in both layouts.** One with an
  internal store is registered, indexed and served read-only; `gitops.Open`
  refuses it with `vcs_jujutsu_unsupported`, whose message names jj instead of
  claiming the folder is not a repository.
- **The `jj` binary is probed the way `git`'s is**, with `jj --version` and
  nothing else — every other jj command snapshots the working copy, which is a
  write. The supported minimum is jj 0.41, reported by `gintrack doctor` as a
  warning, never as a refusal, because the product runs no jj command yet.
- **The `Backend` interface is unchanged.** The kind is exposed through the
  optional `VCSOf(Backend)` accessor and through `Capabilities`. Generalizing the
  interface for jj's model — bookmarks instead of branches, conflicts recorded
  inside commits, `jj undo` in place of `--abort` — is `GIT-US-0039`, done in
  [ADR-022](ADR-022-a-vcs-neutral-backend-interface.md), and the jj backend
  itself is `GIT-US-0040`/`GIT-US-0041`. The guard's refusals moved with it:
  `Abort` and `Continue` are now `Undo` and `Resume`.

## Consequences

**Positive**

- The destructive path is closed by construction: a git write cannot reach a
  repository the product did not detect as git, and a test drives commit on save
  and a full sync against a real jj repository and proves its refs, `HEAD` and
  object graph are byte-identical afterwards.
- A jj user is told the truth in every surface: `gintrack ls`, `gintrack doctor`,
  `gintrack add`, the repository and sync payloads and the sync panel all say the
  same sentence, and none of them shows a red badge for a healthy repository.
- A jj workspace with no git working tree stops being a registration error and
  becomes a repository the product reads.
- Reads that already worked — history, metrics, log — are untouched.

**Negative**

- **A jj repository is read-only to this product until the backend lands.** A user
  who relied on commit on save gets a refusal where they used to get a commit —
  a commit that was silently unreachable, but a commit. The message has to carry
  that explanation, and it does.
- The guard runs one extra `git status` per sync status read, to rebuild the
  dirty set from the working-copy column alone.
- Dropping the index column means a genuinely index-only difference is invisible
  in a jj repository. In jj that difference *is* the working-copy commit, so this
  is the correct reading rather than a compromise — but it is a git-shaped tool
  telling a non-git truth, and it will surprise someone.
- Two more codes, `vcs_jujutsu_write_refused` and `vcs_jujutsu_unsupported`, that
  every client switches on.

## Alternatives considered

- **Leave detection alone and only fix the sync preflight.** It removes the
  wrong advice and leaves commit on save free to orphan commits. Rejected: the
  dangerous path is the reason the story exists.
- **Keep treating a colocated jj repository as git and warn in the UI.** A warning
  is exactly what nobody reads before enabling commit on save. Rejected in favor
  of a refusal that cannot be bypassed.
- **Drive jj directly for reads — `jj log`, `jj status` — right away.** Every jj
  command snapshots the working copy and writes to the operation log, so a
  read-only story would start writing to the user's repository to find out
  whether it may write. Rejected; only `jj --version` is run.
- **Implement the jj backend now.** It requires generalizing a `Backend` built
  around git's index stages, `MERGE_HEAD` and `--continue`, none of which exist
  in jj. That is a separate design, and the destructive path had to close first.
- **Refuse to register a jj repository at all.** Honest and useless: the backlog
  is Markdown, and reading it needs no git.
