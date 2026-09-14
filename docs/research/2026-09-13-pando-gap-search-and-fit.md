---
title: Research — Pando semantic search fidelity and fit with the phase 9 plan
type: page
tags: [research, pando, search, kb]
---

# Pando ↔ git-in-track: semantic search and knowledge-base integration

Slice: search surface + KB integration (Part A) and fit review of GIT-EP-0018 / GIT-EP-0019 (Part B).
Pando at `/www/MCP/Pando/pando`, HEAD `d805b77a` (uncommitted changes confined to `cmd/` and
`internal/ipc/` — none touch `internal/rag`, `internal/api` or `internal/mesnada`, so nothing below is
affected by them). Pando paths below are relative to that root; git-in-track paths to `/www/git-in-track`.
Line numbers verified by reading the files.

---

## Summary table

| # | Finding | Evidence | Severity | Class |
|---|---|---|---|---|
| A1 | No REST route for KB / code / memory search — confirmed. Only 7 administrative routes exist | `internal/api/routes.go:144-152` | High | (c) Pando change, **S** |
| A2 | KB key space is flat and globally unique; no collection/namespace column | `kb_documents.file_path TEXT NOT NULL UNIQUE`, migration `20260311000001_add_kb.sql` | High | (c) **S** (prefix push-down) / **M** (column) |
| A3 | `tags`, `scope`, `exclude_outdated` are **all post-fusion Go filters**, not SQL; `tags` is fuzzy substring both ways | `internal/rag/kb/kb.go:726-742`, `:758-828` | High | (c) **S** |
| **A4** | **Front matter other than `tags`/`aliases` is silently discarded on the KBPath sync path** | `internal/rag/kb/frontmatter.go:14-31` (fixed struct, no inline map); `sync.go:214-228` | **Critical** | (c) **S** — breaks GIT-US-0073 as written |
| **A5** | **The KB watcher does not parse front matter at all — a modify wipes the tags the initial sync stored** | `watcher.go:163-191` vs `sync.go:214-228`; `updateDocument` = delete+add, metadata replaced wholesale (`kb.go:537-569`) | **Critical** | (c) **S** |
| A6 | Confirmed empirically in the live DB: 539 docs carry `source_mtime_unix`, only **16** still carry `tags` | `sqlite3 .pando/data/pando.db` | Critical | evidence for A5 |
| A7 | `kb_add_document` mirror write → watcher re-index feedback loop, no suppression | `remembrances_kb.go:200,214` → `filesystem.go:44` → `watcher.go:94-101`; origin tag exists (`observer.go:99`) but the watcher never consults it | High | (c) **S** |
| A8 | No MCP tool, no REST call and no CLI forces a KB re-sync. `SyncDirectoryWithStats` is exported and ready | `remembrances_kb.go:13-22` (deliberate), `sync.go:42`; `cmd/kb.go` has only `relink` | Medium | (c) **S** |
| A9 | KB sync + watch walk has **no** hidden-dir / `node_modules` exclusion | `sync.go:125-134`, `watcher.go:194-226` | Medium | (b) workaround: dedicated corpus dir |
| A10 | MCP HTTP transport on :9777 is **completely unauthenticated**, and CORS is `Access-Control-Allow-Origin: *` | `internal/mesnada/server/server.go:129`, `:148-160`, `:293-323` | **Critical** | (c) **S** |
| A11 | The hand-rolled `/mcp` handler **is** compatible with the MCP Go SDK v1.4.1 client git-in-track uses | analysis below | — | (a) works today |
| A12 | No embedding-dimension guard anywhere; mismatched chunks are silently skipped at query time | `kb.go:891-894` | High | (c) **S** |
| A13 | Vector search is a full scan that also joins the **whole document body per chunk**. Measured: 6 861 chunks ⇒ 136 MB read, **238 MB allocated**, 561 ms cold / 79 ms warm per query | benchmark below | High | (c) **S** (drop `d.content`) / **L** (ANN) |
| A14 | Two embedding models total (document, code) — global per instance, not per corpus or namespace | `internal/rag/service.go:20-25`, `:47-80`; `config.go:481-491` | Medium | (c) **M** |

---

# Part A — the Pando search surface

## A1. REST routes for search

### Verification

`internal/api/routes.go:144-152` is the complete remembrances block:

```go
144	// Remembrances
145	mux.HandleFunc("GET /api/v1/remembrances/projects", s.handleListCodeProjects)
146	mux.HandleFunc("POST /api/v1/remembrances/projects/index", s.handleIndexCodeProject)
147	mux.HandleFunc("POST /api/v1/remembrances/reindex", s.handleReindexAllCodeProjects)
148	mux.HandleFunc("POST /api/v1/remembrances/test-connection", s.handleTestEmbeddingConnection)
149	mux.HandleFunc("GET /api/v1/remembrances/embedding-models", s.handleListEmbeddingModels)
151	mux.HandleFunc("GET /api/v1/remembrances/enrichment", s.handleGetEnrichmentStatus)
152	mux.HandleFunc("PUT /api/v1/remembrances/enrichment", s.handleToggleEnrichment)
```

`POST /remembrances/reindex` is **code projects only** (`handlers_remembrances.go:122`), not KB.
`grep` for `kb_search|SearchDocuments|HybridSearch` across `internal/api/` returns nothing.
**The earlier report's claim stands: search is reachable only over MCP or an agent run.**

### Service objects already on `App`

`internal/app/app.go:104` — `Remembrances *rag.RemembrancesService`, and `internal/rag/service.go:20-25`:

```go
type RemembrancesService struct {
	KB     *kb.KBStore
	Events *events.EventStore
	Code   *code.CodeIndexer
	…
}
```

`internal/api.Server` already reaches it as `s.app.Remembrances.Code` (`handlers_remembrances.go:32, 37`).
Everything a REST handler needs is exported and nil-guarded by existing precedent:

| Route | Existing method | File:line |
|---|---|---|
| `POST /api/v1/remembrances/kb/search` | `KB.SearchDocumentsWithOptions(ctx, query, limit, kb.SearchOptions{Tags, SortByDate, ExcludeOutdated, Scope})` | `kb.go:671` |
| `POST /api/v1/remembrances/code/search` | `Code.HybridSearch(ctx, projectID, query, limit, langs, symbolTypes)` | `code/indexer.go:1248` |
| `POST /api/v1/remembrances/kb/documents` | `KB.AddDocument` / `KB.UpdateDocument` | `kb.go:171`, `:523` |
| `DELETE /api/v1/remembrances/kb/documents` | `KB.DeleteDocument` | `kb.go:431` |
| `POST /api/v1/remembrances/kb/reindex` | `KB.SyncDirectoryWithStats(ctx, KB.FilesystemMirrorPath(), true)` | `sync.go:42`, `filesystem.go:36` |
| (bonus) `POST /api/v1/remembrances/hybrid/search` | `Remembrances.HybridSearch` | `internal/rag/hybrid.go:34` |

