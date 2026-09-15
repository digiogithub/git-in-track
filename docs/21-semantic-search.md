# 21 — Semantic Search and the Pando Corpus

Status: **as built** for the corpus exporter (`internal/pandosync`, `GIT-US-0073`);
**planning specification** for the search surface that reads it (`GIT-US-0082`, `GIT-US-0091`).
Phase: **Phase 9 — agent interface and semantic search** (`GIT-M-0013`)
Audience: contributors working on `internal/pandosync` and on the search features; anyone
configuring Pando against a git-in-track workspace.

git-in-track's own index answers exact questions: an id, a label, a status, a substring.
Semantic search answers the other kind — *"where did we decide how conflicts are merged?"* —
and that needs an embedding index. Pando already has one. Rather than teach git-in-track to
embed, the companion keeps a plain Markdown **corpus** on disk that Pando imports on its own
schedule, and searches it through Pando.

---

## 1. The corpus in one paragraph

The corpus is a directory of Markdown files, one per backlog item and one per knowledge-base
page, written by the companion and read by Pando. It is **derived data**: everything in it can
be rebuilt from the repository in one pass, nothing in it is authoritative, and deleting the
whole directory costs nothing but the next export. It lives **outside every repository**, so
nothing exported can ever be committed.

---

## 2. Layout

```
<corpus root>/
  GIT/                       # one directory per project key
    items/
      GIT-EP-0019.md         # one file per item, named by its permanent id
      GIT-US-0073.md
      GIT-T-0122.md
    kb/
      README.md              # one file per KB page, mirroring the docs folder
      21-semantic-search.md
      adr/
        035-ag-ui.md
  ACME/
    items/…
    kb/…
```

Rules the exporter enforces:

- An item file is named after its id, and the id alone decides its project directory. An item
  whose id does not parse is skipped and logged, never written to a guessed path.
- A KB page keeps the path it has inside its project's documentation folder, so
  `docs/adr/035-ag-ui.md` in the repository becomes `GIT/kb/adr/035-ag-ui.md` in the corpus.
- Every path is cleaned and then checked to be inside `<project>/items` or `<project>/kb`.
  A page path containing `..`, a backslash, a NUL byte, a leading `/` or a Windows volume is
  refused with `ErrForbiddenPath` and nothing is written.
- Nothing but `.md` files lives in the corpus. A write goes through a sibling dot file named
  `.<name>.md.tmp` which is renamed into place, so an importer scanning the directory mid-write
  either sees the old file or the new one, never half of one, and never sees the temporary.
  A leftover temporary from a crash is pruned by the next full export.
- A file whose source has disappeared is pruned, and a directory the prune emptied is removed.

## 3. What a document looks like

```markdown
---
id: GIT-US-0073
type: story
title: 'Corpus exporter: keep a Pando-indexable copy of the backlog'
status: in_progress
milestone: GIT-M-0013
parent: GIT-EP-0019
project: GIT
updated: "2026-09-13T21:17:27Z"
tags: [GIT-US-0073, server, performance, story, in_progress]
aliases: ['Corpus exporter: keep a Pando-indexable copy of the backlog']
---

GIT-US-0073 — Corpus exporter: keep a Pando-indexable copy of the backlog

## Description

As a companion user, I want every item and KB page mirrored into a corpus directory…
```

A KB page is the same shape with `path` in place of `id` and `Page: <slug> — <title>` as its
first body line.

Three things about that document are deliberate.

**`tags` and `aliases` are the only keys Pando reads.** It parses front matter into a fixed
struct and discards everything else. The remaining keys are written for a human reading the
corpus and for `grep`; they are *not* a data channel. **Nothing downstream may read an item's
status, milestone or parent back out of a Pando search hit.** A consumer resolves a hit to an
item id and re-reads the authoritative fields from git-in-track's own index — which is also why
a stale corpus can never show stale field values in the UI, only a stale set of candidates.

