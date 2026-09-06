---
id: GIT-US-0036
type: story
title: Hold several teams and choose the active one
status: in_review
priority: high
parent: GIT-EP-0009
milestone: GIT-M-0009
author: team
labels: [core, web, server]
estimate: 8
created: 2026-09-05T00:00:00Z
updated: 2026-09-05T00:00:00Z
---

## Description

As a person working with two squads, we want a workspace to hold both team repositories and
to choose which one we are looking at, so that mounting the second team is worth doing.

A workspace held one team, first one wins. `Workspace.TeamMount` returned the first mount
whose folder held a `team.yaml`, and `Workspace.Diagnostics` reported every other one at
**error** severity — "a workspace holds one team repository; this one is ignored". A second
team therefore registered happily and then did nothing, and a limitation was reported as if
it were a broken file.

Everything a team owns goes through that one choke point: `boardContext` resolves the team
mount, and the sprint and retro contexts are built on `boardContext`, so boards, sprints,
retrospectives and the team knowledge base all follow whatever it returns. The web app had
no notion of an active team at all — `getTeam()` took no id, `useQuery({ queryKey: ['team'] })`
took no parameter, and `GET /api/v1/teams` built a zero-or-one list from a single `team.get`
while `GET /api/v1/teams/{key}` ignored the key it was given.

This story makes several teams legal and makes the choice explicit. The active team is
**client state**: the web app remembers it per workspace and sends it as `team` on every
team-scoped call, and no host stores one, so the companion — one process serving every tab —
and browser-only mode behave identically. The Go side gains one resolution rule, applied in
one place: an omitted team selects the only open team, and is refused rather than guessed as
soon as there are two. See [ADR-019](../../adr/ADR-019-active-team-is-client-state-threaded-per-call.md)
and doc 04 §3.8.

## Acceptance Criteria

- [x] A workspace mounts every team repository instead of ignoring all but the first, and
      exposes them all (`Workspace.TeamMounts`, `Workspace.Teams`).
- [x] A second team repository is no longer an error diagnostic; two teams declaring the
      same `key:` are, at error severity.
- [x] Every team-scoped call takes the team it acts on — `team.get`, `board.*`, `sprint.*`,
      `retro.*` and `snapshot.*` — as the `key:` of a `team.yaml` or the id of the
      repository holding it.
- [x] An omitted team selects the only open team, so a single-team workspace, the CLI and
      the MCP server are unchanged; an omitted team with two open is refused with
      `invalid_request`, and an unknown one with `not_found`.
- [x] `GET /api/v1/teams` lists every mounted team, and `GET /api/v1/teams/{key}` resolves
      the key rather than ignoring it. Team-scoped routes accept `?team=` or a `team` field.
- [x] Each team knowledge base is reachable by its own key at `/teams/{key}/kb`.
- [x] The web app holds an active team, shows it in a selector wherever team artifacts are
      (boards, sprints, retros, the team panel) and lets the user switch.
- [x] The choice survives a reload, per workspace, and falls back to the first open team
      when the remembered one is no longer mounted.
- [x] Boards, sprints, retros, sprint metrics and the team knowledge base all follow the
      active team, in both operating modes.
- [x] Go table-driven tests cover a workspace holding two team repositories, in companion
      and browser mode; Vitest covers switching teams and the persistence of the choice.
- [x] Docs 02, 04, 05 and 07 updated, the open question at docs/05 §18.4 answered in the
      document, and ADR-019 records where the active team lives.

## Notes

The team is deliberately **not** in the URL. A shared `/boards/<slug>` link resolves against
the active team of whoever opens it; moving the team into the route is the open question
that replaces the one this story answered in doc 05 §18.

The empty state shown when no team is open at all belongs to GIT-US-0034 and GIT-US-0035,
which make it actionable. The selector renders nothing there rather than saying the same
thing twice, and nothing at all while a single team is open — there is no choice to make.