The reindex handler is the cheapest: `FilesystemMirrorPath()` already returns the resolved absolute
`KBPath` (set at `internal/app/remembrances.go:89`), so it is ~20 lines plus a `sync.Mutex`.

### Auth those routes would inherit — **`X-Pando-Token`, automatically**

`internal/api/server.go:480-517` gates on path prefix, not a per-route list:

```go
496	if r.URL.Path == "/health" || !strings.HasPrefix(r.URL.Path, "/api/") {
497		next.ServeHTTP(w, r)          // unauthenticated
	…
511	if !s.hasValidToken(r) {
512		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
```

`hasValidToken` (`:448-453`) accepts the `X-Pando-Token` header **or** `?token=`, constant-time compared.
Any new `/api/v1/remembrances/**` route is therefore authenticated the moment it is registered — no
middleware wiring. CORS is already `*` with `X-Pando-Token` in `Allow-Headers` (`server.go:430-434`).

### Size

**S.** One new file `internal/api/handlers_remembrances_search.go` (~250-300 LOC) plus six lines in
`routes.go`. No new config, no dependency, no schema change.

Wrinkle for code search: `include_docs`, `min_score`, `offset`, `group_by_file` are **not** in
`CodeIndexer.HybridSearch` — they are applied by `rankAndFilterHybrid` in the tools package
(`internal/llm/tools/remembrances_code.go:396-417, :505`). Export that helper (+10 LOC) or accept
unfiltered results. A generic `POST /api/v1/tools/{name}/call` bridge next to the existing
`GET /api/v1/tools` (`handlers_tools.go:9`) would be ~60 LOC and cover every tool at once, but it
exposes *every* tool over REST — not recommended over five explicit routes.

## A2. Namespaces / collections — how several repositories coexist

`kb_documents` schema (read from the live DB, matching `20260311000001_add_kb.sql`):

```sql
CREATE TABLE kb_documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL UNIQUE,
    …
, memory_key TEXT NOT NULL DEFAULT '', memory_scope TEXT NOT NULL DEFAULT '', outdated …);
```

One global `file_path` unique index. The only namespacing concepts that exist:

- `memory_scope` — a *memory-only* prefix (`user/`, `project/`, `session/`), indexed
  (`idx_kb_documents_memory_scope`) but **never used in a search `WHERE` clause** (see A3).
- `KBPath` — one directory per Pando instance (`config.go:462`), resolved at `internal/app/remembrances.go:79-88`.

Pando is one instance per project (`ipc.lock`, `<Data.Directory>/pando.db`). So **one Pando = one
`KBPath` = one flat key space**. The code index is the counter-example: it *does* have a namespace
(`project_id` on `code_projects`/`code_symbols`, `indexer.go:122`) — exactly what the KB lacks.

### Can several git-in-track repositories coexist?

Only by convention, and the convention is weak:

- **Path-prefix works for uniqueness.** Export to `<corpus>/<project>/items/<ID>.md`; `file_path` is
  the key and is returned in every result (`remembrances_kb.go:304-315`), so the companion can resolve
  a hit back to its project by parsing the prefix. Zero Pando change.
- **Path-prefix does *not* work as a filter.** No parameter on `kb_search_documents` filters by path.
  "Search only project GIT" means: run over the whole corpus, get ≤20 results, drop the rest in the
  companion — silently under-returning whenever another project crowds the top 20.
- **`scope` cannot be repurposed.** `filterByScope` (`kb.go:806-815`) matches
  `strings.HasPrefix(r.Document.MemoryScope, scope)`, and `memory_scope` is only ever written by
  `UpsertMemory` (`memory.go:166-260`), which forces the `memory` tag and a TTL. Abusing it drags every
  document into the memory subsystem, the 180-day expiry and the GC (`memory_gc.go:11`). Don't.

### Which `kb_search_documents` filters are real?

| Filter | Where applied | Real or fuzzy |
|---|---|---|
| `query` | SQL: `kb_fts MATCH ?` + full vector scan | real |
| `limit` | trims **after** fusion and all filters (`kb.go:748-756`) | real, see below |
| `tags[]` | `filterByTags` → `matchesTags`, Go, **post-fusion** (`kb.go:728-730`, `:758-828`) | fuzzy, both directions |
| `exclude_outdated` | `filterOutdated`, Go, post-fusion, reads `Metadata["outdated"]` (`kb.go:733-735`, `:779-798`) | post-fusion |
| `scope` | `filterByScope`, Go, post-fusion, memory-only column (`kb.go:738-740`) | post-fusion, memory-only |
| `sort_by_date` | Go sort after filtering (`kb.go:743-747`) | post-fusion |

`matchesTags` (`kb.go:820-829`) is containment in **both** directions:

```go
if strings.Contains(dtLower, qt) || strings.Contains(qt, dtLower) {
```

so a query tag `status:backlog` matches a document tag `backlog`, and a document tag `s` matches
everything. Encoding structured fields as tags produces false positives.

Candidate pool is `limit*5`, sub-searches fetch `limit*15` (`kb.go:684-687`, `:697`). With `limit=20`
that is 100 fused candidates filtered down. For a 5 000-item backlog across three projects a
`tags:["GIT"]` filter starts from ~100 candidates and can return zero if the top 100 are all in another
project. **Tag filtering is a hint, not a filter.**

### Size of a `collection` / `namespace` column

- **Prefix push-down only — S.** Add `PathPrefix string` to `kb.SearchOptions` (`types.go:29-34`) and
  push it into both SQL queries as `AND d.file_path LIKE ? || '%'` (`kb.go:840-848`, `:945-961`).
  ~25 LOC, no migration, no new column — and it *also* shrinks the full vector scan by the selectivity
  of the prefix, the cheapest perf win available (A13). **Recommended.**
- **A real `collection` column — M.** Migration (`ALTER TABLE`, precedent:
  `20260611000001_add_kb_memory.sql` added seven columns the same way) plus `Document.Collection` plus
  the scan lists in `searchVector` (`kb.go:840-877`), `searchFTS` (`:945-995`), `GetDocument` (`:335`),
  `ListDocuments` (`:1024`), `listDocumentMetadata` (`:394`), `getDocumentMetadata` (`:366`) and the
  memory queries, plus an `AddDocument` signature change and every caller. ~15 call sites.

## A3–A7. The sync path — and the two bugs that break GIT-US-0073

### What `KBPath` + `KBAutoImport` + `KBWatch` actually does

`internal/app/remembrances.go:75-160`:
- `:79-88` — `KBPath` resolved against `config.WorkingDirectory()` when relative; **empty disables
  mirror, import and watch together**.
