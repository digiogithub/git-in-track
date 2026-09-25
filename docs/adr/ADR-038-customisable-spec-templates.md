# ADR-038 — Spec templates are embedded, and a backlog may override them with files

- **Status:** Accepted — 2026-09-25, decided by the human maintainer: the direction (embedded
  by default, extractable, a file on disk wins) and, after review of the proposal, the details
  under "Decision" with one amendment (see "Decisions from review").
- **Date:** 2026-09-25
- **Phase:** 11 (Spec-driven development)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-003](ADR-003-shared-go-core-wasm.md),
  [ADR-037](ADR-037-specs-with-requirement-blocks.md)
- **Extends:** ADR-037 — adds one folder to the backlog layout of docs/03 §2. The spec and
  requirement grammar, the `<SPEC-ID>` placeholder rule and the lint rules are unchanged.
- **Implements:** `GIT-US-0162`

## Context

`GIT-US-0111` shipped two body templates inside the binary: `internal/core/templates/spec.md` (a
whole spec body — `## Purpose`, `## Scope`, `## Glossary` and one example requirement block headed
`### <SPEC-ID>.R1 — …`) and `internal/core/templates/requirement.md` (the text of one requirement
block below its heading: an EARS statement and one `#### Scenario:`). The Go core embeds them with
`go:embed`, so they are also inside `core.wasm`, and the web editor imports the very same files at
build time. The web editor prefills a new spec with the first and the *Add requirement* dialog with
the second.

A team that writes specs in its own house style — a `## Non-goals` section, a `GIVEN` step in every
scenario, a glossary of its domain — can only live with the shipped text or edit it every time. On
2026-09-25 the maintainer decided that the templates stay embedded by default, that a team can
extract them to files and customise them, and that a file on disk, when present, wins over the
embedded copy.

A template file inside the backlog is a new path in the data model, which AGENTS.md reserves to a
human decision, so the format is written down here before any code reads it.

Forces:

- **Files are the only truth, and they travel with git.** A team's template must be shared by
  everyone who clones the repository, in both operating modes, with nothing to configure per
  machine.
- **The WASM core** (ADR-003): the override has to be read through the `core.FS` the host hands the
  core, so browser-only mode sees exactly what the companion sees.
- **A broken template must not break authoring.** A template is a convenience; a typo in one must
  never stop anybody from creating a spec or a requirement.
- **The index must not mistake a template for an item.** A template is Markdown under `.pmngr/`,
  where every Markdown file has so far been an item or a comment.

## Decision

1. **Location.** A backlog may hold a folder `templates/` directly under `.pmngr/`
   (`<docs>/.pmngr/templates/`). It is not an item folder. It is created lazily, like every other
   folder of the layout, and never by `gintrack init` or `core.CreateProject` (docs/03 §2.2 stays
   byte-identical).
2. **File names match the embedded ones.** Exactly two files are recognised:
   `templates/spec.md` overrides the spec template and `templates/requirement.md` the requirement
   template — the names of `internal/core/templates/`. Nothing else is a template; a file with any
   other name inside `templates/`, or a subfolder, is reported `W-LAYOUT-STRAY` and ignored.
3. **Content is the same format as the embedded file.** A template is plain Markdown with no front
   matter: `spec.md` is a spec body, whose requirement headings name the spec as `<SPEC-ID>`
   (docs/03 §21.1, replaced by the allocated id when the spec is created); `requirement.md` is the
   text below one requirement heading. The shipped files are the reference examples.
4. **Precedence is per file: a valid file wins over the embedded copy.** Each template is resolved
   on its own, so a team may override one and keep the other. There is no merge of the two texts
   and no configuration switch in `project.yaml`: presence of a valid file is the switch, and
   deleting it returns to the embedded copy.
5. **An invalid file falls back to the embedded copy, with a warning.** An override is invalid
   when it cannot be read, is not valid UTF-8, or is empty or blank; `requirement.md` also when it
   holds a level-1 to level-3 heading outside a code fence or leaves a fence open (the text
   `create_requirement` would refuse); `spec.md` also when parsing it with the placeholder filled
   in yields an error-severity finding (`E-REQ-FOREIGN` for a heading naming a concrete spec,
   `E-REQ-DUPLICATE`, `E-ID-GRAMMAR`). An invalid file is reported as the new warning
   **`W-TEMPLATE-INVALID`** on that file, and the embedded template is used in its place. It is a
   warning, not an error: nothing in the backlog is wrong, and authoring keeps working.
6. **A valid override is linted, and lint never rejects it.** `gintrack doctor` (through the index)
   also runs the project's `specs.lint` rules over a valid override — the spec template's blocks
   with the placeholder filled in, the requirement template inside one probe block — and reports
   each `LINT-REQ-*` finding on the template file at **warning** at most, whatever severity
   `specs.lint` gives the rule (a rule at `off` still reports nothing). A template that lints badly
   is still used: every spec written from it is linted on its own when it is saved.
