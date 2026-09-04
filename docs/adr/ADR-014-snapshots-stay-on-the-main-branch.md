# ADR-014 — Index snapshots stay on the main branch and are written only when their content changes

- **Status:** Accepted
- **Date:** 2026-09-04
- **Phase:** 3 (Team repository and boards)
- **Related:** [ADR-002](ADR-002-git-as-only-sync.md), [ADR-007](ADR-007-team-repo-references.md), [ADR-013](ADR-013-board-card-ordering.md)
- **Closes:** [`04-team-repository.md`](../04-team-repository.md) open question 1, "Snapshot churn"

## Context

A team board must render a card for `WEB/WEB-US-0031` on the laptop of someone
who has never cloned the website repository. Those cards come from a committed
snapshot, `.pmngr/index/<projectKey>.json`, written by whoever does have the
project cloned (doc 04 §6).

The snapshot is derived data in a repository whose only sync mechanism is git
(ADR-002), so every refresh is a commit somebody else has to pull. Doc 04 flagged
the churn this creates and listed two alternatives to evaluate in Phase 3:

1. **A dedicated `pmngr-index` branch.** Snapshots live off `main`, which stays
   free of generated files.
2. **A schedule.** Refresh hourly rather than on every sync, trading freshness
   for fewer commits.

A third option was implicit and turned out to be the important one: **write the
file only when its content actually changed**.

The generated document carries four fields that move on every run whatever the
backlog did — `generated`, `generated_by`, `generator` and `source`. They are
what make a naive writer produce a commit per run. Everything else — the project
block, the workflow, the labels, the counts and the items sorted by id — is a
pure function of the files in the project repository.

## Decision

Index snapshots stay in the team repository **on the main branch**, in the
dedicated commits R-SNAP-7 already describes.

Every writer — `gintrack snapshot`, `snapshot.refresh` in the core contract, and
`POST /api/v1/snapshots` — regenerates the document, compares it with the file on
disk **ignoring `generated`, `generated_by`, `generator` and `source`**
(`core.SameSnapshotContent`), and writes nothing when only those differ. The
answer reports `unchanged` rather than `written`, so a caller can tell the two
apart without diffing.

Refreshing on a schedule stays available to whoever wants it: it is a cron entry
around the same command, not a product feature.

## Consequences

**Positive**

- A refresh that finds nothing new produces no write, no commit and nothing to
  pull. The churn the open question worried about is gone at its source rather
  than moved to another branch.
- One clone, one truth: a reader of the team repository sees the snapshots next
  to the boards that use them, in the same working tree, at the same revision.
  A second branch would have made "which snapshot does this board render with?"
  a question with two answers.
- CI in a project repository can run `gintrack snapshot` on every push without
  flooding the team repository's history.
- The comparison is content-addressed, so two people regenerating the same
  backlog from two clones produce the same bytes and never fight over the file.

**Negative**

- A snapshot that is written *does* land on `main`, so a busy team still sees
  generated-file commits in its history. They are filterable by their message
  prefix (R-SNAP-7) and diff-suppressible with the `.gitattributes` entry of
  R-SNAP-8, and they are far rarer than one per refresh.
- `generated` is not refreshed when nothing else changed, so a snapshot's
  recorded age is the age of its *content*, not of its last verification. This
  is the honest reading — the UI's "snapshot from 3 days ago" then means "these
  cards have looked like this for three days" — but it means a stale badge can
  appear on a file somebody regenerates hourly. R-SNAP-9's threshold is a team
  setting for exactly that reason.

**Revisit if** a team demonstrates that snapshot commits on `main` still disrupt
review or bisect after the content comparison, or if the recorded-age semantics
confuse people in practice. The migration path is additive: a separate branch or
a `verified` field can be introduced without changing the schema readers depend
on.