**The id is repeated in the body.** Even `tags` is not guaranteed to survive: Pando's KB watcher
does not parse front matter and strips tags from every document it re-processes (523 of 539
documents in Pando's own knowledge base had already lost theirs when this was measured). The
first body line puts the id and the title inside the indexed chunk, where no front-matter parser
can lose it, so a hit is always resolvable back to an item.

**The body is copied verbatim.** `[[GIT-US-0024]]` wikilinks are left alone so that Pando builds
the same link graph the backlog has.

## 4. Sync model

```
gintrack ──writes Markdown──▶  corpus directory  ◀──imports on a schedule── Pando
   │                                                                          │
   └──── hub events (item.changed, file.changed, overflow) ────┘              ▼
                                                                    embedding index
```

The exporter runs a **full export** when the companion starts, in the background so the listener
is never delayed, and then keeps the corpus current from events. A full export writes only the
documents whose bytes changed — the exporter compares a content hash, from its in-memory cache or
from the file on disk after a restart — so a second export over an unchanged corpus rewrites
nothing. That matters beyond saving I/O: Pando's importer skips by mtime, so a needless rewrite
would make it re-embed a document that did not change.

Configure Pando like this:

```toml
[Remembrances]
KBPath = "/home/you/.cache/gintrack/pando-kb"
KBAutoImport = true
KBWatch = false
```

`KBWatch` stays **false** on purpose. The watcher is the component that erases `tags`, and
fsnotify on a large corpus is expensive for no benefit: the exporter has already written
complete files by the time an import pass runs. Forcing a re-sync today means waiting for the
next auto-import pass. Once Pando exposes `POST /api/v1/remembrances/kb/reindex`, the companion
will call it and "reindex now" becomes an awaitable operation with real counters.

### 4.1 ⚠️ Never point `KBPath` at a repository

Pando's directory walk has **no** hidden-directory exclusion and **no** `node_modules`
exclusion. Pointed at a repository root it will walk `.git`, `node_modules`, `dist` and every
build artifact, and — with `KBWatch` on — exhaust the host's inotify watches. `KBPath` must name
the corpus directory and nothing else. That is also why the exporter keeps the corpus free of
every file that is not an exported `.md` document, and why it refuses to start when the corpus
root turns out to be a git working tree.

### 4.2 Overflow means re-export, not drift

The companion's event hub drops a subscriber that falls behind, permanently. An exporter driven
by such a subscriber would stop receiving events and desync silently and for ever. So overflow
is an event of its own: `Event{Kind: pandosync.Overflow}` runs a full export, which rewrites what
changed and prunes what vanished. The same fallback covers a page removal the exporter cannot
resolve to a corpus path — a page it never saw exported — because doing nothing there would
leave an orphan document in the index.

## 5. The Go seam

`internal/pandosync` depends on `internal/core` and on a two-method source. It does **not**
import `internal/server`: the hub lives there, and the server adapts its own events to
`pandosync.Event` rather than the other way round.

```go
type Source interface {
	Items() []core.Item          // every indexed item, deleted ones included, with bodies
	Pages() []*core.KBPage       // every indexed KB page, with bodies
}

// Optional: lets Apply resolve one document without cloning the whole index.
type Lookup interface {
	Item(id core.ItemID) (core.Item, bool)
	Page(vaultPath string) (*core.KBPage, bool)
}

e, err := pandosync.New(pandosync.Options{
	Dir:        cfg.CacheDir(configPath) + "/pando-kb",
	Source:     vault,
	Project:    "GIT",          // fallback for a document that names no project
	OnProgress: publishProgress, // optional, for the hub
})

stats, err := e.ExportAll(ctx)                                    // full pass
stats, err = e.Apply(ctx, pandosync.Event{Kind: pandosync.ItemChanged, ID: "GIT-US-0073"})
last := e.LastExport()                                            // for the settings card
```

Event kinds: `ItemChanged`, `ItemRemoved`, `PageChanged`, `PageRemoved`, `Overflow`.
`Stats` carries `Items`, `Pages`, `Written`, `Removed`, `Skipped`, `Duration`, `At` and `Full`.

`Options.FS` replaces `Dir` with any `core.FS`, which is how the tests run the whole exporter
over an in-memory file system.

## 6. Searching the corpus

Search itself is specified with `GIT-US-0082`. The contract the corpus imposes on it is short:

- A Pando hit is a **candidate**, not a record. Resolve it to an item id — from the tags when
  they survived, otherwise from the identity line of the chunk — and read every field the UI
  shows from git-in-track's own index.
- An id that no longer resolves is a stale hit from a corpus Pando has not re-imported yet.
  Drop it rather than rendering a ghost.
- Do not use `code_index_project` for this corpus: `code_hybrid_search` excludes Markdown by
  default.

---

## 7. Operating notes

| Question | Answer |
|---|---|
| Can I delete the corpus? | Yes. It is rebuilt on the next start or on the next overflow. |
| Is it committed? | Never. It lives outside every repository, and the exporter refuses a corpus root that is a git working tree. |
| Does it contain secrets? | It contains exactly what the backlog and the KB contain. Treat it with the same care as the repository. |
| Why did my edit not show up in search? | The exporter wrote the file immediately; Pando imports on its own schedule with `KBWatch = false`. |
| Why are tags missing in Pando? | Its watcher strips them. That is why the id is also in the body, and why no consumer depends on front matter. |
