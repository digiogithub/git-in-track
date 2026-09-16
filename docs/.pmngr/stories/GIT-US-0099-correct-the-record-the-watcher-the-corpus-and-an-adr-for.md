---
id: GIT-US-0099
type: story
title: "Correct the record: the watcher, the corpus and an ADR for the reversal"
status: done
priority: medium
parent: GIT-EP-0020
milestone: GIT-M-0013
author: mcp
labels: [docs]
created: 2026-09-16T12:47:12Z
updated: 2026-09-16T15:56:15Z
started: 2026-09-16T15:56:06Z
closed: 2026-09-16T15:56:15Z
---

## Description

As a reader of this repository, I want the documentation to state what is true about Pando and to say plainly where it was wrong before, so that nobody rebuilds the corpus from a stale warning.

Two claims are repeated across the documentation, the templates and the code comments. One is false, the other is true but was applied to the wrong thing:

- *"Pando's KB watcher re-writes the documents it processes without parsing their front matter."* False for the current version. The watcher performs no file write at all; the bug was real, damaged database metadata rather than files, and is fixed.
- *"Pando's directory walk has no hidden-directory or `node_modules` exclusion."* True of the **KB** walk, and the reason `KBPath` is the documentation directory rather than the repository root. It is **not** true of the code indexer, which skips them.

Correct every occurrence, and record the reversal in an ADR. There is no ADR for the original decision, so the new one supersedes nothing and has to carry the original reasoning itself, along with the evidence that overturned it and the alternatives rejected: moving the corpus into the repository, keeping it behind a flag, writing derived `tags`/`aliases` into source front matter, and fixing Pando's mirror-write serializer instead of restricting the allow-list.

## Acceptance Criteria

- [ ] A new ADR records the decision, states the original reasoning, the evidence that overturned it, the rejected alternatives and the negative consequences — including the loss of tag filtering inside Pando and the fact that the allow-list, not a code boundary, is what keeps the writing KB tools away from the repository.
- [ ] `docs/21-semantic-search.md` is rewritten around the two indexations; §4.1 "Never point `KBPath` at a repository" becomes a correct statement about the KB walk and the documentation directory rather than a blanket prohibition.
- [ ] `docs/20-agent-interface.md` §3.4, its architecture diagram, its security section and its two troubleshooting rows about missing tags no longer describe a corpus or a destructive watcher.
- [ ] `docs/07-cli-and-api.md` (the `agent init` description and the search settings payload) and `docs/05-web-app.md` (the settings card) match the shipped behaviour.
- [ ] `CHANGELOG.md` records the reversal and says the old corpus directory is safe to delete by hand.
- [ ] `docs/03-data-model.md` R-CF-4 gains a note that Pando reads unknown front-matter keys into its search metadata, so an `x-` key is visible to semantic search.
- [ ] The research note `docs/research/2026-09-13-pando-gap-search-and-fit.md` is annotated rather than edited: it was accurate when written and its conclusion is now superseded.
- [ ] `GIT-EP-0019` and `GIT-US-0073` carry a comment pointing at this epic, so a reader of the closed work finds the reversal.

## Notes

Every occurrence found so far: `docs/20-agent-interface.md:29-41,62,82,198,204,255-271,331-332,393-395,421-422`; `docs/21-semantic-search.md` in full, especially `:109-137`, `:139-180`, `:182-231`, `:241-247`; `docs/07-cli-and-api.md:299,391-394,1673,2879-2915`; `docs/05-web-app.md:534-547`; `docs/08-mcp-server.md:865,907`; `docs/11-roadmap.md:940`; `docs/README.md:32`; `CHANGELOG.md:82-93`; `internal/pandosync/doc.go:5-28` and `paths.go:16-19` (deleted with the package); `cmd/gintrack/agent.go:152,414-415`; `cmd/gintrack/agent_merge.go:38,52-54`; `cmd/gintrack/templates/agent/pando.toml.tmpl:150-162`; `internal/pando/rest.go:27`.

Evidence to cite in the ADR: Pando tree at commit `710a39281`; `internal/rag/kb/watcher.go` holds no write call; `internal/rag/kb/repair.go:22-40` documents the historical bug as database metadata loss; the fix landed under PANDO-US-0003/0004; `KBWatch = true` is Pando's default (`internal/config/config.go`); mirror writes come from `internal/llm/tools/remembrances_kb.go:200,214,482`, `internal/llm/tools/remembrances_memory.go:280`, `internal/rag/kb/memory.go` and `internal/api/handlers_remembrances_kb.go:134`.

Do not delete the wrong statements silently. A reader who remembers the warning needs to find out it was withdrawn and why.