- `:89` — `ConfigureFilesystemMirror(kbPath)`; this is also what makes `kb_add_document` mirror to disk.
- `:96-100` — document converter (docx/pdf/xlsx/pptx → Markdown) when `KBConvertDocuments`.
- `:102-141` — `SyncDirectoryWithStats(ctx, kbPath, true)` in a background goroutine when `KBAutoImport`.
- `:143-160` — `WatchDirectory(ctx, kbPath)` in another goroutine when `KBWatch`.

### Create / modify / delete / rename

`watcher.go:106-192`:

| fs event | Behaviour | Line |
|---|---|---|
| create (dir) | added to the watch set recursively, event swallowed | `:83-92` |
| create (file) | `isIndexableFile` gate, then debounce → `AddDocument` | `:94-101`, `:182-187` |
| write/chmod | mtime compared to stored `source_mtime_unix`; unchanged ⇒ skipped; else `UpdateDocument` | `:150-161`, `:189-191` |
| **rename** | `DeleteDocument` **only** — the new path is re-added only if fsnotify also emits Create for it inside the watched tree | `:124-129` |
| remove / stat ENOENT | `DeleteDocument` | `:124-138` |

Debounce is **250 ms per path**, `time.AfterFunc`, keyed by absolute path (`:46-59`), cancelled on ctx
(`:63-70`). `isIndexableFile` = `.md` plus convertible documents when a converter is installed
(`sync.go:382-388`).

**No hidden-directory, `node_modules`, `.git` or symlink exclusion anywhere** — neither in
`addWatchRecursively` (`watcher.go:194-226`) nor in the sync walk (`sync.go:125-134`). The only skip is
permission errors (`:228-233`). GIT-US-0073's warning about never pointing `KBPath` at a repo root is
correct and important.

### A4 — front matter other than `tags`/`aliases` is discarded (**critical**)

`internal/rag/kb/frontmatter.go:14-31`:

```go
type FrontMatter struct {
	CreatedAt time.Time `yaml:"created_at,omitempty"`
	UpdatedAt time.Time `yaml:"updated_at,omitempty"`
	Tags      []string  `yaml:"tags,omitempty"`
	Aliases   []string  `yaml:"aliases,omitempty"`
	Key, Scope, Source string; Outdated bool; ExpiresAt *time.Time; Hits int; Importance float64
}
```

A **fixed struct with no inline map**. `yaml.Unmarshal` drops every unknown key. `sync.go:206-228` lifts
only two of them into metadata:

```go
214		fm, body, _ := ParseFrontMatter(res.content)
…
220		if len(fm.Tags) > 0 { meta = InjectTagsIntoMetadata(meta, fm.Tags) }
223		if len(fm.Aliases) > 0 { meta = InjectAliasesIntoMetadata(meta, fm.Aliases) }
```

Stored metadata is exactly `{source_path, source_mtime_unix, source_format[, converted][, tags][, aliases]}`
and nothing else. The body stored and embedded is front-matter-stripped text.

**This falsifies GIT-US-0073's central assumption**, quoted from its body:

> Each file carries YAML front matter with `id`, `type`, `status`, `milestone`, `parent`, `project`,
> `updated` … because `tags` and `aliases` are the only keys Pando reads specially
> (`kb/frontmatter.go:16-31`); **everything else lands in the searchable metadata.**

Everything else lands nowhere. Its own citation is the code that disproves it.

### A5 — the watcher never parses front matter, so a modify **erases** the tags (**critical**)

`watcher.go:163-180` calls `loadDocumentBody` (returns `content, format, converted, err` — no front
matter) and builds:

```go
173	metadata := map[string]interface{}{
174		"source_path":       absPath,
175		"source_mtime_unix": mtimeUnix,
176		"source_format":     format,
177	}
```

No `ParseFrontMatter`, no `InjectTagsIntoMetadata`. It then calls `UpdateDocument` (`:189`), and
`updateDocument` (`kb.go:537-569`) is **delete-then-add** — metadata replaced wholesale, not merged:

```go
563	if err := s.deleteDocument(ctx, filePath); err != nil { … }
568	return s.addDocument(ctx, filePath, content, metadata)
```

Consequences:
1. The first sync stores tags. The first *edit* deletes them.
2. `source_mtime_unix` is written correctly by the watcher, so the next startup sync sees the file as
   unchanged (`sync.go:150-157`) and **never repairs it**. The loss is permanent.
3. The raw front-matter block is left in the indexed body on the watcher path but stripped on the sync
   path, so the same document embeds differently depending on which path last touched it.

### A6 — confirmed in production data

`/www/MCP/Pando/pando/.pando/data/pando.db` (read-only query):

```
documents with source_mtime_unix : 539
documents with tags              :  16
```

Spot check against disk — `pando/plans/superpowers-opt-in-mode-implementation.md` has
`tags: [plan, superpowers, slash-commands, skills]` in its front matter and is stored as:

```json
{"source_format":"md","source_mtime_unix":1783979091,"source_path":"…/superpowers-opt-in-mode-implementation.md"}
```

No tags. Same for `pando/plans/custom_engine_templates_plan.md` and
`pando/plans/ask_user_question_tool_plan.md`. The two documents that *do* still have tags
(`opencode/research/tui_sidebar_info_column.md`, `pando/features/realtime_context_token_counter.md`)
also lack `source_format`, i.e. written by an older sync path and never touched by the watcher since.

**523 of 539 documents in Pando's own knowledge base have lost their tags.** The one filter
`kb_search_documents` offers is, in practice, empty.

### A7 — the mirror ⇄ watcher feedback loop

`kb_add_document` writes the DB *and* mirrors to `<KBPath>/<file_path>` with regenerated front matter
(`remembrances_kb.go:200, 214` → `filesystem.go:44-63`). With `KBWatch=true` the watcher sees that write.
The metadata stored by `kb_add_document` has **no** `source_mtime_unix`, so `metadataInt64(...)` returns
0 ≠ mtime (`watcher.go:156-161`) and the watcher runs `UpdateDocument` with bare `source_*` metadata —
clobbering the `metadata` and `tags` the tool just stored.

The infrastructure to stop this exists and is unused: `WithWriteOrigin(ctx, "watcher"|"sync"|"tool")`
(`observer.go:96-112`, set at `watcher.go:107` and `sync.go:45`) is only consumed by the optional write
observer, never by the watcher's own skip logic. There is no "recently mirrored, ignore the next event
for this path" set.

### A8 — forcing a re-sync

**No MCP tool, no REST route, no CLI subcommand.** The omission is deliberate for the *tool* surface —
`remembrances_kb.go:13-22` argues a path-rooted import produces unstable keys and says:

> Use `kb.KBStore.SyncDirectoryWithStats` from host code if another sync point is ever needed; do not
> expose it as a tool.

`cmd/kb.go` has exactly one subcommand, `pando kb relink`. But `SyncDirectoryWithStats` is exported and
`FilesystemMirrorPath()` gives the one rooting that produces stable keys, so a **REST** re-sync endpoint
(A1) sidesteps the objection entirely: the host chooses the root, not the model.