7. **The core resolves templates through the FS it is given.** `core.LoadSpecTemplates(fs,
   backlogDir, cfg)` reads the two files through `core.FS` and answers the effective texts, where
   each came from (`embedded` or the file path) and the diagnostics of decisions 5–6. The index
   calls the same function for doctor, and the vault serves it as the read method
   `spec.templates` (REST `GET /api/v1/projects/{key}/specs/templates`), so companion and
   browser-only mode answer identically.
8. **Where the effective template is used.** The web editor's *New spec* and the *Add requirement*
   dialog prefill from `spec.templates`; they keep the build-time embedded import only as the text
   shown before the answer arrives or when it fails. The MCP tools start from the effective template
   when the caller brings no text: `create_spec` without `body` writes the spec template (its example
   block becomes `R1`, which the agent then rewrites with `update_requirement`), and
   `create_requirement` without `text` writes the requirement template below the heading. The CLI
   does the same: `gintrack item new --type spec` without `--body` writes the spec template. The
   vault methods `item.create` and `requirement.create` themselves never inject a template: an
   empty body sent to them stays empty, so a person who deletes the prefilled text in the editor
   gets what they asked for.
9. **Export writes both files.** `gintrack spec templates export [--project KEY] [--force]
   [--dry-run] [--json]` writes both embedded templates into `<docs>/.pmngr/templates/` of one
   project; it accepts no template names (a team that wants one override deletes the other file).
   Per file: missing → `created`; byte-identical to the embedded copy → `unchanged`, not rewritten
   (so the command is idempotent); different → `skipped` unless `--force`, then `overwritten`.
   `--dry-run` reports the same outcomes and writes nothing. A skipped file is not a failure: the
   command exits 0 and names `--force`. `--project` is required when the workspace holds more than
   one project.
10. **The folder is committed.** Unlike `index.json` and `verify.json`, templates are a team
    decision and belong in git; the `.gitignore` of docs/03 R-LOC-5 does not list them.
11. **The index and doctor ignore templates as items.** The folder is a known entry of the layout
    (no `W-LAYOUT-STRAY` for it), its two files are never parsed as items, comments or knowledge
    base pages, never allocate or reserve an id, and are re-checked when a watcher reports a change
    to them. The knowledge-base walk already skips `.pmngr/`.
12. **No schema bump.** A template is not a spec construct (docs/03 §21.10): a `schema: 1`
    project may hold one, and a build that predates this ADR reports the folder as
    `W-LAYOUT-STRAY` and otherwise ignores it.

## Consequences

### Easier

- A team customises its spec style once, in a reviewed commit, and every surface — CLI, MCP, the
  companion and browser-only web app — starts new specs and requirements from it.
- Upgrading `gintrack` updates the embedded templates for every team that did not override them;
  a team that did keeps its own text until it runs `export --force`.

### Harder, and what we now live with

- Two sources of template text exist. `spec.templates` answers where each came from, and doctor
  reports an invalid file, so "why does my editor show the old text?" has an answer.
- A team override does not follow later improvements to the shipped templates. `export --dry-run`
  reports `skipped` for a file that differs from the embedded copy, which is the cue to compare.
- `create_spec` without a body now writes the template's example block as `R1`. An agent that
  wants an empty spec passes a body of its own.

## Decisions from review (2026-09-25)

The maintainer reviewed the proposal on 2026-09-25 and confirmed:

- **MCP creates start from the templates** (decision 8): `create_spec` without `body` and
  `create_requirement` without `text` fill in the effective template. Kept as proposed, with the
  `R1` caveat under "Consequences".
- **Location and sharing** (decisions 1 and 10): `<docs>/.pmngr/templates/`, committed with the
  backlog. Kept as proposed.
- **Amendment — the CLI matches MCP.** `gintrack item new --type spec` without `--body` also
  writes the spec template in effect, rather than an empty body; decision 8 now says so.
- Every other decision stands as proposed.

## Alternatives considered

- **Templates declared in `project.yaml`** (a `specs.templates` map of inline strings). Rejected: a
  multi-line Markdown template inside YAML is hard to edit and to review, and the file already has
  a job.
- **A path setting pointing anywhere in the repository.** Rejected: one fixed place is discoverable
  by humans and agents without reading configuration, and keeps the override inside the backlog it
  belongs to.
- **An invalid override is an error that blocks spec creation.** Rejected: a template is a
  convenience, and the embedded copy is always a correct fallback.
- **The vault injects the template into any empty create.** Rejected for the web editor path: a
  user who clears the prefilled body must get an empty body, not the template back.
