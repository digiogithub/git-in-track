# ADR-008 — Item ID scheme `<KEY>-<TYPE>-<NNNN>`

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 0 (Foundations)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-007](ADR-007-team-repo-references.md), [ADR-012](ADR-012-comments-as-separate-files.md)

## Context

Items need identifiers that appear in filenames, front matter, board references,
commit messages, branch names, pull request titles, chat messages and agent prompts.
The identifier is the single most frequently typed and spoken artifact of the whole
system.

Competing pressures:

- **Human legibility.** "ACME-US-42" survives being said out loud in a standup.
  A UUID does not.
- **Uniqueness without coordination.** IDs are allocated on machines that may be
  offline, by humans and by agents, and reconciled by git. There is no allocator
  service ([ADR-002](ADR-002-git-as-only-sync.md)).
- **Cross-project references.** The team board refers to items in several
  repositories ([ADR-007](ADR-007-team-repo-references.md)); refs must be
  unambiguous.
- **Sortability and greppability.** IDs should sort sensibly and be safe in
  filenames, URLs and branch names on all platforms.

## Decision

**IDs have the form `<KEY>-<TYPE>-<NNNN>`**, for example `ACME-US-0042`.

- `KEY` — the project key from `project.yaml` (`key: ACME`). Uppercase ASCII letters
  and digits, 2–10 characters, starting with a letter. Unique within a team by
  convention, enforced by validation of `team.yaml`.
- `TYPE` — a fixed two-or-three letter code: `EP` (epic), `US` (story), `TSK` (task),
  `MS` (milestone), `SPR` (sprint), `RET` (retro). Boards are referenced by slug, not
  by numeric ID.
- `NNNN` — a zero-padded decimal counter, **per project and per type**, minimum four
  digits, growing to five and beyond when exhausted (`ACME-US-10000` is valid).
- The filename is `<ID>-<slug>.md`, where `slug` is a lowercased, ASCII-folded,
  hyphenated form of the title, truncated to 60 characters.
- **Allocation is by index scan, not by counter file.** The next ID is
  `max(existing NNNN for that project and type) + 1`, computed from the in-memory
  index. There is no `counter.txt` to conflict on.
- IDs are **immutable**. Renaming an item changes its title, its slug and therefore
  its filename, but never its ID. The ID in front matter is authoritative; the
  filename is derived.
- Cross-project references are written `<projectKey>/<itemId>` (`ACME/ACME-US-0042`).
  The redundancy is deliberate: the ref is unambiguous even if the file is quoted out
  of context, and the prefix lets a resolver find the right repository without
  parsing the ID.

## Consequences

**Positive**

- Readable, speakable, greppable, tab-completable. `ACME-US-0042` in a commit message
  or a branch name is self-explanatory.
- Type is visible in the ID, so a reference in a chat message or an agent prompt
  carries meaning without a lookup.
- No allocator, no counter file, no coordination: allocation works offline and the
  common case produces no conflicts.
- Zero padding sorts correctly in file listings for the first 9,999 items of a type.
- Safe in every filesystem, URL and git ref we care about.

**Negative**

- **Concurrent allocation can collide.** Two people (or an agent and a person)
  offline at the same time both allocate `ACME-US-0043`. Git will merge both files
  because they have different slugs and therefore different filenames — so nothing is
  lost, but two items share an ID. Mitigations: validation reports duplicate IDs
  loudly as a diagnostic; the UI offers one-click renumbering of the newer item
  (rewriting inbound references it can find); `gintrack doctor` checks for duplicates.
  We accept collisions as rare and recoverable rather than adopting unreadable IDs.
- **A rename rewrites the filename.** That shows in git as a rename plus a content
  change and may be noisy. The core detects continuity via the front-matter `id` so
  boards and links never break.
- **The `KEY` is a namespace with no registry.** Two projects in a team choosing
  `API` collide; `team.yaml` validation catches it, but only for projects listed
  there.
- **Padding is cosmetic, not semantic.** `ACME-US-0042` and `ACME-US-42` denote the
  same item; the parser must normalise on read and the writer must always emit the
  padded form.
- **Sequence gaps happen.** A deleted item leaves a hole and a discarded allocation
  is never reused, so numbers are not a count of anything. This surprises people and
  should not be presented as a metric.

## Alternatives considered

- **UUIDv4 or ULID.** Collision-free without coordination, and unusable by humans in
  conversation, commit messages or filenames. Rejected as the primary ID; a
  monotonic ULID remains an option for internal artifacts such as comment filenames,
  where humans never type the identifier.
- **A per-project counter file (`.pmngr/counter.yaml`).** Cheap and simple, and it
  produces a merge conflict on *every* concurrent creation — strictly worse than the
  rare duplicate-ID case, because it conflicts even when the two creations are
  unrelated. Rejected.
- **Global sequence across all types (`ACME-42`).** Shorter, but loses type
  information in the ID and makes per-type numbering impossible. Rejected: type
  encoding in the ID pays for itself in every reference.
- **Content-hash IDs.** Stable and coordination-free, but opaque, and they change
  meaning if the content is treated as identity. Rejected.
- **Timestamp-based IDs (`ACME-20260903-1014`).** Nearly collision-free and sortable,
  but long, hard to say out loud, and they leak creation time into every reference.
  Rejected.