### Push (`kb_add_document`) vs pull (`KBPath` watcher) for git-in-track's exporter

| | `KBPath` + `KBWatch` (pull) | `kb_add_document` per item (push) |
|---|---|---|
| Rich metadata (`status`, `type`, `parent`, …) | **lost** (A4) | **preserved verbatim** — `metadata` stored as-is (`remembrances_kb.go:110-119`, `kb.go:234-240`) and **returned** in every search result (`remembrances_kb.go:296-316`) |
| Tags | lost on first edit (A5) | passed explicitly, then clobbered by the mirror loop if `KBWatch` is on (A7) |
| Deletions | pruned by `deleteMissing` on the next full sync (`sync.go:309-336`); watcher deletes immediately | explicit `kb_delete_document` |
| Chattiness | one fs write per item | one MCP round trip per item; each embeds synchronously (`kb.go:274-293`), so 5 000 items = 5 000 serialized embedding calls |
| Bootstrap | one `SyncDirectoryWithStats`, worker pool of `IndexWorkers` (`sync.go:89-119`) | 5 000 sequential calls |
| Transport needed | none (filesystem) | the MCP client of GIT-US-0077 |
| Auth surface | none | the unauthenticated :9777 (A10) |

**Recommendation, which reverses GIT-US-0073's Notes section:**

> Do not use `kb_add_document` per item instead: it is chattier and mirrors into `KBPath` anyway.

The chattiness objection is right; the conclusion is wrong, because the pull path cannot carry the
metadata the feature needs. The right shape given git-in-track's existing debounced watcher and event
hub is **hybrid**:

1. **Export to disk** as GIT-US-0073 describes (the companion already has the debounce and the atomic
   write; it gives a human-inspectable corpus and a cheap bootstrap).
2. **Set `KBPath` to that directory with `KBAutoImport=true` and `KBWatch=false`.** Turning the watcher
   off removes A5 and A7 in one config change, at the cost of freshness.
3. **Drive freshness from git-in-track's side** with a `POST /api/v1/remembrances/kb/reindex` call
   (A1, ~20 LOC in Pando) debounced behind the companion's own event hub. `SyncDirectoryWithStats` skips
   unchanged files by mtime, so a re-sync after one item changed re-embeds one document.
4. If Pando fixes A4 this becomes the whole answer with no per-item MCP traffic at all. **That fix is the
   single highest-value Pando change in this slice.**

`kb_add_document` remains right for a handful of documents where metadata matters more than throughput —
not for a 5 000-item backlog.

## A10–A11. MCP HTTP transport on :9777

### A10 — unauthenticated, and CORS-open

`cmd/mcp_server.go:77-78` sets the default port to 9777; `:161-175` constructs
`mesnadaServer.New(Config{Addr, Orchestrator, Version, UseStdio:false, Remembrances, PandoTools})`.
`Config` (`internal/mesnada/server/server.go:96-107`) has **no token, no auth, no TLS field**.
`New` (`:110-146`) mounts:

```go
129	mux.HandleFunc("/mcp", s.handleMCP)
130	mux.HandleFunc("/mcp/sse", s.handleSSE)
131	mux.HandleFunc("/health", s.handleHealth)
132	mux.Handle("/", s.newGinEngine())
…
140	Handler: s.corsMiddleware(mux),
```

`handleMCP` (`:293-323`) reads `Mcp-Session-Id`, decodes, dispatches. There is no `Authorization` check
anywhere in the package (`grep -n "Authorization|Bearer|token"` over `server.go` returns nothing).

Worse than "no auth": `corsMiddleware` (`:148-162`) sets `Access-Control-Allow-Origin: *` with
`Allow-Headers: Content-Type, Mcp-Session-Id`. Since `Content-Type: application/json` plus a custom
header makes the request non-simple, the browser preflights — and the server answers the preflight with
`*`. So **any web page the user visits can drive the full Pando MCP tool surface on localhost:9777**,
including `system-exec` and `file-tools-write` if those groups are enabled. Drive-by RCE shape, not
merely a missing token. Note also `pandoApp.Permissions.SetGlobalAutoApprove(true)` at
`cmd/mcp_server.go:125` — every tool call from that surface is auto-approved.

**Smallest change (S, ~50 LOC):**
1. `MCPServerConfig` (`config.go:594-620`) gains `HttpToken string` and `HttpAllowedOrigins []string`.
2. A `bearerMiddleware` wrapping the mux at `:140`, doing `subtle.ConstantTimeCompare` on
   `Authorization: Bearer <token>` — same shape as `internal/api/server.go:448-453` / `:480-517`, so
   there is in-repo precedent to copy.
3. `corsMiddleware` echoes an allow-listed `Origin` instead of `*`, and emits no CORS headers at all
   when the list is empty (loopback Go clients are not browsers and need none).
4. Print the generated token to stderr at startup the way `cmd/agui_serve.go` does for AG-UI.

Independently of the fix: **bind to loopback and treat :9777 as hostile until it is done.**

### A11 — go-sdk client compatibility: **yes, it works**

git-in-track depends on `github.com/modelcontextprotocol/go-sdk v1.4.1` (`go.mod:12`) and already uses
its *server* side (`internal/mcp/transport.go:40-45`, `:56-69`). Checked against
`$GOMODCACHE/github.com/modelcontextprotocol/go-sdk@v1.4.1/mcp/streamable.go`:

| Client behaviour | SDK | Pando | Verdict |
|---|---|---|---|
| POST with `Accept: application/json, text/event-stream` | `:1766` | accepts anything; `Accept` never inspected | OK |
| 200 + `application/json` single response | `:141` | `:321-322` always writes JSON | OK |
| Session id from `Mcp-Session-Id` response header, stored on first sight, mismatch = error | `:1811-1822` | minted at `:299-301`, echoed on **every** response at `:312` — always the same value | OK |
| `Mcp-Protocol-Version` header on later requests | `:1887` | ignored | OK (harmless) |
| Notifications expect `202` with no body | `:1830+` | `:315-320` returns bare `202` | OK — explicitly commented as spec compliance |
| Standalone SSE: `GET /mcp`, tolerates **405** | `:1638-1658` — 405 ⇒ close body, return, no failure | `handleMCP:294-297` returns 405 for non-POST | OK |
| `Close()` sends `DELETE /mcp`, **ignores the status** | `:2168-2191` — only transport errors recorded | 405 | OK |
| `404` ⇒ `ErrSessionMissing`, terminal | `:1992` | Pando never 404s a session; `getOrCreateSession` mints a new one | OK (a restarted Pando silently gives a fresh session — benign for stateless tool calls) |
| Protocol version negotiation | — | `:438-461` echoes `2024-11-05` / `2025-03-26` / `2025-06-18` | OK |
| JSON-RPC batches | not sent by v1.4.1 | `handleMCP` decodes a single request only | OK |

