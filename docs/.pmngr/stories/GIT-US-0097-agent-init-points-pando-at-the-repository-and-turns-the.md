---
id: GIT-US-0097
type: story
title: agent init points Pando at the repository and turns the watcher on
status: backlog
priority: high
parent: GIT-EP-0020
milestone: GIT-M-0013
author: mcp
labels: [cli, security]
created: 2026-09-16T12:46:35Z
updated: 2026-09-16T12:46:35Z
---

## Description

As someone setting this up, I want `gintrack agent init` to generate a configuration that indexes my repository and keeps it fresh, so that the two-command procedure ends with working semantic search over the backlog I actually edit.

The generated `[Remembrances]` block changes: `KBPath` becomes the repository's documentation directory — the one `internal/config/repo.go:300` discovers, `docs/` here — and `KBWatch` becomes `true`, which is Pando's own default. The comments that justified the old values are replaced by the reason for the new ones: the documentation directory rather than the repository root, because Pando's KB walk has no exclusions of any kind (`internal/rag/kb/sync.go:125`) and `docs/.pmngr/` is precisely the half the code indexer cannot reach.

The watcher being safe is the other half. Pando's KB watcher performs no file write (`internal/rag/kb/watcher.go`); the tools that do are `kb_add_document`, `kb_delete_document` and the memory `remember`/`forget` path, which mirror to disk through `SerializeFrontMatter` and would rewrite an existing repository file with only Pando's typed keys, destroying `id`, `status` and `parent`. So the generated `[AGUI] Tools` allow-list must include the KB tools that read — `kb_search_documents`, `kb_get_document`, `kb_related_documents` — and exclude every KB tool that writes, with a comment saying why in one sentence.

`agent_merge.go` follows: `agentOwnedKeys["Remembrances"]` keeps its four keys, and `agentDivergenceReasons` for `Remembrances.KBPath` and `Remembrances.KBWatch` are rewritten — both currently state the retired rationale, and `KBWatch` would otherwise report a divergence against a user who is already correct.

Add the `--kb-path` override the command has never had, noted as a gap in the GIT-US-0069 hand-off.

## Acceptance Criteria

- [ ] A generated `.pando.toml` has `KBPath` at the repository's documentation directory, `KBWatch = true` and `KBAutoImport = true`.
- [ ] The generated `[AGUI] Tools` allow-list admits the reading KB tools and admits no KB tool that writes; a test names each excluded tool so adding one to Pando cannot silently widen the list.
- [ ] The `[Remembrances]` comments explain the documentation directory over the repository root, and say that the watcher is safe and why — with no claim that it rewrites files.
- [ ] Merging into a configuration that already has `KBWatch = true` reports no divergence.
- [ ] Merging into a configuration whose `KBPath` points at a repository root, or outside the repository entirely, reports a divergence whose reason is the current one.
- [ ] `--kb-path` overrides the generated value and is documented in `docs/07-cli-and-api.md` §4.18.
- [ ] `agent_test.go`'s assertions on `KBWatch = false` and on the "strips the metadata" comment are replaced, not deleted: the new comment text is asserted the same way.

## Notes

Tests that pin the retired values and must be reworked: `cmd/gintrack/agent_test.go:112-113` (`KBWatch = false`), `:127-129` ("strips the metadata"), `:451-471` (`TestAgentInitCorpusPathAndFlags`, which asserts `KBPath` does *not* point inside the repository); `cmd/gintrack/agent_merge_test.go:135,143,220,238,239`.

`[AGUI] Tools` and `[AGUI.Profiles.<name>]` shipped in Pando under PANDO-EP-0002, so the allow-list is available today and the template no longer needs its interim-boundary comment.

The allow-list interacts with the MCP gateway: with an `[MCPServers]` entry present, ToolDiscovery activates the gateway and MCP tools hide behind `tool_search`/`mcp_call_tool`, which is why the template sets `[ToolDiscovery] Enabled = false` and `[MCPGateway] Enabled = false`. PANDO-US-0031 makes that configuration deadlock on the first `gintrack_*` call; it is still open, and this story does not fix it. Say plainly in the generated comment which of the two configurations the user is in.
