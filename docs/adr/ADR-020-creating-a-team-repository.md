# ADR-020 — The product creates team repositories; registering one stays with the CLI

- **Status:** Accepted
- **Date:** 2026-09-05
- **Phase:** Post-1.0 (Team workspaces, `GIT-EP-0009`)
- **Related:** [ADR-007](ADR-007-team-repo-references.md), [ADR-018](ADR-018-bounded-project-discovery.md)
- **Implements:** `GIT-US-0034` — Create a team repository, `GIT-US-0035` — Mount a team repository from the web UI

## Context

A team repository is the home of boards, sprints, retrospectives and the team
knowledge base (doc 04). It is discovered by one marker: a `team.yaml` at the
repository root.

Nothing in the shipped product wrote that file. There was `core.CreateProject`
and no `core.CreateTeam`, no vault method, no REST route, no CLI verb and no UI.
`gintrack init --team` only recorded a role in the configuration, and that role
is *reported, never enforced* — every team surface keys off a parsed root
`team.yaml`. So `gintrack add --team <folder without team.yaml>` succeeded
silently and every board, sprint and retro call then failed. The board and retro
empty states, meanwhile, told the user to "mount a team repository" while
offering no way to do it.

Two questions had to be answered to close that gap.

**1. Is a team repository with no projects and no members valid?** The validation
of doc 04 §3.5 made both "none is declared" cases errors. A team repository is
created before anybody has been listed on it and before any project has been
connected — connecting one is a separate, deliberate act that needs a remote URL,
a docs path and a project key. Under the old rules the product would have written
a file and then refused to load it.

**2. Who registers a repository in companion mode?** `POST /api/v1/repos` has
always answered 501. The web wizard needs *some* answer to "add this folder", and
browser-only mode answers it itself: its mount record in IndexedDB *is* the
registration. In companion mode the workspace is the user's configuration file,
which the process read at startup.

## Decision

**The shared core scaffolds team repositories, and every surface reaches the same
scaffolder — but registering a repository with the companion stays a CLI act.**

- `core.CreateTeam` (WASM-safe, `core.FS` only) writes `team.yaml`, the
  `.pmngr/boards|sprints|retros|index/` folders and a `knowledge/index.md`. It is
  reached from `gintrack init --team`, `gintrack add --team --key`, the vault
  method `team.create`, `POST /api/v1/repos/{id}/team`, and the add-repository
  wizard of the web app. One implementation, byte-identical files in both hosts.
- `core.MarshalTeamConfig` is the only emitter of `team.yaml`, so the round trip
  `LoadTeamConfig` → `MarshalTeamConfig` is byte-stable.
- **An empty `members:` and an empty `projects:` are warnings, not errors.** A
  malformed member or project entry stays an error. The scaffolder declares no
  project: a team repository listing a project nobody can reach is worse than one
  listing none (ADR-007).
- **A team repository is never overwritten.** A folder that already holds a
  `team.yaml` is refused with `ErrTeamExists` / `team_exists` / HTTP 409.
- **`gintrack add --team` refuses a folder with no `team.yaml`**, naming the
  command that fixes it, and `--key` creates the team repository in the same
  invocation. Registering something inert is no longer possible.
- **`POST /api/v1/repos` keeps answering 501**, now carrying the exact
  `gintrack add` command built from the requested `path`, `role` and `docs`. The
  companion-mode wizard surfaces that command. Nothing fakes a successful
  registration.

## Consequences

**Positive**

- A team can start from an empty folder in one command, or from the web app in one
  form, without hand-writing YAML.
- The file the browser writes and the file the companion writes are the same
  bytes, because both run `core.CreateTeam`.
- A registered team repository is a real one: the "role is inert" failure mode is
  gone at its source.
- The empty states can now honestly point somewhere: the create flow exists.
- The companion never writes the user's configuration behind their back, and a
  browser tab cannot add arbitrary paths of the machine to a workspace.

**Negative**

- **The validation of doc 04 §3.5 is weaker.** A `team.yaml` that lost its
  `projects:` list through a bad merge now loads with a warning instead of an
  error. The board surfaces have to say "this team declares no project" clearly,
  or the user sees an empty board with no explanation.
- **Two kinds of "add a repository" in the web app.** Browser-only mode completes
  the wizard; companion mode ends in a command to paste. That asymmetry is real
  and visible, and it will keep surprising people who switch modes.
- A freshly created team repository is empty in a way git does not record: the
  `.pmngr/` artifact folders carry no file, so a clone of the fresh repository
  will not have them until the first board is written.
- `team_exists` is one more code in the catalog every client switches on.

## Alternatives considered

- **Keep "no project declared" an error and make the scaffolder ask for a first
  project.** Honest to the old spec, and it puts a remote URL, a docs path and a
  project key between the user and a team repository they cannot yet describe —
  in the browser, where none of that information is at hand. Rejected.
- **Have the scaffolder invent a project entry from the repository itself.** It
  would write a `projects:` list nobody asked for, pointing at a repo that may not
  be the team's, and a wrong entry decides whether cards render live or from a
  snapshot. Rejected as actively misleading.
- **Implement `POST /api/v1/repos` in the companion.** It would let the web app
  finish the flow in both modes, at the price of a running process whose in-memory
  workspace disagrees with the configuration file it read, and of a browser tab
  that can mount any path on the machine. Rejected; the 501 carries the command
  instead.
- **Hide the add-repository wizard entirely in companion mode.** Simpler, and it
  leaves a user who followed a link from an empty state staring at a blank page
  with no explanation. Rejected in favor of showing the command.
- **Let `gintrack add --team` keep registering a folder with no `team.yaml`, and
  warn.** That is the shipped behavior, and the warning is exactly what nobody
  read before every team call started failing. Rejected: a refusal that names the
  fix is cheaper than a broken workspace.
