# ADR-002 — Git is the only sync mechanism; no central server

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 0 (principle), 4 (implementation)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-006](ADR-006-isomorphic-git-vs-go-git.md), [ADR-007](ADR-007-team-repo-references.md)

## Context

A multi-user project management tool must answer: where does shared state live, how
do concurrent edits reconcile, and what happens offline?

The default industry answer is a central server with a database, an auth system, a
real-time channel, and an operations burden. That answer conflicts with the
project's core premise. Our users already run a distributed, authenticated,
conflict-aware, offline-capable synchronisation system for the exact same
repositories: git. Teams that must self-host already host git. Teams that use
GitHub, GitLab, Gitea or a bare repo over SSH already have access control, audit,
backup and availability for this content.

Adding a server would mean two sources of truth (files and database rows), a
reconciliation problem between them, an identity model of our own, hosting costs,
and a component that can be down while the user's laptop is fine.

## Decision

**Git is the only synchronisation mechanism. git-in-track ships no server that
stores state.**

- Every write goes to the working tree of a local clone.
- Sharing is `git push`; receiving is `git fetch` plus rebase or merge.
- Concurrency control is optimistic: reads return a `rev` (content hash of the
  file); writes may supply the `rev` they are based on and are rejected with
  `CONFLICT` if it no longer matches. Cross-machine conflicts are git conflicts,
  presented in an item-aware conflict UI.
- `gintrack serve` is a **local** process serving the UI and the local API. It binds
  loopback, holds no shared state, and is per-user.
- Access control is whatever the git remote enforces. There are no permissions in
  git-in-track. Branch protection is the mechanism for gated changes.
- Commit-on-save is optional and configurable (message template per action);
  explicit sync is always available.

## Consequences

**Positive**

- Nothing to deploy, nothing to back up beyond the repositories, no accounts, no
  seats, no vendor. Self-hosting is free because it is already done.
- Full offline operation. A flight, a train, a firewalled network: everything works
  except sharing.
- History, attribution, audit, revert and branching come from git and are exactly as
  good as they are for code. Planning a large refactor on a branch is possible.
- The security surface is tiny: no public endpoint, no session store, no
  multi-tenancy bugs.

**Negative**

- **No push notifications.** A change is invisible to teammates until someone
  fetches. The UI must make "last synced" state legible and make syncing cheap, and
  must never pretend to be real-time across machines.
- **Conflicts are the user's problem.** We can reduce them (one item per file,
  comments as separate files, field-level merges) but cannot eliminate them. The
  conflict UI is a first-class feature, not an afterthought.
- **Board card ordering is merge-prone.** `order:` lists in a single board file are
  the sharpest edge in the data model; mitigation is tracked as an open question in
  the architecture document.
- **No server-side automation.** Notifications, scheduled reminders and SLA timers
  have no place to run. Where teams want them, the answer is CI in their own repo.
- **No enforcement.** Anyone who can push can write any field. Governance must come
  from review, not from the app.
- **Large histories.** Thousands of items with churn make repositories grow; we
  recommend keeping the backlog in the project repo it belongs to rather than a
  monolithic planning repo.

## Alternatives considered

- **Central server with a database, files exported on demand.** Solves real-time and
  permissions, and destroys the premise: the database becomes authoritative, the
  files become a lossy export, and self-hosting becomes an operations project.
  Rejected outright.
- **Optional sync server for real-time only (relay, no storage).** Attractive, and
  still rejected for v1: it introduces a network dependency, a discovery and auth
  problem, and a second code path for change propagation. Local real-time via
  fsnotify covers the single-machine case, which is the common one.
- **CRDTs over a peer-to-peer transport.** Removes conflicts, at the cost of an
  opaque binary or heavily annotated on-disk format — which breaks
  [ADR-001](ADR-001-markdown-yaml-storage.md) — plus a transport and identity
  problem. Rejected.
- **Git plus a custom merge driver installed per clone.** Would improve automatic
  merging of front matter, but requires every collaborator to install and configure
  it; a clone without it silently behaves differently. May return later as an
  optional enhancement, never as a requirement.
- **Using the host's issue API (GitHub Issues) as the store.** Ties us to one host,
  puts state outside the repo, and breaks offline. Rejected.
