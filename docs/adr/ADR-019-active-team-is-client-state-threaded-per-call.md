# ADR-019 — The active team is client state, threaded on every team-scoped call

- **Status:** Accepted
- **Date:** 2026-09-05
- **Phase:** 8 (Team workspaces)
- **Related:** [ADR-007](ADR-007-team-repo-references.md), [ADR-003](ADR-003-shared-go-core-wasm.md), [ADR-004](ADR-004-browser-only-file-system-access.md)
- **Implements:** `GIT-US-0036` — Hold several teams and choose the active one

## Context

A workspace is several repositories open at once (doc 04 §3.6). Until now it
could hold **one** team repository: `Workspace.TeamMount` returned the first
mount whose folder held a `team.yaml`, and every other one was reported by
`Workspace.Diagnostics` at **error** severity with "a workspace holds one team
repository; this one is ignored". Registering a second team therefore succeeded
and then did nothing, which is a limitation described as if it were a broken
file.

Boards, sprints, retrospectives and the team knowledge base all resolve through
that single choke point — `boardContext` calls `TeamMount`, and the sprint and
retro contexts are built on `boardContext`. So the moment a workspace may hold
two teams, every one of those calls has to say which team it means, and
something has to remember what the user chose.

Where that memory lives is the decision. Three options:

1. **Workspace state on the Go side.** `Workspace.SetActiveTeam(key)`, and every
   call keeps taking no team. Smallest diff, and wrong: the companion is one
   process serving every browser tab on the machine (and, with `--host`, other
   devices). One tab switching team would silently switch every other tab, and
   an in-flight request would be answered by whichever team won the race. It
   also puts view state in a process whose whole job is files, and it has no
   meaning at all in browser-only mode, where there is no shared process to hold
   it — the two hosts would then need two different mechanisms, which is exactly
   what doc 02 §2 forbids.
2. **A parameter threaded from the client, and nothing else.** Correct and
   stateless, but on its own it answers only half the story: the user still has
   to re-pick the team on every reload.
3. **Both, split by what each side is good at.** The Go side stays stateless and
   takes the team as a parameter; the web app owns the choice, remembers it, and
   passes it on every call.

## Decision

**The active team is client state. The Go side never holds one.**

- Every team-scoped method of the `CoreApi` contract accepts a `team` field
  (`vault.TeamScope`, embedded in the params of `board.*`, `sprint.*`, `retro.*`,
  `snapshot.*` and `team.get`). It takes the `key:` of a `team.yaml` or the id of
  the repository holding it.
- `Workspace.TeamMount(key)` resolves it. An **empty** key selects the only open
  team, so a single-team workspace — every workspace that exists today, plus the
  CLI and the MCP server — keeps working with no change at all. As soon as two
  teams are open, an empty key is refused with `invalid_request` naming the
  field to set, rather than answered by an arbitrary team.
- Over HTTP the same choice travels as `?team=` on every team-scoped route, or
  as `team` in the body of a write. `GET /api/v1/teams` lists every mounted team
  and `GET /api/v1/teams/{key}` resolves the key it is given.
- The web app holds the choice in its client store and persists it in
  `localStorage` under `gintrack:active-team:<workspace>`, where `<workspace>` is
  the companion URL, or `browser` in browser-only mode. Two workspaces open on
  one machine therefore remember their own team. The key travels into every
  TanStack Query key, so switching teams shows the other team's boards instead
  of the cached ones.
- A remembered team that is no longer mounted is not an error: the app falls
  back to the first open team and the selector shows which one answered.

Holding several teams stops being a diagnostic. What replaces it is a real
finding: two mounted repositories declaring the **same** `key:` are reported at
error severity (`E-TEAM-KEY`), because a request naming that key could then be
answered by either one.

## Consequences

**Good.**

- Identical behaviour in both hosts, by construction: the browser and the
  companion receive the same field in the same place, and neither keeps session
  state. Nothing to reconcile when the companion appears or goes away mid-session.
- Two tabs can look at two different teams of the same workspace at once.
- Every answer is a pure function of the request plus the files on disk, which
  keeps the companion cacheable, restartable and testable.
- The refusal is loud where it matters: a call that would have been guessed for
  the user is refused with the field to set.

**Costs.**

- Every team-scoped signature grew an optional parameter — in the Go params
  structs, in the `DataProvider` interface and in the three providers. Optional
  and trailing, so no existing caller had to change.
- The choice is per browser profile, not per user: a second browser starts on
  the first team again. That is the same trade-off the rest of the app's view
  state already makes (doc 05 §2), and `localStorage` is never a source of truth.
- A shared link to `/boards/<slug>` does not carry the team. The slug is resolved
  against the active team of whoever opens it. Putting the team in the URL is the
  natural next step and is deliberately left out of this story.
