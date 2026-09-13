# ADR-033 — The inbox is a reserved `triage` status category plus a front-matter block

- **Status**: Accepted
- **Phase**: 8
- **Related**: [ADR-001](ADR-001-markdown-yaml-storage.md),
  [ADR-008](ADR-008-id-scheme.md),
  [ADR-031](ADR-031-external-references.md),
  [ADR-017](ADR-017-metrics-history-from-git-not-a-stored-time-series.md)

## Context

Work arrives from places that are not the backlog: a web form, an agent through
MCP, an importer pulling from an external tracker (ADR-031), a colleague who has
an idea. None of it has been reviewed. Putting it straight into the backlog
poisons every number a team plans with — scope, velocity, burndown, the board's
WIP — and putting it somewhere else costs it the things that make it useful: a
permanent id (ADR-008), a file, a git history, comments.

So the inbox has to satisfy two things at once: an item is **real from the moment
of submission**, and it is **invisible to planning until somebody accepts it**.
Plane solves the same problem the same way — `IntakeIssue`
(`apps/api/plane/db/models/intake.py` L50-84) keeps the Issue real and wraps it
with a triage status — and we adopt that shape minus the join table.

The triager also needs somewhere to put the decision: accepted, rejected,
duplicate of something, or "not now, ask me again in October". The last one is a
deadline, and a deadline in a tool with no server is a design trap: there is
nothing running to wake up and move the item.

## Decision

**An inbox item is an ordinary item file whose `status` belongs to a fifth
reserved category, `triage`, and which carries an `inbox:` front-matter block.**

- `StatusCategory` gains `triage` beside `todo`, `in_progress`, `done` and
  `cancelled`. A project declares a status in it (the scaffolder seeds
  `{id: triage, name: Triage, category: triage}`) or it does not, in which case
  the project simply has no inbox and behaves exactly as it did before.
- The block holds `status` (`pending | accepted | rejected | snoozed |
  duplicate`), `snoozed_until`, `duplicate_of`, `source` and `received`. Unknown
  keys inside it are preserved on rewrite, like unknown top-level keys.
- **The category is the truth; the block is metadata.** No `is_inbox` boolean is
  stored anywhere, so the two can never disagree.
- Queries exclude triage by default. `Filter.Inbox` is tri-state — `exclude`
  (zero value), `only`, `include` — so every filter written before the inbox
  existed keeps its meaning. Board views, sprint views, sprint candidates and
  sprint metrics exclude them unconditionally, and a sprint file that names a
  triage reference reports it as *unresolved*, not as work.
- **Snooze expiry is a query-time comparison, not a scheduler.** A `snoozed` item
  whose `snoozed_until` is at or before the caller-supplied `SnoozeAsOf` matches a
  query for `pending`. `internal/core` reads no clock — it compiles to WebAssembly
  and its answers have to be reproducible — so the instant is an input, exactly as
  Plane evaluates the same rule in the request
  (`apps/api/plane/api/views/intake.py` L77).

Accepting an item is one status change: `triage` → the workflow's initial status.
The block stays behind as the record of how the item arrived.

## Consequences

### Easier

- An inbox item has an id, a file, a history and comments from the first second,
  so a conversation with the submitter can happen before anybody decides anything.
- Nothing new to learn: a triager moves a card between statuses, which is what
  everything else in the tool already does. Accepting is not an import step.
- Planning numbers are unaffected by submission volume. A thousand spam entries
  change no burndown.
- Snoozing costs nothing to operate. There is no daemon, no cron, no queue, and a
  repository left untouched for a year still produces the right answer the moment
  somebody opens it.
- The rule is enforceable in the model. A hand-edited sprint file naming a triage
  item cannot smuggle it into a sprint.

### Harder — the price we are paying

- **A fifth category every consumer must now handle.** Every `switch` on
  `StatusCategory` across the core, the API, the MCP server and the web app now
  has a case it did not have, and the compiler will not point at the ones that
  used a `default:` branch. Anything that silently maps an unknown category to
  "todo" — a third-party tool, an older binary, a board column mapping
  `categories:` — will show inbox items as backlog. The risk is highest exactly
  where we cannot see it: outside this repository.
- **A project that declares no triage status silently has no inbox.** There is no
  error and no warning; submissions simply have nowhere to go, and a user who
  never edits `project.yaml` will conclude the feature is broken rather than
  unconfigured. Older projects created before this ADR are all in that state.
- **Snooze expiry has no notification.** Nothing tells anybody that an item woke
  up. It reappears in the pending queue the next time somebody looks, which means
  "remind me in October" is really "stop showing me this until October" — weaker
  than what the wording promises, and a user will eventually be surprised by it.
- **`SnoozeAsOf` is a parameter every caller must remember to pass.** A surface
  that forgets it expires nothing, and the failure is invisible: snoozed items
  just stay snoozed forever. Purity bought reproducibility at the cost of a
  footgun.
- **The block outlives the category.** An accepted item keeps its `inbox:` block,
  which is deliberate (it is provenance) but means `W-INBOX-CATEGORY` fires on
  every item that was ever triaged. The warning is noise on exactly the items
  where the block is most correct.
- **Triage is not a workflow state like the others.** It is excluded from metrics,
  which means an item's time in the inbox is invisible to cycle-time statistics.
  "How long did this sit before anybody looked at it?" is a question the inbox
  makes possible to ask and this design does not answer.
- **Two reserved concepts now live in `project.yaml`** — the category vocabulary
  and the status list — and a user can create a status in the `triage` category
  with any name they like, including several. `TriageStatus()` returns the first,
  which is arbitrary the moment there are two.

## Alternatives considered

- **A new `ItemType`, e.g. `inbox`.** Rejected: types are baked into the ID
  grammar (`<KEY>-<EP|US|T|M>-<NNNN>`, ADR-008) and into the folder layout.
  Accepting an item would mean changing its type, therefore its id, therefore
  breaking every reference to it — and ids are permanent (R-ID-3). An id that
  changes on acceptance is not an id.
- **A separate folder, `.pmngr/inbox/`.** Rejected: accepting an item would be a
  file move, which git handles but the id allocator, the index, the comment
  folders and every stored path do not handle for free. It also duplicates the
  layout rules for a state that is meant to be temporary, and it makes the inbox
  invisible to every existing query instead of explicitly excluded from it.
- **A stored `is_inbox: true` boolean on the item.** Rejected: two sources of
  truth that can disagree, and they will — a merge that takes the status from one
  side and the boolean from the other produces an item that is in the inbox
  according to one field and not according to the other. The category alone cannot
  desynchronise from itself.
- **A label, e.g. `labels: [inbox]`.** Rejected: labels are a free, user-owned
  vocabulary (R-LBL-1 explicitly tolerates undeclared ones). Hanging exclusion
  from planning on a string anybody can add or remove by accident is not a model,
  it is a convention with consequences.
- **A scheduler or background job for snooze expiry.** Rejected outright: it
  contradicts the product — there is no server (ADR-002), the browser-only mode
  has no process at all, and a repository that nobody opens must still be correct.
  A query-time comparison is the only form of the rule that works in every mode.
- **A `snoozed_until` sweep on index build, rewriting the files.** Rejected: it
  turns a read into a write, produces commits nobody asked for, and makes the
  answer depend on who opened the repository last.
