# ADR-013 — Card order as a plain ordered list, not a fractional index

- **Status:** Accepted
- **Date:** 2026-09-04
- **Phase:** 3 (Team repository and boards)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-002](ADR-002-git-as-only-sync.md), [ADR-007](ADR-007-team-repo-references.md)
- **Closes:** [`02-architecture.md`](../02-architecture.md) open question 5, "Board ordering conflicts"

## Context

A board stores the position of its cards in the board file, because git is the
only sync mechanism there is (ADR-002). The obvious data shapes are two:

1. **An ordered list per column**, one card reference per line:

   ```yaml
   order:
     in_progress:
       - ACME/ACME-US-0042
       - ACME/ACME-T-0107
   ```

2. **A fractional index per card**: each card carries a sort key (`"a0"`,
   `"a0V"`, `"a1"`) and a new position is a key strictly between its two
   neighbours. This is what collaborative editors use, because inserting between
   two items touches exactly one record and never renumbers anything.

The fractional index is the better structure in a database. It is a worse one in
a git repository, and doc 02 flagged the question rather than assuming an answer.

Two facts decide it:

- **Where the key would live.** A card's position belongs to the board, not to
  the item — the same story can sit on three boards. So the key cannot go in the
  item's front matter, and it would have to live in a board-side mapping of
  reference → key. That is the ordered list again, with an extra column of
  opaque strings that a human cannot read, cannot edit by hand, and cannot
  review in a pull request. It breaks requirement 2 of ADR-001 for no gain.
- **What actually conflicts.** With one reference per line, two people moving
  two different cards touch two different regions of the file and git merges
  them with no help. Two people moving *the same* card conflict — and with a
  fractional index they would conflict too, on that card's key. The structure
  that costs readability does not buy fewer conflicts in the case that matters.

The real risk (R5) is not the encoding but the *shape of the diff*: a list
written in flow style (`[a, b, c]`) makes every reorder a whole-line rewrite and
therefore a conflict. That is a formatting decision, not a data-structure one.

## Decision

Card order stays a plain ordered list of references per column, written **one
reference per line**, never in flow style, and the columns keep the order the
file already had.

The emitter (`internal/core/SerializeBoard`) guarantees:

- one `- <KEY>/<ITEM-ID>` per line under each column id;
- a deterministic key order for everything else in the front matter, so that a
  write never produces a diff caused by Go map iteration;
- idempotency: parsing a board and serialising it again yields the same bytes.

Order is **advisory and partial** (R-ORD-2): a card the list does not mention is
appended after the listed ones, sorted by priority then `updated` descending, and
a reference that no longer belongs to the column is ignored on read and pruned on
the next write. A board therefore renders correctly with no `order:` block at
all, which is what makes a hand-written board file a reasonable thing to commit.

A move rewrites exactly one region: the reference leaves the lines of its old
column and is inserted into its new one. Nothing else in the file changes.

## Consequences

**Positive**

- A board file is readable and hand-editable, and its diff in a pull request
  says what moved.
- Two people moving different cards merge with no custom merge driver
  (`internal/core` has a test that drives `git merge-file` over two divergent
  moves and requires a clean merge — a milestone-4 exit criterion).
- No new concept: the list is the same data the UI renders.

**Negative**

- Two people re-ordering the **same** column concurrently produce a genuine text
  conflict. This is accepted: card order is the least valuable state in the
  system, and the conflict UI offers "take mine / take theirs / union (mine
  first)" (doc 04 R-ORD-3).
- A very large column produces a long block. A board with hundreds of cards in
  one column has a bigger problem than its file size.

**Revisit if** boards routinely carry columns of several hundred cards *and*
same-column concurrent reordering becomes a real complaint rather than a
theoretical one. The migration path is additive: a per-card key mapping can be
introduced beside `order:` and the list kept as the readable projection.
