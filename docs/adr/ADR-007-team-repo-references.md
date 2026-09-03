# ADR-007 — The team repo holds references and index snapshots, never copies

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 3 (Team repo)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-002](ADR-002-git-as-only-sync.md), [ADR-008](ADR-008-id-scheme.md)

## Context

A team works across several project repositories, and needs one board that shows
work from all of them. The backlog of each project belongs in that project's
repository — that is what makes the plan travel with the code, get reviewed in the
same pull request, and be checked out at a release tag.

So the team repository (one per team, holding `team.yaml`, `knowledge/`,
`.pmngr/boards/`, `.pmngr/sprints/`, `.pmngr/retros/`) needs a way to talk about
items it does not own. Three approaches exist: copy the items in, use git
submodules, or store references.

A further constraint: a team member may not have cloned every project repository.
A PM with three squads should still see a full board without cloning eight
repositories they will never build.

## Decision

**The team repository stores only references to project items, plus a committed,
explicitly stale index snapshot per project.**

- `team.yaml` lists each project repository: remote URL, default branch, docs folder
  path, and project key.
- Board columns and sprints reference items as `ref: <projectKey>/<itemId>`, for
  example `ref: ACME/ACME-US-0042`. The reference is resolved at read time.
- Card ordering lives in the board file as an `order:` list of refs per column. The
  board owns *arrangement*; the project repo owns *content* (title, status, body).
- Resolution has two tiers:
  1. **Local clone available** — the item is read live from the project repo's
     working tree. It is fully interactive: open, edit, move, comment.
  2. **No local clone** — the card is rendered as a **remote reference** from the
     snapshot committed at `.pmngr/index/<projectKey>.json` (id, type, title, status,
     assignees, labels, updated, remote URL). It is visually marked as remote,
     read-only, and shows when the snapshot was generated. It links out to the file
     on the remote host.
- Snapshots are produced by `gintrack snapshot` or a CI job in the project repo, and
  committed to the team repo. They are a **cache**, never a source of truth: when a
  local clone exists, the snapshot is ignored entirely.
- A board move writes two files in two repositories (the item's `status` in the
  project repo, the `order:` list in the team repo), each committed in its own repo.

## Consequences

**Positive**

- One source of truth per item. There is no possibility of a copy in the team repo
  disagreeing with the project repo.
- The project's backlog stays with its code: reviewable in the project's PRs,
  checkout-able at a tag, meaningful to anyone reading that repository alone.
- Partial adoption works: a team can put one project under git-in-track and add
  others later without restructuring.
- Non-developers get a useful board without cloning code repositories.
- Team-level artifacts that genuinely belong to the team — boards, sprints, retros,
  shared knowledge — live in one place with their own history.

**Negative**

- **Snapshots go stale.** A remote card can show a status that is hours or days old.
  The UI must always show snapshot age and never let a stale card be edited. Getting
  this wrong would be actively misleading, so it is a hard UI requirement.
- **Snapshot generation needs an owner.** Somebody or some CI job must run
  `gintrack snapshot`. If nobody does, remote cards rot. This is called out as an
  open question in the architecture document.
- **Cross-repo operations are not atomic.** A card move can succeed in the project
  repo and fail in the team repo, or vice versa. The UI must report partial success
  precisely and offer a retry, rather than claiming the move happened.
- **Dangling references.** An item deleted or renamed in a project leaves a ref
  pointing at nothing. Validation surfaces these as diagnostics with a one-click
  removal from the board.
- **`order:` lists conflict.** Two people reordering the same column produce a merge
  conflict in a list of refs. It resolves easily but it is the most common conflict in
  the system; a fractional-index scheme is under investigation.
- **More setup.** Users must configure `team.yaml` and, ideally, clone the projects
  they work on. First-run guidance has to carry this.

## Alternatives considered

- **Copy items into the team repo (denormalise).** Boards work offline with no
  snapshot machinery, and every item now exists twice with a synchronisation problem
  between them — exactly the "database and files disagree" failure the whole project
  is designed to avoid. Rejected.
- **Git submodules for each project inside the team repo.** Genuinely single-source
  and offline-capable, but submodules are notoriously hostile in daily use (detached
  heads, pinned commits, extra commands for every update), and they force every team
  member to clone every project. Rejected.
- **Git subtree merges.** Same duplication problem as copying, with a harder merge
  story. Rejected.
- **No team repository at all; boards defined per project.** Simpler, and it makes a
  cross-project board impossible, which is a primary requirement for Phase 3.
  Rejected.
- **Resolve remote items live via the host API (GitHub/GitLab REST).** Always fresh,
  but requires per-host API support, network, and tokens for every project — and it
  reintroduces a host dependency the project is built to avoid. Rejected; the remote
  URL link gives the user a manual path to fresh data.