**Conclusion: GIT-US-0077 can be built today against Pando as-is, transport-wise.** Two caveats for the
story: `ReadTimeout: 30s` / `WriteTimeout: 0` (`server.go:141-144`) means a slow embedding call is not
cut off server-side but a slow *request body* is; and the session map (`:114`) has no eviction, so a
client that reconnects per call leaks a session struct per call — reuse one session, which GIT-US-0077
already requires.

## A12–A14. Embeddings

### A12 — no dimension guard

`kb.go:891-894`, inside `searchVector`:

```go
891		vec := deserializeFloat32(blob)
892		if len(vec) != len(queryEmb) {
893			continue // Skip dimension mismatch
894		}
```

The **only** place dimensions are ever compared, at query time, silently, per chunk. Nothing validates on
write: `addDocument` (`kb.go:274-300`) checks only `len(embedVecs) == len(chunks)`. Nothing records the
model or dimension that produced a chunk — `kb_chunks` has no `model` or `dims` column. Switching
`DocumentEmbeddingModel` degrades recall to zero with no error, no log line and no way to detect it.

**Fix (S→M):** store the dimension (or model id) at write time; compare at startup with
`embedder.Dimensions()`; log loudly and expose a `stale_chunks` count on the existing
`GET /api/v1/remembrances/enrichment` or a new status route. A migration plus ~60 LOC.

### A13 — the full scan, measured

`searchVector` (`kb.go:840-848`) issues:

```sql
SELECT c.id, c.document_id, c.content, c.embedding,
       d.file_path, d.content, d.metadata, d.created_at, d.updated_at, …
FROM kb_chunks c JOIN kb_documents d ON d.id = c.document_id
WHERE c.embedding IS NOT NULL
```

with **no `LIMIT`** — every embedded chunk, every query. Cosine is computed in Go (`kb.go:895`, `cosine`
at `:1180`), then a full sort. Note `d.content` — **the entire document body is returned once per chunk
of that document**, then discarded; `SearchResult.Document.Content` is never read by any caller.
Metadata JSON is unmarshalled per row (`:884-889`).

Benchmarked against the live database with the same driver Pando uses (`github.com/ncruces/go-sqlite3`,
WASM — `internal/db/connect.go`), replaying the exact query and the exact per-row work:

```
corpus : 540 documents, 6 861 embedded chunks, 768-dim (3 072-byte) vectors, mean chunk 693 chars
scan   : 136.3 MB materialised  (19.9 KB per row — because d.content repeats per chunk)
cold   : 561 ms,  238.5 MB allocated
warm   :  79 ms   (OS page cache warm)
```

Extrapolating to ~5 000 backlog items plus KB pages, 800-char chunks:

| Corpus | Chunks | Bytes/query | Est. warm latency | Est. alloc/query |
|---|---|---|---|---|
| 5 000 items only (short, ~1.5 chunks each) | ~7 500 | ~45 MB | 60–120 ms | ~90 MB |
| + 500 KB pages (~6 chunks each) | ~10 500 | ~75 MB | 100–200 ms | ~150 MB |
| 3 repositories sharing one Pando | ~30 000 | ~220 MB | 300–600 ms | ~450 MB |
| Pando's own KB added on top | ~37 000 | ~360 MB | 0.5–1.2 s | ~700 MB |

**The practical ceiling is roughly 25 000–35 000 chunks** before the vector leg alone exceeds ~300 ms,
and the binding constraint is allocation, not arithmetic: cosine over 10 000 × 768 floats is ~8 M flops,
single-digit milliseconds. Two cheap fixes move the ceiling ~4×:

- **Drop `d.content` from the `searchVector` and `searchFTS` selects** — nothing reads it. On the
  measured corpus that alone removes ~85 % of the bytes. **S**, two SQL strings and two `Scan` lists.
- **Push a path prefix into the `WHERE`** (A2) — narrows the scan by project.
- Real ANN (sqlite-vec, HNSW, IVF over a quantised copy) is **L** and not needed at this scale.

`internal/rag/types.go:1-14` claims sqlite-vec/`vec0` are in use. That comment is stale and wrong —
embeddings are little-endian float32 BLOBs with similarity in Go (`internal/rag/store.go:11-18`).
GIT-US-0082's Notes already say the right thing ("full scan … measure before promising anything at ten
thousand"). The measurement above is that measurement: **10 000 chunks is fine, 35 000 is not.**

### A14 — per-corpus vs global embedding model

Two embedders, both global to the Pando instance (`internal/rag/service.go:20-25`, built at `:47-80`
from `DocumentEmbeddingProvider/Model` and `CodeEmbeddingProvider/Model`, `config.go:481-491`):

- `docEmbedder` — the **whole** KB: every document, every memory, every namespace.
- `codeEmbedder` — the code index; deduplicated to the same instance when provider, model and base URL
  all match (`service.go:62-66`).

**There is no per-corpus, per-collection or per-project embedding model.** Combined with A12: if
git-in-track shares a Pando instance with any other consumer, they are locked to one document embedding
model forever, and whoever changes it silently blinds everyone. Live config (`.pando.toml:352-401`):
documents `ollama/nomic-embed-text:latest` (768), code
`ollama/hf.co/limcheekin/CodeRankEmbed-GGUF:Q4_K_M`, `ChunkSize=800`, `ChunkOverlap=100`,
`IndexWorkers=4`. The 3 072-byte blobs in the live DB confirm 768 dimensions.

GIT-US-0091's instinct to put a standing "the embedding model is pinned configuration" warning on the
settings card is right, and should be strengthened: **pin it in the companion config and refuse to start
semantic search if Pando reports a different one** — which needs a Pando route that reports the model,
i.e. extend `GET /api/v1/remembrances/embedding-models` (`handlers_embedding_models.go`) or add the
active model to the enrichment status.

---

# Part B — fit review of the git-in-track plan

Legend: **(i)** buildable today against Pando as-is · **(ii)** buildable with a git-in-track-side
workaround · **(iii)** blocked on a named Pando change.

## GIT-EP-0019 — Semantic search with Pando

### GIT-US-0073 — Corpus exporter · **(iii) blocked** — and its central assumption is wrong

**The exporter half is (i); the metadata contract is (iii).**

- The file-writing, debouncing, atomic-rename, path-safety and pruning work is all git-in-track-side and
  buildable today.
- **Wrong assumption (blocking):** "everything else lands in the searchable metadata" — it does not (A4).
  `id`, `type`, `status`, `milestone`, `parent`, `project`, `updated` are parsed into a fixed struct and
  discarded. Blocked on **PANDO: preserve unknown front-matter keys in document metadata (S)**.
- **Wrong assumption (blocking):** front matter `tags` survive. They survive the first import and are
  erased by the first edit through the watcher (A5; A6 shows 523/539 documents in production have
  already lost them). Blocked on **PANDO: parse front matter in the KB watcher (S)**.
