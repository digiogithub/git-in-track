# ADR-001 — Markdown with YAML front matter as the storage format

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 0 (Foundations)
- **Related:** [ADR-002](ADR-002-git-as-only-sync.md), [ADR-008](ADR-008-id-scheme.md), [ADR-012](ADR-012-comments-as-separate-files.md)

## Context

git-in-track stores every artifact — epics, stories, tasks, milestones, comments,
boards, sprints, retrospectives and knowledge-base pages — in a git repository.
The storage format therefore has to satisfy an unusually wide set of readers:

1. **Git** must produce diffs a human wants to read in a pull request, and must
   merge concurrent edits without a custom merge driver in the common case.
2. **Humans** must be able to open the file in any editor and change it correctly
   without documentation open beside them.
3. **The application** needs typed, queryable fields (status, assignees, labels,
   priority, estimates, relations) to build an index and render boards.
4. **AI agents** must be able to read a file and understand it with no schema
   fetch, and write one back without corrupting it.
5. **Other tools** — GitHub's Markdown preview, Obsidian, Logseq, `grep`, `less` —
   should render or at least display the content usefully with zero configuration.

A pure structured format (JSON, YAML, TOML) satisfies (3) and fails (2) and (5) for
long-form prose. A pure prose format satisfies (2) and (5) and fails (3). The
combination of prose plus a typed header is a well-trodden solution: it is what
Jekyll, Hugo, Astro, Obsidian and Zettelkasten tooling all converged on
independently.

## Decision

Every artifact is a single UTF-8 Markdown file whose first bytes are a YAML front
matter block delimited by `---`, followed by a free Markdown body.

```markdown
---
id: ACME-US-0042
type: story
title: Login with SSO
status: in_progress
priority: high
parent: ACME-EP-0007
assignees: [dana]
labels: [auth, backend]
estimate: 5
created: 2026-08-21T09:14:00Z
updated: 2026-09-01T10:22:11Z
---

## Description

...

## Acceptance Criteria

- [x] SAML metadata endpoint published
- [ ] Group claims mapped to roles

## Notes

...
```

Supporting rules:

- **Front matter is flat and typed.** Scalars, string lists, and dates in ISO-8601.
  No nested objects except the small, documented shapes that genuinely need them
  (`links[]`, board `columns[]`). Nesting hurts both diffs and agents.
- **`rev` is never stored.** It is a content hash computed on read
  ([ADR-002](ADR-002-git-as-only-sync.md)); storing it would make every save a
  self-invalidating write and would conflict on every merge.
- **Unknown keys are preserved verbatim.** A field a user or another tool added
  survives a round trip through our writer untouched.
- **Serialisation is stable.** Existing keys keep their original order and scalar
  style; new keys are appended in a documented canonical order. An unmodified file
  saved through the UI produces an empty diff.
- **The body is a contract by convention, not by schema.** `## Description`,
  `## Acceptance Criteria` (task lists) and `## Notes` are recognised for metadata
  extraction, but any Markdown is legal and nothing is lost if the conventions are
  ignored.
- **Markdown flavour:** GFM plus footnotes, callouts/admonitions, wikilinks
  `[[Page]]`, optional math, and Mermaid fenced blocks.
- **One artifact, one file.** File name `<ID>-<slug>.md`.

## Consequences

**Positive**

- `git log -p` on a file is that story's complete history, with author and date, for
  free. `git checkout v1.4.0` restores the backlog as it was at that release.
- Pull requests can review plan changes exactly like code changes.
- Text merges work: two people editing different fields of the same item usually
  merge cleanly because they touch different lines.
- Any editor, any platform, offline, forever. The data outlives the application.
- Agents read and write the format natively; no client library is required.

**Negative**

- **No referential integrity.** Nothing prevents a `parent` pointing at a deleted
  epic. The core must validate continuously and surface diagnostics rather than
  assume consistency.
- **No transactions.** A board move touches an item file and a board file, possibly
  in two repositories. Partial application is possible and must be handled in the UI
  and in the sync flow.
- **YAML is a sharp tool.** Norway problem (`no` → `false`), sexagesimal-looking
  strings, tabs, and duplicate keys are real. We mitigate with a strict decoder, a
  linter in `gintrack doctor`, quoting rules in the writer, and no custom tags or
  aliases.
- **Renames are ambiguous.** Changing a title changes the slug and therefore the
  filename; the ID in front matter is what gives an item continuity.
- **Large fields cost.** A very long body makes every scan read more bytes; the
  indexer mitigates this by not retaining bodies in memory.

## Alternatives considered

- **One JSON or YAML file per item, with the description as a string field.**
  Trivial to parse and validate, but the description — the part humans actually
  write — becomes an escaped blob with `\n` sequences, unreadable in a diff and
  hostile to editing. Rejected.
- **A single database file (SQLite) committed to the repo.** Excellent queries and
  real integrity, but it is a binary blob: no diffs, no merges, no `grep`, no
  GitHub preview, no agent access without a driver. It contradicts "files are the
  API". Rejected.
- **One file per item with TOML front matter.** TOML is less ambiguous than YAML,
  but it is markedly less common in the Markdown ecosystem; Obsidian, GitHub and
  most static site generators expect YAML. Interoperability won.
- **Block-level storage (Logseq-style outlines).** Powerful for thinking, terrible
  for git: fine-grained blocks produce noisy diffs and merge conflicts on almost
  every concurrent edit, and agents must reconstruct a document from fragments.
  Rejected.
- **Markdown with an HTML-comment metadata block.** Invisible in rendered previews,
  which sounds attractive, but it is unparseable by every other tool in the
  ecosystem and unfamiliar to users. Rejected.
