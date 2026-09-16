---
id: GIT-T-0227
type: task
title: agent init merges into an existing .pando.toml instead of refusing
status: done
priority: high
parent: GIT-US-0069
milestone: GIT-M-0013
author: mcp
labels: [cli]
created: 2026-09-15T21:49:10Z
updated: 2026-09-16T05:28:01Z
started: 2026-09-15T22:00:09Z
closed: 2026-09-16T05:28:01Z
---

## Description

`gintrack agent init` today treats an existing `.pando.toml` as a hard conflict: `runAgentInit` (`cmd/gintrack/agent.go:228-240`) stats all three generated files and exits `exitConflict` (5) if any exists, and `--force` replaces them wholesale. That is wrong for the one file the user is most likely to already own: a real `.pando.toml` is a Pando-wide config, not a gintrack artefact, and the repository's own is 453 lines of user settings that predate this feature.

Make `.pando.toml` merge instead. The persona and skill files keep the existing refuse/`--force` behaviour — they are gintrack's own files.

**Merge is line-level text editing, not a decode/re-encode round trip.** No TOML library available to this repo can preserve comments: `go-toml/v2` and `BurntSushi/toml` have no comment API at all, and `go-toml` v1's `SetWithComment` is never populated by its parser — a real round trip of the template loses every comment, rewrites `'literal'` strings as `"basic"` (contradicting `tomlString`, `agent.go:510-535`), collapses multi-line arrays and indents nested tables. The template's comments carry the design rationale and three of them are asserted by tests (`agent_test.go:114` `":9777"`, `:123-129` `"strips the browser"` / `"strips the metadata"`), so losing them fails the suite and erases the reasoning.

So: locate each table by its header line, rewrite only the lines of the keys gintrack owns, and insert whole missing blocks — comments included — verbatim from the rendered template. Never re-serialise the file.

**Conflict policy: warn, do not overwrite.** The merge writes only into tables gintrack creates (`[MCPServers.gintrack]` and its `Auth`/`Headers` sub-tables, `[AGUI.Profiles.<persona>]`) and adds keys that are absent. Where a key already exists in a table gintrack shares with the user and its value differs from the template's, the merge leaves the user's value and reports the divergence on stdout with the recommended value and the one-line reason. Rationale: in this repository's own config, `[ToolDiscovery] Enabled = true` and `[MCPGateway] Enabled = true` are what make `gintrack_*` calls work at all, because with the gateway off Pando v0.705.1 deadlocks (PANDO-US-0031, see GIT-T-0118). Imposing the template's values would break a working setup.

The three `[MCPServers.gintrack]` auth branches are mutually exclusive — encrypted `Auth`, plaintext `Headers`, or the commented-out stub — so a re-run that changes the token mode must remove the branch it replaces, not leave two.

`--force` keeps its current meaning for every file: full overwrite.

## Acceptance Criteria

- [ ] Running `agent init` in a repository that already has a `.pando.toml` merges into it and exits `exitOK`; the persona and skill files still trigger the `exitConflict` refusal when they exist without `--force`.
- [ ] Every table, key, comment, blank line and array-of-tables the file already had that gintrack does not own survives byte-identical — including root-level keys written before the first table header, mixed key casing, and `[[evaluator.taskPatterns]]` entries.
- [ ] Tables gintrack owns and the file lacks are inserted whole, with the template's comments intact; `agent_test.go`'s existing comment-substring assertions pass against a merged file.
- [ ] A key gintrack owns that is absent from an existing shared table is added; a key that is present with a different value is left alone and reported on stdout with the recommended value and the reason.
- [ ] `[MCPServers.gintrack]` and `[AGUI.Profiles.<persona>]` are gintrack-owned outright: their keys are written to the template's values even when they already differ.
- [ ] Re-running after a token-mode change leaves exactly one of the encrypted `Auth`, plaintext `Headers` or commented-stub branches in the file.
- [ ] A backup is written before the first modification (`.pando.toml` is gitignored, so git is not a safety net) and its path is printed.
- [ ] The merged file keeps mode `0o600`.
- [ ] A merge that cannot locate a table header it needs fails without writing anything, naming the line it could not parse.
- [ ] `go test -race ./cmd/gintrack/...` covers: merge into the fixture's real-shaped config, the preservation guarantee, the divergence report, the auth-branch swap and the failure path.
- [ ] `docs/07-cli-and-api.md` §4.18, `docs/20-agent-interface.md` and `CHANGELOG.md` describe the merge, the conflict policy and the backup.

## Notes

Maintainer decisions, 2026-09-15: line-level textual merge over a library round trip; warn-do-not-overwrite over gintrack-wins on shared keys.

`TestAgentInitRefusesToOverwrite` (`agent_test.go:384-408`) asserts the current refusal *and* that the hand-edited `.pando.toml` is untouched. It has to be split: the refusal assertion moves to the persona/skill files, and a new test asserts the `.pando.toml` merge. `TestAgentInitForceOverwrites` (`:412-436`) stays as the `--force` path.

Test idiom: one `func Test…` per behaviour, the shared `newHarness(t)` from `harness_test.go:28`, `readGenerated(t, repo, name)` (`agent_test.go:63`), `installFakePando` / `emptyPATH` for the token path, and `for _, want := range []string{…} { strings.Contains }` for content assertions.

Keys gintrack owns, by table: `[AGUI]` 14 keys; `[AGUI.Profiles.<persona>]` `Base`, `Prompt`; `[ToolDiscovery]` `Enabled`, `Mode`; `[MCPGateway]` `Enabled`; `[PersonaAutoSelect]` `Enabled`, `PersonaPath`; `[Skills]` `Enabled`, `Paths`; `[MCPServers.gintrack]` `Type`, `URL`, `Timeout`; `[MCPServers.gintrack.Auth]` `Type`, `Token`; `[MCPServers.gintrack.Headers]` `Authorization`; `[Remembrances]` `Enabled`, `KBPath`, `KBAutoImport`, `KBWatch`; `[MCPServer]` `HttpEnabled`, `StdioEnabled`. Everything else in those tables belongs to the user.

The divergences this repository's own config would report today: `[ToolDiscovery] Enabled`/`Mode`, `[MCPGateway] Enabled`, `[Remembrances] KBPath` (points at the repository root, which the template comment warns against) and `KBWatch`, `[MCPServer] StdioEnabled`.