- **Correct and well-sourced:** never point `KBPath` at the repository root (A9 confirms no
  hidden-dir/`node_modules` exclusion); the `sync.go` mtime skip and `deleteMissing` pruning; the 250 ms
  debounce; `code_hybrid_search` excludes Markdown by default.
- **Wrong recommendation (should be reversed):** "Do not use `kb_add_document` per item instead." The
  chattiness argument is sound but the conclusion is not — see the push/pull table in A3–A7. Rewrite the
  Notes to the hybrid shape: export to disk, `KBAutoImport=true`, **`KBWatch=false`**, and drive re-sync
  from the companion.
- **Missing acceptance criterion:** the hub drops a slow subscriber permanently
  (`internal/server/hub.go:113-141` — `deliver` marks overflow, `markOverflow` closes `overflowed`). An
  exporter fed by the hub that overflows silently stops exporting. Add: *"a hub overflow triggers a full
  re-export rather than a silent desync."* Better still, drive the exporter from the same publishers
  `internal/server/events.go:293, :359` call, not from an SSE-shaped `hubClient`.
- **Stale line refs (cosmetic):** `hub.go:141` → the subscribe API is `:79-104` / `:212`;
  `events.go:256, :282` → the publishers are `:293` and `:359`; the payload structs are `:178` and
  `:188`. `IsKb` is real (`events.go:195`).

**Recommendation: keep, rewrite the Notes and two acceptance criteria, and split off a
`GIT-US-00xx: metadata round-trip contract with Pando`** whose acceptance is "an exported item's
`status` and `type` come back in a `kb_search_documents` result" — because that is the criterion that
actually gates the rest of the epic, and today it fails.

### GIT-US-0077 — Companion MCP client · **(i) buildable today**

The most solid story in the epic.

- Transport compatibility verified in A11 — the go-sdk v1.4.1 streamable client works against Pando's
  hand-rolled `/mcp`, 405s on GET and DELETE included.
- `go.mod:12` confirmed: `github.com/modelcontextprotocol/go-sdk v1.4.1`, already a direct dependency,
  already used server-side (`internal/mcp/transport.go:41`).
- Tool contracts cited are accurate: `kb_search_documents` params and defaults confirmed at
  `remembrances_kb.go:227-259` (limit default 5, clamped to 20 at `:274-279`), result shape at
  `:296-316` — and it **does** include `metadata`, which makes the A4 fix pay off immediately with no
  client change. `code_hybrid_search` at `:300`, `code_list_projects` at `:1142`, `sanitizeProjectID` at
  `:1523-1543`.
- The advice to avoid `hybrid_search_remembrances` is correct: `internal/rag/hybrid.go:118-121` sorts raw
  scores from three sources with no normalisation, so boosted code scores (~1.0) bury KB RRF scores (~0.016).
- "Refuse any URL that is not loopback unless the operator opts in explicitly" — **correct, and
  understated.** A10 shows the surface is not merely unauthenticated but CORS-open with
  `SetGlobalAutoApprove(true)`. Strengthen the criterion and note that even loopback is reachable from
  the user's browser until Pando ships auth.
- Minor: add "reuses one session" to the perf rationale — Pando's session map has no eviction.

**Recommendation: keep as written**, strengthen the loopback criterion, add a one-line note that a future
`POST /api/v1/remembrances/kb/search` (A1) would let this client drop MCP for plain HTTP — the package
boundary is right either way.

### GIT-US-0082 — Pando search backend behind the core contract · **(ii) with a workaround**

The story most affected by A4/A5.

- The architecture is right and consistent with ADR-003 / `docs/02` §8: `Searcher` in `internal/core`,
  native implementation in `internal/server`, per-request selection with fallback.
- `core.SearchHit` already carries `Snippet` (`internal/core/query.go:602-610`) — one less thing to widen.
- **The ranking caveat is correct and important.** Never sort RRF scores against boosted code scores.
- **The performance note is correct and now quantified** (A13): fine at ~10 000 chunks (100–200 ms), not
  fine at ~35 000. Replace *"measure before promising anything at ten thousand"* with the measured
  numbers and a hard budget: fall back to core above a configured chunk count.
- **The workaround it needs:** resolving `file_path` back to item ids works today (path prefix), but
  *filtering by project* does not (A2) — with several repositories in one corpus a multi-project query
  under-returns. Workaround: over-fetch (`limit=20`, the hard cap) and filter by path prefix in the
  companion, accepting under-return. Clean fix: **PANDO: path-prefix filter in `SearchOptions` (S)**.
- **Stale line refs:** `internal/core/query.go:544` → `Search` is at **`:628`**; `SearchHit` `:518` →
  **`:602`**; `internal/server/server.go:411-435` → `handleCapabilities` is at **`:443-497`**, with
  `"search": "core"` at **`:459`**; `web/src/api/provider.ts:689` → `fullTextSearch: 'core' | 'bleve'` is
  at **`:1179`**; `provider.ts:940` → `search(query)` is at **`:1493`**; `companion-provider.ts:761` →
  the mapping is at **`:1022`** (`asString(features['search']) === 'bleve' ? 'bleve' : 'core'`).
  Everything referenced exists; only the numbers drifted. `GET /api/v1/search` confirmed at
  `internal/server/api.go:79`.

**Recommendation: keep**, refresh the line numbers, add the measured latency budget, and add an
acceptance criterion for the multi-project under-return (either "single project per corpus" as an
explicit constraint, or the prefix filter as a dependency).

### GIT-US-0086 — Semantic results in the search UI · **(i) buildable today**

Purely git-in-track-side; nothing depends on a Pando change beyond what GIT-US-0082 delivers.

- `web/src/features/workspace/WorkspaceSearch.tsx` exists; there is no `web/src/features/search/`
  directory, so the story is right to extend the workspace panel rather than invent one.
- `SearchHit.Snippet` already exists server-side, so widening it with an origin discriminator is small.
- The sanitisation requirement is right and non-negotiable: `chunk_content` is verbatim repository text.
- One addition: the chunk Pando returns is the *fused* chunk, which on the watcher path may still contain
  the raw YAML front-matter block (A5, consequence 3). Until that is fixed, strip a leading `---…---`
  block before rendering a snippet, or users will see YAML as their "why matched" text.

**Recommendation: keep as written**, add the front-matter-in-snippet note.

### GIT-US-0088 — `search_semantic` MCP tool + routing skill · **(i) buildable today**, given GIT-US-0082

- The layering (business logic in the vault dispatch table, `internal/mcp` holds only schemas and wire
  projection) matches `internal/mcp/tools.go` and the transport isolation in `internal/mcp/transport.go:12-19`.
