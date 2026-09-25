# 14 — Upgrading a project to specs (schema 2)

> **Audience.** Whoever maintains a git-in-track backlog that other people, agents or CI jobs
> write to, and wants to start using specs and requirement blocks
> ([ADR-037](./adr/ADR-037-specs-with-requirement-blocks.md), [doc 03 §21](./03-data-model.md#21-specs-and-requirement-blocks)).
> The CHANGELOG upgrade note says *what* changed; this guide says *what to do, in which order*.

## 1. Which projects need schema 2

`schema` in `project.yaml` is the version of the whole `.pmngr/` layout (doc 03 §19, R-EVO-1).
A project needs `schema: 2` **only if it holds a spec construct** (R-SCHEMA-2-1):

- a file under `.pmngr/specs/` (an `SP` item);
- a link — item-level or in `requirements.R<n>.links` — of kind `implements`, `implemented_by`,
  `modifies`, `modified_by`, `supersedes` or `superseded_by`;
- a link target that is an `SP` ID or a requirement ref (`ACME-SP-0003.R2`).

`## Spec Delta` sections, requirement wikilinks, `Implements:` / `Verifies:` markers in code and
the `specs:` key of `project.yaml` are **not** spec constructs: an older binary reads them as
prose, a broken wikilink, a comment or a preserved unknown key. A project that never uses specs
stays at `schema: 1` and nothing in this guide applies to it. New projects are still created at
`schema: 1` (R-SCHEMA-2-2).

Schema is **per project**. In a workspace with several projects, upgrade only the ones that
will hold specs; the others keep working with every binary.

## 2. What older binaries do with a schema-2 project

| Binary | On a `schema: 2` project |
|---|---|
| 2.1.0 and newer (supported schema 2) | Reads and writes it normally. |
| A future release built for a higher schema | Reads and writes it (schema 2 is below its supported version). |
| **Up to and including 2.0.1** (supported schema 1) | Reports `E-PROJ-SCHEMA` and **still writes**. It does not understand the spec link kinds or requirement-ref targets, reports them `E-ENUM` / `E-ID-GRAMMAR`, and may rewrite files that hold them. |

The write gate — every write refused while `project.yaml` declares no schema or one newer than
the build supports (R-EVO-2) — ships in 2.1.0, the release that introduces specs, not in 2.0.1, and
there is no backport (R-SCHEMA-2-4). So from the first spec on, **a single old binary, web
build or CI job that writes to the repository can damage spec links**. That is why the order
below matters.

A newer binary facing a schema it does not know (a future `schema: 3`) opens the project
read-only and says why; it never guesses.

## 3. The upgrade, in order

Do these steps **before** the first spec construct lands on the default branch. Steps 1–3 can
happen weeks ahead; nothing in them changes the repository's content.

### 3.1 Upgrade every binary that writes

Every machine and agent that runs `gintrack` against the repository — `gintrack serve`,
`gintrack mcp`, CLI scripts, the pre-push hook — needs a release that reports `schema v2`:

```bash
gintrack version        # the "core:" line must say "schema v2"
```

Homebrew, Scoop, the container image and `go install` all ship the same binary (doc 07 §2).
Agents configured through `.mcp.json` pick up whatever `gintrack` is on their `PATH`.

### 3.2 Upgrade every web app build

The web app ships inside the binary (`go:embed` of `web/dist`), so step 3.1 upgrades the
companion UI. A **browser-only** deployment — `web/dist` served from a static host, running the
Go core as WASM (doc 05 §1) — is a separate artifact: rebuild and redeploy it from the same
release (`make web`), and have users reload so the new `core.wasm` replaces the cached one.
An old browser-only build writes like an old binary (§2).

### 3.3 Upgrade CI

Any CI job that writes to the backlog (a bot that moves stories, a sync job, `gintrack doctor
--fix`) must pin the new release. Then add the requirement gate, so a pull request that breaks
or leaves unverified a requirement cannot merge unnoticed (`GIT-US-0133`, doc 09 §2):

```bash
make spec-check SPEC_BASE=origin/<base-branch>
```

In a repository that is not git-in-track itself, copy what the target does: run the test suites
with JSON output, `gintrack spec ingest` the reports, then
`gintrack spec impact --since origin/<base> --tiers 1,2 --fail-on failing,suspect` and fail the
job on exit `7`. A project without specs, or still on `schema: 1`, passes with `0 hits`, so the
gate can be added before the upgrade.

### 3.4 Ignore the verification cache

`gintrack spec ingest` writes the derived verification cache `<docs>/.pmngr/verify.json`
(`GIT-US-0141`, doc 03 R-LOC-5). Backlogs created by `gintrack init` from this release already
ignore it; an existing backlog must add it, or the cache shows up as an untracked change:

```bash
printf 'index.json\nverify.json\n' >> docs/.pmngr/.gitignore
```

(or the same two lines, with their `.pmngr/` path, in the repository's `.gitignore`).

### 3.5 Optionally install the pre-push hook

The same gate can run before every push on each developer's machine (`GIT-US-0134`, doc 07
§4.21):

```bash
gintrack spec hook install                  # add --project ACME in a multi-project workspace
```

It reads results already ingested and runs no tests, so run `make spec-check` (or the suites and
`gintrack spec ingest`) first. In a Jujutsu repository it prints an equivalent `jj` alias
instead. This step is optional; the CI gate of §3.3 is the one that protects the branch.

### 3.6 Raise the schema

With every writer upgraded, raise the schema as **its own reviewable change**:

```bash
gintrack migrate --to 2 --dry-run    # shows "- schema: 1" / "+ schema: 2" per project
gintrack migrate --to 2              # one project in the workspace
gintrack migrate --to 2 --project ACME
gintrack migrate --to 2 --all        # every project of the workspace
```

The command (doc 07 §4.22) changes the one `schema:` line of each selected `project.yaml` and no
other file; comments and formatting are kept. It does not commit: review the diff and commit it
alone, for example `chore(docs): raise ACME to schema 2 for specs`, so the pull request that
flips the schema is easy to find in history. Running it again changes nothing.

This step is optional in the strict sense: the first write through the vault that introduces a
spec construct raises the schema in the same write and reports `schemaUpgraded: 2`, and
`gintrack doctor --fix` raises it for constructs written by hand (R-SCHEMA-2-3). Doing it
explicitly moves the one-line change out of a feature pull request and makes the moment every
writer must be upgraded a deliberate, reviewed decision.

### 3.7 Write the first spec

Create the first spec from the web app, `gintrack item new --type spec …` or the MCP `create_spec`
tool. `gintrack doctor` should report no `E-SCHEMA-FEATURE` finding afterwards.

## 4. There is no downgrade

`gintrack migrate --to 1` on a `schema: 2` project is refused (exit 3) with the reason; so is a
project that declares a schema newer than the binary supports. There is no content migration
from 2 back to 1: spec links would become invalid. If the upgrade was premature and no spec
construct has been written yet, revert the commit that raised the schema. Once specs exist, the
project stays at schema 2.

## 5. Checklist

- [ ] Every `gintrack` binary that writes reports `schema v2` (`gintrack version`).
- [ ] Every browser-only web build is redeployed from the same release.
- [ ] CI jobs that write are pinned to the release; `make spec-check` (or its equivalent) gates
      pull requests.
- [ ] `verify.json` (and `index.json`) are git-ignored.
- [ ] Optional: `gintrack spec hook install` on developer machines.
- [ ] `gintrack migrate --to 2 --project <KEY>` committed as its own change.
- [ ] First spec written; `gintrack doctor` is clean.
