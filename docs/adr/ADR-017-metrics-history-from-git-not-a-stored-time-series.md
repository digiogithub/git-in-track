# ADR-017 — Sprint metrics reconstruct their history from git, and say so when they cannot

- **Status:** Accepted
- **Date:** 2026-09-04
- **Phase:** 6 (Agile loop and 1.0)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-002](ADR-002-git-as-only-sync.md), [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-006](ADR-006-isomorphic-git-vs-go-git.md)
- **Implements:** `GIT-US-0028` — Show burndown and cumulative flow metrics

## Context

A burndown is a time series. A cumulative flow diagram is a time series. Cycle
time and lead time are durations between two moments. All four need to know what
an item's status was on a day that has already passed.

**The product stores no such thing, and must not start.** An item file carries
`updated` — one instant, overwritten by every write — and `rev`, which is a
content hash computed at read time and never stored (doc 03 §4). There is no
history field, no transition log, no `.pmngr/events/`. Adding one would break
three commitments at once: Markdown and YAML are the storage format (ADR-001),
git is the only sync mechanism (ADR-002), and a transition log is precisely the
kind of append-only file that two people editing the same board would conflict
on every day.

So the history has to come from somewhere that already exists. Three candidates:

1. **The git history of the item files.** Every status change is a commit; every
   commit has an author date; every revision of the file can be read back. This
   is the truth, and it is already in the repository.
2. **A derived event log**, written by the app when it observes a change and
   rebuilt from git if lost. It answers faster, and it is a second thing to keep
   correct, to merge and to explain.
3. **The `updated` stamp**, which places the *last* transition of an item and
   nothing before it.

Option 1 is only available where the process can read a git repository — that is,
the companion (`gintrack serve`, the CLI). The browser-only build has no git at
all in `internal/core`, which compiles to WebAssembly and must stay free of
`os/exec` and `syscall`; isomorphic-git exists in the front end for sync
(ADR-006) but pushing per-revision blob reads through it, in the worker, is a
different feature with a different cost.

That leaves a real question for browser-only mode: show nothing, or show
something that is not the whole truth?

## Decision

**The git history of the item files is the source, and it is read live.**

- `internal/gitops` reads it: `Backend.History` returns every revision of a set
  of paths — path, commit, author date, and the file's bytes as they stood —
  from both backends. It is native-only by construction.
- `internal/core` computes everything: `HistoriesFromRevisions` turns revisions
  into per-item observations, and `BuildSprintMetrics` turns observations into
  the burndown, the cumulative flow diagram and the flow statistics. Nothing in
  `internal/core` knows what git is, so the same arithmetic runs in both hosts.
- `internal/vault` joins the two through a one-method `HistorySource` interface.
  The companion installs a git-backed implementation; the browser installs none.

**No time series is ever stored.** A `gitops.HistoryCache` memoizes one walk per
repository against the commit it was read at: a new commit moves HEAD, a moved
HEAD misses the cache, and the walk runs again. It is a process-lifetime memo of
derived data — thrown away costs one walk — and never a source of truth.

**Where the history cannot be read, the product degrades in the open rather than
faking a curve.** Every result carries a `MetricsProvenance`:

| `source` | Meaning | `approximate` |
|---|---|---|
| `git` | Every revision of every covered item file, reconstructed from commits. | `false` (`true` if the walk was truncated) |
| `updated` | Each item is assumed to have held its current status since its `updated` stamp; nothing is claimed about the time before that. | `true` |
| `none` | Nothing is known but the current state. | `true` |

A day the history cannot account for is counted as **unknown** — it appears as
`unknown` on the burndown row and as a hatched band in the cumulative flow
diagram — and never as an empty backlog or as finished work. The web UI prints
`provenance.note` above the charts, before a reader can act on them.

## Consequences

**Positive**

- The numbers cannot drift from reality, because there is nothing separate that
  could drift. Deleting every cache and re-cloning the repository reproduces
  them exactly.
- Nothing new to merge. Two people running a sprint produce the same commits
  they already produced; the metric is a read.
- Filtering by path is git's fastest question, so cost tracks the number of
  commits that touched the sprint's item files, not the size of the repository.
  Measured on a 2,002-commit fixture: 32 ms with system git, 78 ms with go-git.
- Browser-only mode still shows a chart, and a reader is told exactly what it
  is and how to get the real one.

**Negative**

- Browser-only mode's burndown is genuinely weaker: it can place each item's
  last transition and nothing earlier, so the early days of a sprint are mostly
  unknown. That is visible, which is the point, but it is not parity.
- A repository whose backlog is not committed (a fresh working copy, a folder
  that is not a git repository) has no history at all and falls back to the
  `updated` approximation.
- A history rewritten by a rebase or a squash rewrites the metric with it. That
  is the honest consequence of git being the record.
- The walk is bounded by `gitops.DefaultHistoryLimit` (2,000 commits per path).
  Beyond it the result is flagged `truncated` and the days before the oldest
  revision read are unknown.

**Neutral**

- Reading history in the browser through isomorphic-git remains possible later.
  It would change `provenance.source` from `updated` to `git` for that host and
  nothing else: the contract, the computation and the UI already handle both.
- Board-level (non-sprint) cumulative flow is not built. It needs a window
  definition rather than new machinery, and can be added against the same
  `HistorySource`.

## Alternatives considered

- **A committed transition log.** Rejected: a second source of truth, and an
  append-only file that conflicts on exactly the days a team is busiest.
- **A `history:` block in each item's front matter.** Rejected: it makes every
  status change a body-and-front-matter rewrite, bloats the file people read,
  and duplicates what git already stores perfectly.
- **Refusing to answer at all in browser-only mode.** Rejected: an item's
  `updated` stamp plus its current status is real information, and a chart that
  states its own limits is more useful than an empty panel — provided it is
  never mistaken for the reconstruction, which the provenance banner ensures.
- **Adding a charting library.** Rejected: both charts are polylines over a
  linear scale, and hand-rolled SVG keeps the marks on the app's own design
  tokens instead of a second palette.