- "Pando reaches these tools automatically once `[MCPServers.gintrack]` is configured" — consistent with
  what I read; not re-verified in this slice (AG-UI is another slice's scope).
- "With no Pando backend selected, return a typed error and never fall back silently" — good, and the
  right asymmetry against GIT-US-0082's *silent* fallback in the REST endpoint. Worth a sentence in the
  story explaining why the two differ (a human wants results; an agent needs to know which index answered).
- The doc-drift fix it bundles (`docs/07-cli-and-api.md:896-909` listing 12 tools where there are 13) is
  unrelated housekeeping. Harmless; just don't let it grow.

**Recommendation: keep as written.**

### GIT-US-0091 — Pando settings card · **(ii) today, (i) after the Pando reindex route**

- **Correct:** "Pando has no REST or MCP trigger for a KB reindex — the filesystem watcher is the only
  path" (A8). The workaround it picks — re-export and let the watcher notice — works, but it is exactly
  the path that loses metadata (A4/A5), so the card would advertise a reindex that quietly degrades the index.
- **This is the story that most wants A1.** `POST /api/v1/remembrances/kb/reindex` is ~20 LOC in Pando
  (`SyncDirectoryWithStats` + `FilesystemMirrorPath`, both exported) and turns "re-export and hope" into
  a real awaitable operation returning `SyncStats{scanned, added, updated, unchanged, deleted}`
  (`types.go:44-55`) — exactly the "index status" the card promises.
- **Correct and important:** the embedding-model pin warning (A12). Strengthen per A14: the model is
  global to the Pando instance, so it applies to every consumer of that instance, not just git-in-track.
- `code_index_status(job_id)` for code-side progress — confirmed (`remembrances_code.go:248`).
- `POST /api/v1/remembrances/projects/index` (`routes.go:146`) already exists over plain REST, so the
  code-index half of "reindex now" needs no MCP call — the card can use the existing route with
  `X-Pando-Token`. Worth saying in the story; simpler than going through `internal/pando.Client`.

**Recommendation: keep**, note the existing REST route for the code half, and make the KB half depend on
the proposed Pando reindex route (falling back to re-export-and-wait when absent).

## GIT-EP-0018 — Conversational agent interface (AG-UI)

AG-UI is another slice's subject; reviewed here only for assumptions touching search, the MCP transport
or the KB.

| Story | Verdict (search/MCP/KB aspects only) |
|---|---|
| **GIT-US-0049** Companion agent proxy | **(i)** as far as this slice reaches. Nothing touches the KB or :9777; the `insecure_tls` / `--no-tls` guidance is about `pando agui-serve`, a different listener. |
| **GIT-US-0053** AG-UI client + store | **(i)**. Out of this slice. |
| **GIT-US-0057** Chat route and message UI | **(i)**. Out of this slice. |
| **GIT-US-0061** HITL dialogs + state panel | **(i)**. Out of this slice. |
| **GIT-US-0064** Frontend tools registry | **(i)**. Its Note — "everything that reads or writes backlog content must go through the gintrack MCP server" — is the right boundary and consistent with GIT-US-0088. |
| **GIT-US-0069** `gintrack agent init` + docs | **(ii), with one factual correction.** |

**GIT-US-0069 corrections:**

1. It generates `[Remembrances]` with `KBPath`, `KBAutoImport` **and `KBWatch`**. Per A5 and A7,
   `KBWatch=true` over a mirrored corpus is what destroys the metadata. Until Pando fixes the watcher the
   generated template should set **`KBWatch = false`** and say why. A one-word change with a large
   behavioural consequence.
2. Its Note — "Pando's MCP HTTP transport has no authentication, so bind it to loopback" — is correct but
   incomplete: A10 shows the CORS policy is `*`, so loopback binding does **not** protect the user from a
   web page they visit. The generated doc and ADR-035 must say that plainly, and the ADR's negative
   consequences should list it.
3. `pando-schema.json` being stale and `internal/config/config.go` being the authority — consistent with
   what I read (`RemembrancesConfig` at `:460-531`, `MCPServerConfig` at `:594-620`).

**Recommendation: keep all six; amend GIT-US-0069's template and security note.**

## Cross-cutting verdicts

**Split:** GIT-US-0073 should shed a `metadata round-trip contract` story (above). It gates the whole epic
and is currently invisible inside a story about writing files.

**Merge:** none. The six GIT-EP-0019 stories are well separated along real seams.

**Drop:** none. Every story survives; two carry wrong assumptions that are cheap to correct.

**Sequencing change:** GIT-US-0091 sits last at `medium`. The reindex route and the embedding-model pin it
wants are what make GIT-US-0073's corpus *trustworthy*. Pull its backend half (settings + reindex
endpoints) forward next to GIT-US-0077 and leave only the card in place.

---

## Proposed backlog items

### Epic — PANDO-EP-A: Knowledge-base metadata fidelity

*The KB silently discards the structured metadata external hosts depend on. Front matter other than
`tags`/`aliases` is dropped, and the watcher erases even those on the first edit — 523 of 539 documents
in Pando's own KB have already lost their tags.*

**PANDO-US-A1 — Preserve unknown front-matter keys in document metadata** *(S)*

Parse YAML front matter into a map as well as the typed struct, and merge the unrecognised keys into the
document's `metadata` JSON so they are stored, filterable and returned by `kb_search_documents`.

- [ ] `ParseFrontMatter` returns the full key set alongside the typed `FrontMatter` (`internal/rag/kb/frontmatter.go:45`).
- [ ] `SyncDirectoryWithStats` merges unrecognised keys into `meta` (`sync.go:206-228`), never overwriting the reserved `source_*` keys.
- [ ] A document with `status: backlog` in its front matter returns `metadata.status == "backlog"` from `kb_search_documents`.
- [ ] Reserved keys (`tags`, `aliases`, `created_at`, `updated_at`, memory fields) keep their current typed handling.
- [ ] A document whose front matter fails to parse is still indexed, with a warning.

**PANDO-US-A2 — Parse front matter in the KB watcher** *(S)*

The watcher writes only `source_*` metadata and `UpdateDocument` replaces metadata wholesale, so every
edit erases the tags the initial sync stored — permanently, because the mtime is then recorded as current.

- [ ] `handleWatchEvent` (`internal/rag/kb/watcher.go:163-191`) parses front matter and builds the same metadata as `sync.go:206-228`.
- [ ] The body stored by the watcher is front-matter-stripped, matching the sync path.
- [ ] A regression test edits a tagged document through the watcher and asserts the tags survive.
- [ ] A migration or startup repair re-indexes documents whose stored metadata lacks tags their source file declares.

**PANDO-US-A3 — Suppress the mirror ⇄ watcher feedback loop** *(S)*

`kb_add_document` mirrors to `KBPath`; the watcher then re-indexes that write and overwrites the metadata
the tool just stored.

- [ ] `WriteDocumentToFilesystem` (`internal/rag/kb/filesystem.go:44`) records the path and mtime it just wrote.
- [ ] The watcher skips an event matching a recent self-write (bounded TTL, bounded map).
- [ ] `kb_add_document` with `metadata: {"status":"x"}` still reports `status: x` from `kb_get_document` five seconds later with `KBWatch=true`.

### Epic — PANDO-EP-B: A REST surface for search

*Search is reachable only over MCP or an agent run, which forces every host into an MCP client it may not
want. The service objects are already on `App` and the auth middleware already covers `/api/`.*

**PANDO-US-B1 — REST routes for KB and code search** *(S)*

`POST /api/v1/remembrances/kb/search` and `/code/search` over `Remembrances.KB` and `.Code`, inheriting
`X-Pando-Token` automatically.

- [ ] Both routes registered in `internal/api/routes.go` next to `:144-152`.
- [ ] Request and response shapes mirror the MCP tools, including `metadata`, `tags`, `score`, `rank`.
- [ ] `include_docs` / `min_score` behave as `rankAndFilterHybrid` does today (`internal/llm/tools/remembrances_code.go:417`).
- [ ] An unauthenticated request gets `401` without reaching a handler.
- [ ] `Remembrances == nil` answers an empty result, not a panic, matching `handlers_remembrances.go:32`.

**PANDO-US-B2 — REST document upsert, delete and KB reindex** *(S)*

- [ ] `POST /api/v1/remembrances/kb/documents` upserts (`AddDocument`/`UpdateDocument`), with `metadata` stored verbatim.
- [ ] `DELETE /api/v1/remembrances/kb/documents` removes the document and its mirror.
- [ ] `POST /api/v1/remembrances/kb/reindex` runs `SyncDirectoryWithStats` against `FilesystemMirrorPath()` and returns `SyncStats`.
- [ ] Reindex refuses a concurrent run with `409`.
- [ ] Secondary instances proxy writes to the primary, as `kb.go:190-226` already does.

### Epic — PANDO-EP-C: Secure the MCP HTTP transport

*Port 9777 accepts any caller and answers `Access-Control-Allow-Origin: *`, while `cmd/mcp_server.go:125`
auto-approves every tool call. Any web page the user visits can drive it.*

**PANDO-US-C1 — Bearer auth on the MCP HTTP transport** *(S)*

- [ ] `MCPServerConfig` gains `HttpToken` and `HttpAllowedOrigins` (`internal/config/config.go:594`).
- [ ] A middleware wrapping `internal/mesnada/server/server.go:140` enforces `Authorization: Bearer` with a constant-time compare.
- [ ] `corsMiddleware` (`:148`) echoes only allow-listed origins and emits no CORS headers when the list is empty.
- [ ] A generated token is printed to stderr at startup when none is configured.
- [ ] The MCP Go SDK client still completes initialize → tools/list → tools/call with the token, and the standalone-SSE 405 and close-DELETE paths still behave (regression test).

### Epic — PANDO-EP-D: Search scale and correctness

**PANDO-US-D1 — Stop selecting the document body in vector and FTS search** *(S)*

`searchVector` returns `d.content` once per chunk and no caller reads it — 85 % of the bytes on a measured
corpus (136 MB → ~20 MB for 6 861 chunks).

- [ ] `d.content` removed from `kb.go:840-848` and `:945-961` and from the `Scan` lists.
- [ ] `SearchResult.Document.Content` is documented as unpopulated by search, or backfilled only for the returned top-k.
- [ ] A benchmark records bytes scanned and allocation before and after.

**PANDO-US-D2 — Path-prefix filter pushed into SQL** *(S)*

- [ ] `kb.SearchOptions` gains `PathPrefix`.
- [ ] Both search legs add `AND d.file_path LIKE ? || '%'`.
- [ ] `kb_search_documents` and the REST route expose it.
- [ ] A prefixed query scans only the matching documents (asserted via a counter or an explain test).

**PANDO-US-D3 — Embedding dimension guard** *(M)*

- [ ] The embedding dimension (or model id) is recorded when a chunk is written.
- [ ] A startup check compares it with the configured embedder and logs loudly on mismatch.
- [ ] A status field reports the count of stale chunks, surfaced on an existing remembrances route.
- [ ] `kb_search_documents` warns in its response when it skipped chunks for dimension mismatch.

---

## Decisions for the user

1. **Which sync model for the corpus — and it is not the one GIT-US-0073 chose.**
   *Recommendation:* export to disk (as planned) but run `KBAutoImport=true`, **`KBWatch=false`**, and
   drive freshness from the companion's own debouncer. Today that means re-export plus waiting for the
   next full sync; with PANDO-US-B2 it becomes one authenticated HTTP call returning `SyncStats`. Turning
   the watcher off is a one-line config change that removes both metadata-destroying bugs (A5, A7)
   immediately, without waiting for Pando.

2. **Do you need structured filtering (by `status`, `type`, `project`) on semantic results?**
   If yes → PANDO-US-A1 is a hard dependency and the epic should not start without it.
   If no (semantic search returns candidates; the companion re-reads each hit from its own index for the
   authoritative fields) → the epic can proceed today with the plain `file_path` → item-id resolution
   GIT-US-0082 already specifies. *Recommendation:* the second. It is strictly more robust — git-in-track
   owns the truth, Pando only owns "which documents are relevant" — and it removes the whole
   metadata-fidelity risk from the critical path. Ship PANDO-US-A1 anyway, as an improvement, not a gate.

3. **One Pando per repository, or one shared instance?**
   *Recommendation:* one per repository, matching the existing `ipc.lock` model. A shared instance means
   one flat key space (A2), one global embedding model for everyone (A14), a scan cost that grows with the
   sum of all corpora (A13), and no way to filter a query to one project until PANDO-US-D2 lands. The
   operational cost of one Pando per repo is a port and a config file.

4. **Do you wait for MCP auth (PANDO-US-C1) before shipping GIT-US-0077?**
   *Recommendation:* no — build the client now (it works today, A11), but make the non-loopback refusal
   non-overridable in v1, and say in `docs/20-agent-interface.md` that loopback is not a security boundary
   against the user's own browser while the CORS policy is `*`. Revisit when C1 ships.

5. **MCP now, or REST later, for the search path?**
   *Recommendation:* MCP now. GIT-US-0077's package boundary (`internal/pando.Client` with
   `SearchKB`/`SearchCode`/`ListProjects`/`Health`) is transport-agnostic, so swapping in PANDO-US-B1's
   REST routes later is an internal change to one file. Do not block on the REST routes.

6. **What latency budget do you promise for semantic search?**
   *Recommendation:* 300 ms p95 for the Pando leg, with an automatic fall back to the core index above a
   configured chunk count. Measured: 6 861 chunks ⇒ 79 ms warm / 561 ms cold, 238 MB allocated per query.
   That gives roughly 25 000–35 000 chunks of headroom, which a single 5 000-item backlog plus its KB will
   not approach — but two more repositories in the same instance would (decision 3).
