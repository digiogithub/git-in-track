---
title: Research — YouTrack REST API reference (from youtrack-cli)
type: page
tags: [research, youtrack, api]
---

# YouTrack integration reference, extracted from `/www/youtrack-cli`

Everything marked **[CLI]** is from the code (file + line). Everything marked **[API-KNOWLEDGE]** is **not** in the CLI and comes from YouTrack REST API knowledge / the JetBrains devportal — verify before relying on it.

| File | Lines | Role |
| --- | --- | --- |
| `/www/youtrack-cli/src/youtrack.js` | 393 | Entire HTTP client + field selectors. **The file that matters.** |
| `/www/youtrack-cli/src/config.js` | 65 | Token/URL storage, base-URL normalization |
| `/www/youtrack-cli/src/cli.js` | 388 | Command routing, arg parsing, export/apply |
| `/www/youtrack-cli/src/markdown.js` | 274 | Issue/Article ↔ Markdown+YAML, custom-field payload shaping |
| `/www/youtrack-cli/src/project-export.js` | 612 | Bulk-export orchestrator (paging, job graph, resume) |
| `/www/youtrack-cli/src/queue.js` | 529 | File-backed job queue + coordinator lock |

No runtime dependencies.

---

## 1. Authentication

### Token obtained / stored **[CLI]**
- Never obtained programmatically. User pastes a **permanent token** from the YouTrack UI. `README.md:8`, `README.md:59` (`youtrack-cli setup --url ... --token perm:...`). No OAuth, no Hub flow, no refresh.
- Stored in YAML with tight perms — `src/config.js:43-51`: `mkdir(..., { mode: 0o700 })`, `writeFile(..., { mode: 0o600 })`. Two keys only: `url`, `token`.
- Path — `src/config.js:8-18`: `$YOUTRACK_CONFIG` wins; Windows `%APPDATA%\youtrack-cli\config.yaml`; else `$XDG_CONFIG_HOME/youtrack-cli/config.yaml` → `~/.config/...`.
- Env `YOUTRACK_URL` / `YOUTRACK_TOKEN` **override** the file (`src/config.js:20-41`).
- `requireConfig()` (`src/config.js:62-65`) fails fast when either is missing.
- `yt config` prints `{ path, url, hasToken }` — never echoes the token (`src/cli.js:60-63`).

### Header format **[CLI]** — `src/youtrack.js:326-340`
```js
headers: {
  Accept: "application/json",
  Authorization: `Bearer ${this.token}`,
  ...(body ? { "Content-Type": "application/json" } : {}),
}
```
Attachment downloads: same Bearer, plus `redirect: "follow"` (`src/youtrack.js:294-298`). The `perm:` prefix is part of the token string, not added by the client. Test pins the literal header: `test/youtrack.test.js:157` → `"Bearer tok"`.

### Base URL **[CLI]**
- `normalizeBaseUrl()` (`src/config.js:53-60`) strips trailing slashes from the pathname and **drops `search` and `hash`**.
- Constructor also strips trailing slash: `this.url = url.replace(/\/$/, "")` (`src/youtrack.js:109`).
- Requests: `new URL(\`${this.url}${path}\`)`, `path` always starts `/api/...` (`src/youtrack.js:327`).
- **Self-hosted context paths work**: `test/youtrack.test.js:220-223` proves `attachmentUrl("https://yt.example.com/youtrack", { url: "/api/files/1-1?sign=abc" })` → `"https://yt.example.com/youtrack/api/files/1-1?sign=abc"`.
  - **Go caveat**: this works because it's plain string concat. Use `strings.TrimRight(base,"/") + "/api/issues"`, **not** `url.ResolveReference()`, which would drop `/youtrack`.
- Cloud vs self-hosted is **not distinguished anywhere**. Same code path.

### Token validation **[CLI: ABSENT]**
No `GET /api/users/me` anywhere. `yt setup` writes config without contacting the server (`src/cli.js:54-58`). Validation is implicit — first call throws `YouTrack API 401 Unauthorized: <body>` (`src/youtrack.js:342-345`).

**[API-KNOWLEDGE]** Add an explicit probe:
```
GET {base}/api/users/me?fields=id,login,fullName,email,avatarUrl
```
```json
{ "id": "1-1", "login": "jose", "fullName": "Jose F. Rives", "email": "jose@digio.es", "$type": "Me" }
```
401 = bad/expired token; 403 = missing scope; **404 on `/api/...` usually means the base URL is missing its context path** (forgot `/youtrack`). Distinguishing these gives a good "Connect YouTrack" error message.

### Error contract **[CLI]** — `src/youtrack.js:342-348`
```js
if (!response.ok) { const text = await response.text();
  throw new Error(`YouTrack API ${response.status} ${response.statusText}: ${text}`); }
if (response.status === 204) return null;
return response.json();
```

---

## 2. Projects

### What the CLI does **[CLI]** — one endpoint, get-by-key only
`src/youtrack.js:216-220`
```js
async getProject(key) {
  return this.request(`/api/admin/projects/${encodeURIComponent(key)}`, {
    query: { fields: "id,name,shortName,description" },
  });
}
```
Confirmed `test/youtrack.test.js:177` (`/api/admin/projects/PRJ`). Uses the **admin** resource; `{key}` is the *shortName* (both shortName and internal `0-1` id work as the path segment).

**`getProject()` is dead code** — nothing in `cli.js` or `project-export.js` calls it; the export takes the key from argv and puts it in a query string.

### Not covered **[CLI: ABSENT]**
No project list, no search/autosuggest, no `$skip`/`$top` over projects.

### Supplement **[API-KNOWLEDGE]**
```
GET {base}/api/admin/projects?fields=id,shortName,name,description,archived,leader(login,fullName)&$top=100&$skip=0
```
Returns a bare JSON **array** — all YouTrack list endpoints do, never an envelope (matches `asArray()` at `src/project-export.js:534`).
```json
[ { "id": "0-1", "shortName": "ACME", "name": "Acme Platform", "archived": false, "$type": "Project" },
  { "id": "0-4", "shortName": "KB",   "name": "Handbook",      "archived": false, "$type": "Project" } ]
```
Autosuggest: `/api/admin/projects` accepts `query=acme` (substring over name + shortName). **Recommendation: fetch once at connect time and filter client-side in React** — projects number in the tens, and `/api/admin/projects` needs Read-Project permission per project; paging it for typeahead is waste.

Related, and needed by git-in-track:
- `GET /api/admin/projects/{id}/customFieldSettings?fields=field(name,fieldType(id)),bundle(id),canBeEmpty,emptyFieldText` — **which custom fields exist** (Type, State, Priority, Estimation, Sprint…) before you try to write them. The CLI never calls this and blindly trusts frontmatter field names.
- `GET /api/admin/projects/{id}/customFieldSettings/{fieldId}/bundle/values?fields=id,name,description` — allowed enum values for dropdowns.
- `GET /api/admin/projects/{id}/articles` — project-scoped articles.

---

## 3. Issues

### 3.1 Field selectors **[CLI]**

`src/youtrack.js:7-19` — `ISSUE_FIELDS` (used by `getIssue`, `createIssue`, `updateIssue`):
```
id,idReadable,summary,description,created,updated,resolved,
project(id,name,shortName),
reporter(id,login,fullName,email),
customFields(id,name,value(id,name,login,fullName,presentation,localizedName,idReadable)),
links(direction,linkType(id,name,sourceToTarget,targetToSource),issues(id,idReadable,summary))
```
`src/youtrack.js:38-42` — `ISSUE_EXPORT_FIELDS` = above + `attachments(id,name,size,mimeType,extension,charset,created,url,author(login),comment(id))` + `tags(id,name)`. Asserted at `test/youtrack.test.js:59-63` but **never actually used by a request** — `issue:fetch` calls `client.getIssue(id)` with the narrower `ISSUE_FIELDS` (`src/project-export.js:129`).

`src/youtrack.js:21` `COMMENT_FIELDS` = `id,text,created,updated,author(id,login,fullName,email)`
`src/youtrack.js:35` `ATTACHMENT_FIELDS` = `id,name,size,mimeType,extension,charset,created,url,author(login),comment(id)`
`src/youtrack.js:162` list default = `id,idReadable,summary,project(shortName),updated,resolved`
`src/youtrack.js:231` project-paging default = `id,idReadable,summary,updated,resolved,project(shortName)`

**Gap:** the `customFields(...)` selector requests no `$type` on the field or value. Fine for Markdown rendering, insufficient for a typed UI editor. See §3.7.

### 3.2 Search **[CLI]** — `src/youtrack.js:162-164`
```js
async listIssues({ query = "", limit = 20, fields = "id,idReadable,summary,project(shortName),updated,resolved" } = {}) {
  return this.request("/api/issues", { query: { query, $top: limit, fields } });
}
```
→ `GET /api/issues?query=<q>&$top=<limit>&fields=<sel>`. Default `$top=20` (`src/cli.js:67`, `:97`).
`request()` **omits empty-string params** (`src/youtrack.js:329`: `if (value !== undefined && value !== "")`), so an empty query isn't sent at all.

Paged variant, `src/youtrack.js:226-241`: `{ query: projectQuery(project, query), $skip, $top, fields }`.

### 3.3 Query-language patterns **[CLI]** — `src/youtrack.js:378-385`
```js
export function projectQuery(project, extra = "") {
  const parts = [`project: {${project}}`];
  const trimmed = String(extra || "").trim();
  if (trimmed) parts.push(trimmed);
  parts.push("order by: created asc");
  return parts.join(" ");
}
```
- **Braces around the project key**: `project: {ACME}`. Braces are YouTrack's quoting for values with spaces; using them unconditionally is the safe choice — copy it.
- **`order by: created asc` appended unconditionally.** The comment at `src/youtrack.js:222-225` says why: *"`order by: created asc` is appended so that `$skip`/`$top` paging stays stable while the export runs."* **This is the single most important operational lesson in the repo — without a stable sort, `$skip` paging over a live instance silently skips and duplicates issues.**
- Date filter — `src/project-export.js:551-553`: `` `updated: ${since} .. Today` ``. Range syntax `field: A .. B`; `Today` is a literal keyword.
- Articles query — `src/youtrack.js:277`: `project: {${project}}`, **no ordering clause** (latent paging bug; don't copy).
- README user-facing examples: `project: ABC #Unresolved` (`README.md:85`), `project: ABC` (`README.md:90`).
- Tests pin composition (`test/youtrack.test.js:161-163`): `projectQuery("PRJ")` → `project: {PRJ} order by: created asc`; `projectQuery("PRJ","  #Unresolved ")` → `project: {PRJ} #Unresolved order by: created asc`.

**[API-KNOWLEDGE]** patterns git-in-track will need:

| Intent | Query |
| --- | --- |
| Epics of a project | `project: {ACME} Type: Epic` |
| Open items | `project: {ACME} #Unresolved` (alias for `State: -Resolved`) |
| Subtasks of an issue | `subtask of: ACME-42` |
| Parent of an issue | `parent for: ACME-99` |
| By assignee | `Assignee: jose`, `for: me` |
| Unassigned | `Assignee: Unassigned` |
| In a sprint | `Board ACME: {Sprint 12}` or `Sprint: {Sprint 12}` |
| By fix version | `Fix versions: {1.4.0}` |
| Updated window | `updated: 2026-01-01 .. Today` |
| Text search | bare words: `project: {ACME} login error` |
| Sort | `order by: updated desc`, `order by: {Priority} desc` |
| Negation | `-Type: Bug`, `Type: -Bug` |

Values with spaces **must** be wrapped: `{In Progress}`.

### 3.4 Get one issue **[CLI]** — `src/youtrack.js:166-170`
```js
async getIssue(id, { comments = false } = {}) {
  const issue = await this.request(`/api/issues/${encodeURIComponent(id)}`, { query: { fields: ISSUE_FIELDS } });
  if (comments) issue.comments = await this.getIssueComments(id);
  return issue;
}
```
`{id}` accepts internal id (`2-1`) or readable id (`ACME-42`); the CLI passes readable throughout (`src/cli.js:72-76`). Comments are a **second request**, not a `fields` expansion — git-in-track can collapse this into one call by adding `comments(id,text,created,author(login,fullName))` to the selector.

### 3.5 Links, subtasks, parents **[CLI]**

**The CLI never calls `GET /api/issues/{id}/links`.** Links come from the issue payload via `links(direction,linkType(id,name,sourceToTarget,targetToSource),issues(id,idReadable,summary))` (`src/youtrack.js:18`).

Normalization, `src/markdown.js:125-138`:
```js
const normalized = links
  .map((link) => compact({ type: link.linkType?.name, direction: link.direction,
      issues: link.issues?.map((issue) => compact({ id, idReadable, summary })) }))
  .filter((link) => link.issues?.length);
```
Note `.filter((link) => link.issues?.length)` — **YouTrack returns one entry per (linkType, direction) pair including the empty ones**. An issue with a single "relates to" link comes back with ~20 entries, 19 of them `"issues": []`. Real shape gotcha.

Dependency traversal (`yt issues export --dependencies`), `src/cli.js:255-270`: flattens `issue.links[].issues[]`, dedupes into a `Map` keyed by `idReadable`, drops self-refs, then **one `getIssue` per linked issue, sequentially, one level deep**. No recursion; a "Subtask" link and a "Relates" link are treated identically.

**[API-KNOWLEDGE]** For a proper parent/subtask tree:
```
GET /api/issues/{id}/links?fields=id,direction,linkType(id,name,sourceToTarget,targetToSource,directed,aggregation),issues(id,idReadable,summary,customFields(name,value(name)))
```
```json
[
  { "id": "97-1s", "direction": "OUTWARD",
    "linkType": { "id": "97-1", "name": "Subtask", "sourceToTarget": "parent for",
                  "targetToSource": "subtask of", "directed": true, "aggregation": true },
    "issues": [ { "id": "2-8", "idReadable": "ACME-8", "summary": "Child" } ] },
  { "id": "97-1s", "direction": "INWARD",
    "linkType": { "name": "Subtask", "sourceToTarget": "parent for", "targetToSource": "subtask of" },
    "issues": [ { "idReadable": "ACME-1", "summary": "Parent epic" } ] },
  { "direction": "BOTH", "linkType": { "name": "Relates", "sourceToTarget": "relates to" }, "issues": [] }
]
```
**Reading rule:** linkType `Subtask` + `direction: "OUTWARD"` → those issues are **children**; `"INWARD"` → that issue is the **parent**. `"BOTH"` is used for undirected types (Relates, Duplicate). `aggregation: true` marks the hierarchy-forming type.

Create a link: `POST /api/issues/{parentId}/links/{linkTypeId}{s|t}/issues` body `{ "id": "<childInternalId>" }` (`s` = source/outward, `t` = target/inward, e.g. `97-1s`). Enumerate types: `GET /api/issueLinkTypes?fields=id,name,sourceToTarget,targetToSource,directed,aggregation`. Delete: `DELETE /api/issues/{id}/links/{typeId}{dir}/issues/{otherId}`.

Simpler query-based alternative (no id arithmetic, one call per parent): `GET /api/issues?query=subtask of: ACME-42&fields=idReadable,summary,customFields(name,value(name))`.

### 3.6 Comments **[CLI]**
Read — `src/youtrack.js:172-174`:
```js
return this.request(`/api/issues/${encodeURIComponent(id)}/comments`, { query: { fields: COMMENT_FIELDS } });
```
→ `GET /api/issues/{id}/comments?fields=id,text,created,updated,author(id,login,fullName,email)`. **No `$top`/`$skip` sent** — the CLI assumes the default page covers all comments, which is wrong past the server default. git-in-track must page explicitly.

**Creating a comment is NOT implemented.** `markdownToIssuePayload` (`src/markdown.js:58-70`) builds only `{ summary, description, project, customFields }` and drops the `comments` frontmatter key.

**[API-KNOWLEDGE]**
```
POST /api/issues/{id}/comments?fields=id,text,created,author(login,fullName)
{ "text": "Comment body in **Markdown**" }
```
Optional `"usesMarkdown": true` (default on modern YouTrack), `"visibility": {"$type":"UnlimitedVisibility"}` or `{"$type":"LimitedVisibility","permittedGroups":[{"id":"..."}]}`. Edit: `POST /api/issues/{id}/comments/{commentId}` `{ "text": "..." }`. Delete: `DELETE .../comments/{cid}` or POST `{"deleted": true}`.

### 3.7 Custom fields — read and write **[CLI]**

**Reading.** `customFieldMap()` (`src/markdown.js:91-98`) flattens to `{ name: renderedValue }`. The crux is `renderFieldValue()` (`src/markdown.js:114-118`):
```js
if (Array.isArray(value)) return value.map(renderFieldValue);
if (value && typeof value === "object")
  return value.name || value.login || value.fullName || value.localizedName
      || value.presentation || value.idReadable || value.id || value;
return value;
```
Reduction order: `name` (enums/states/versions) → `login`/`fullName` (users) → `localizedName` → `presentation` (periods `2d 4h`, dates) → `idReadable` → `id`. Scalars pass through; arrays map (multi-value). Assignee is additionally hoisted to a top-level key via `firstFieldValue(issue, "Assignee")` (`src/markdown.js:27`, `:120-123`) — a plain `.find(e => e.name === "Assignee")`.

**Lossy**: discards `$type` and value `id`. Fine for a Markdown file, bad for a typed UI. Keep the raw objects and add `$type`.

**Writing.** `src/markdown.js:100-112`:
```js
function customFieldsPayload(customFields = {}) {
  return Object.entries(customFields).map(([name, value]) => ({ name, value: fieldValuePayload(value) }));
}
function fieldValuePayload(value) {
  if (Array.isArray(value)) return value.map(fieldValuePayload);
  if (value && typeof value === "object") return value;   // pass through
  if (typeof value === "string") return { name: value };   // string -> { name }
  return value;
}
```
`customFields: { Priority: Major }` → `"customFields": [ { "name": "Priority", "value": { "name": "Major" } } ]`.

**Critical caveat: no `$type` is sent.** Name-only addressing works for many single-value enum/state fields on modern YouTrack but is fragile for multi-value, period and version fields. **[API-KNOWLEDGE]** robust form:
```json
POST /api/issues/ACME-42?fields=idReadable,summary,customFields(name,value(name))
{ "customFields": [
  { "name": "Type",       "$type": "SingleEnumIssueCustomField",   "value": { "name": "Epic" } },
  { "name": "State",      "$type": "StateIssueCustomField",        "value": { "name": "In Progress" } },
  { "name": "Priority",   "$type": "SingleEnumIssueCustomField",   "value": { "name": "Critical" } },
  { "name": "Assignee",   "$type": "SingleUserIssueCustomField",   "value": { "login": "jose" } },
  { "name": "Estimation", "$type": "PeriodIssueCustomField",       "value": { "presentation": "3d" } },
  { "name": "Sprints",    "$type": "MultiVersionIssueCustomField", "value": [ { "name": "Sprint 12" } ] },
  { "name": "Due Date",   "$type": "DateIssueCustomField",         "value": 1767225600000 }
] }
```

**[API-KNOWLEDGE]** `$type` by field kind (verify via `customFieldSettings`):

| Field kind | `$type` | Value shape |
| --- | --- | --- |
| Single enum (Type, Priority) | `SingleEnumIssueCustomField` | `{ "name": "Bug" }` or `{ "id": "..." }` |
| Multi enum | `MultiEnumIssueCustomField` | `[ {"name":"A"}, {"name":"B"} ]` |
| State | `StateIssueCustomField` | `{ "name": "Open" }` (value also has `isResolved`) |
| Single user (Assignee) | `SingleUserIssueCustomField` | `{ "login": "jose" }` / `{ "id": "1-1" }` |
| Multi user | `MultiUserIssueCustomField` | array |
| Version / Fix versions | `SingleVersionIssueCustomField` / `MultiVersionIssueCustomField` | `{ "name": "1.4.0" }` |
| Owned | `SingleOwnedIssueCustomField` | `{ "name": "..." }` |
| Period (Estimation, Spent time) | `PeriodIssueCustomField` | `{ "presentation": "2d 4h" }` or `{ "minutes": 1200 }` |
| Date | `DateIssueCustomField` | unix ms integer |
| Simple text/int/float | `SimpleIssueCustomField` | raw scalar |
| Group | `SingleGroupIssueCustomField` | `{ "name": "..." }` |
| Build | `SingleBuildIssueCustomField` | `{ "name": "..." }` |

Clear a field: `"value": null`. Read selector to use instead of the CLI's: `customFields(id,name,$type,value(id,name,$type,login,fullName,presentation,minutes,isResolved,color(id)))`.

### 3.8 Create / update **[CLI]** — `src/youtrack.js:176-190`
```js
async createIssue(payload) {
  return this.request("/api/issues", { method: "POST", query: { fields: ISSUE_FIELDS }, body: payload });
}
async updateIssue(id, payload) {
  return this.request(`/api/issues/${encodeURIComponent(id)}`, { method: "POST", query: { fields: ISSUE_FIELDS }, body: payload });
}
```
**Both are `POST`** — YouTrack has no `PUT` for issues. Update is a **partial merge**: only keys present in the body are touched.

Payload — `src/markdown.js:58-70`: `compact({ summary, description, project: meta.project })` plus `customFields` if non-empty. `compact()` (`src/markdown.js:272-274`) strips `undefined`/`null`. `summary` falls back to the first `# ` heading (`headingFromBody`, `:82-85`); `description` is the body minus that heading (`stripFirstHeading`, `:87-89`).

Create-vs-update, `src/cli.js:273-279`:
```js
const id = options.id || meta.idReadable || meta.id;
const issue = id ? await client.updateIssue(id, payload) : await client.createIssue(payload);
```
For create, `project` must be present — `README.md:208`: *"To create new issues from Markdown, include `project.shortName` in the frontmatter."* Body becomes `"project": { "shortName": "ACME" }`.

**[API-KNOWLEDGE] caveat:** `POST /api/issues` officially wants `"project": { "id": "0-1" }`. `{ "shortName": "ACME" }` works on current versions but isn't the documented contract — resolve shortName → id once via `GET /api/admin/projects/{shortName}?fields=id` (already implemented at `src/youtrack.js:216-220`) and send the id.

Minimal create:
```json
POST /api/issues?fields=id,idReadable,summary
{ "project": { "id": "0-1" }, "summary": "Implement YouTrack sync", "description": "Markdown body",
  "customFields": [ { "name": "Type", "$type": "SingleEnumIssueCustomField", "value": { "name": "Task" } } ] }
```
→ `{ "id": "2-345", "idReadable": "ACME-42", "summary": "...", "$type": "Issue" }`

### 3.9 Attachments **[CLI]**
List: `GET /api/issues/{id}/attachments?fields=<ATTACHMENT_FIELDS>` (`src/youtrack.js:243-247`); articles at `:268-272`.

Download resolution — `src/youtrack.js:387-393`:
```js
export function attachmentUrl(baseUrl, attachment) {
  const raw = typeof attachment === "string" ? attachment : attachment?.url;
  if (!raw) throw new Error("Attachment has no url");
  if (/^https?:\/\//i.test(raw)) return raw;
  return `${baseUrl}${raw.startsWith("/") ? "" : "/"}${raw}`;
}
```
Doc comment `src/youtrack.js:285-290`: *"YouTrack returns a signed RELATIVE url, so it is appended to the instance base url."* In practice `attachment.url` = `/api/files/1-1?sign=<token>&updated=<ts>`. **The signature is in the URL and the Bearer header is still sent** (`:297`). `redirect: "follow"` required. Absolute URLs pass through.

Worth copying (`src/youtrack.js:305-323`): stream to `<dest>.<pid>.<base36 ts>.part`, `stat`, then `rename` into place — *"so a partial download never looks complete"*; `rm` the temp on error. Plus skip-if-correct-size at `src/project-export.js:204-210`.

**[API-KNOWLEDGE]** Upload is **not** in the CLI: `POST /api/issues/{id}/attachments?fields=id,name,url` with `Content-Type: multipart/form-data` (do **not** set JSON content-type).

### 3.10 Activity history **[CLI]** — `src/youtrack.js:249-266`, cursor-paged
```
GET /api/issues/{id}/activitiesPage
  ?categories=<ISSUE_ACTIVITY_CATEGORIES>
  &fields=activities(<ACTIVITY_FIELDS>),afterCursor,hasAfter
  &$top=100&reverse=false[&cursor=<afterCursor>]
```
`ACTIVITY_FIELDS` (`src/youtrack.js:81-91`): `id,timestamp,$type,category(id),author(id,login,fullName),field(id,name,presentation),added(id,name,text,login,fullName,presentation,idReadable,summary),removed(...),target(id,idReadable,text,summary)`.

25 category ids at `src/youtrack.js:45-79`; the two `Article*` ones are **rejected by the issue endpoint**, hence the split into `ISSUE_ACTIVITY_CATEGORIES` (23 ids) at `:78-79` (asserted `test/youtrack.test.js:65-72`).

Loop — `src/project-export.js:166-188`: follow `afterCursor` while `hasAfter`, with an `afterCursor !== cursor` infinite-loop guard. This is the mechanism for an audit/activity feed; `added`/`removed` give before/after per field change.

---

## 4. Knowledge base articles

### Covered **[CLI]** — articles are covered well

`src/youtrack.js:22-33`, `ARTICLE_FIELDS`:
```
id,idReadable,summary,content,ordinal,created,updated,
project(id,name,shortName),parentArticle(id,idReadable,summary),reporter(id,login,fullName,email)
```
Endpoints, `src/youtrack.js:192-214`:
```
listArticles   → GET  /api/articles?query=&$top=&fields=
getArticle     → GET  /api/articles/{id}?fields=ARTICLE_FIELDS
createArticle  → POST /api/articles?fields=ARTICLE_FIELDS
updateArticle  → POST /api/articles/{id}?fields=ARTICLE_FIELDS
```
Plus `src/youtrack.js:274-283`: `GET /api/articles?query=project: {PRJ}&$skip=&$top=&fields=id,idReadable,summary,updated,project(shortName)`; and `:268-272` attachments.

Payload — `src/markdown.js:72-80`:
```js
return compact({ summary: meta.summary || headingFromBody(body),
                 content: stripFirstHeading(body).trim(),
                 project: meta.project, parentArticle: meta.parentArticle, ordinal: meta.ordinal });
```
**`content` is the article's Markdown body** — the field is literally `content`, unlike issues where it's `description`. `parentArticle` nests; `project` scopes.

Same create-vs-update dispatch (`src/cli.js:281-287`). Web URLs: articles `${base}/articles/${idReadable}` (`src/youtrack.js:356-358`) vs issues `${base}/issue/${idReadable}` (`:352-354`).

Article ids: README uses `12-345` (`README.md:91`), tests use `PRJ-A-1` (`test/youtrack.test.js:179`). **Article `idReadable` is `<PROJECT>-A-<n>`** — the `-A-` infix distinguishes it from an issue id.

### Markdown quirks
- **[CLI]** Content is passed through untransformed (`src/markdown.js:55`). Export prepends `# ${summary}` as H1; import strips the first heading back off (`stripFirstHeading`, `:87-89`). **The summary is duplicated as an H1 in the body and must be stripped round-tripping** — git-in-track should decide explicitly whether the title lives in `summary` or in the body, not both.
- **[CLI]** Frontmatter uses a **hand-rolled YAML writer** (`stringifyYaml`, `src/markdown.js:190-238`) with an aggressive quoting regex at `:236`: ``/[:#\-[\]{},&*!|>'"%@`]|^\s|\s$|\n/`` → JSON-quote. Don't reuse; use `gopkg.in/yaml.v3`.
- **[API-KNOWLEDGE]** YouTrack Markdown is CommonMark-ish with extensions:
  - Issue ids like `ACME-42` in text are **auto-linked** — don't render them as links yourself.
  - `{color:red}text{color}` and `{width=300px}` are YouTrack-specific.
  - Image embeds reference attachments **by filename**: `![alt](file.png)` resolves against the entity's attachments, not a URL. Mirroring content into git-in-track requires rewriting these to your own asset URLs (the CLI does not — it just dumps to `assets/`).
  - `usesMarkdown` boolean controls the legacy wiki-markup mode on older instances.
  - `- [ ] item` checkbox lists supported.

### Not covered **[CLI: ABSENT]**
- **`childArticles`** — the CLI reads `parentArticle` but never `childArticles`, so it walks the KB tree **up but not down**. **[API-KNOWLEDGE]** `GET /api/articles/{id}/childArticles?fields=id,idReadable,summary,ordinal,hasChildren`, or add `childArticles(...)` and `hasChildren` to the `GET /api/articles/{id}` selector.
- Article **comments**: `GET|POST /api/articles/{id}/comments?fields=id,text,created,author(login,fullName)`, body `{ "text": "..." }`.
- Article **tags**: `GET /api/articles/{id}/tags`.
- **Linking an article to a project**: `project` is documented `Read-only`, so it is settable **only at creation time** in the POST body — `{"project": {"id": "0-1"}}`. You cannot move an article between projects via that field; re-parenting uses `parentArticle`.
- `visibility`, `updatedBy`, `hasStar`, `pinnedComments` — exist on the entity, none requested.

Verified against `https://www.jetbrains.com/help/youtrack/devportal/api-entity-Article.html` (fetched 2026-09-13): attributes are `id, attachments, childArticles, comments, content, created, externalArticle, hasChildren, hasStar, idReadable, ordinal, parentArticle, pinnedComments, project, reporter, summary, tags, updated, updatedBy, visibility`. Read-only: `id, created, idReadable, ordinal, project, updated, updatedBy, hasChildren, externalArticle, pinnedComments`. Sub-resources: `/attachments`, `/childArticles`, `/parentArticle`, `/tags`, `/comments`, plus `/api/admin/projects/{projectID}/articles`.

**Note:** `ordinal` is documented read-only yet `markdownToArticlePayload` sends it (`src/markdown.js:78`) — silently ignored by the server.

---

## 5. Rate limiting, retries, batching, caching, `$top`

### Token bucket **[CLI]** — `src/youtrack.js:117-125`
```js
async #throttle() {
  if (!this.rate) return;
  const interval = 1000 / this.rate;
  const now = Date.now();
  const slot = Math.max(now, this.#nextSlot);
  this.#nextSlot = slot + interval;
  if (slot > now) await sleep(slot - now);
}
```
A single monotonically-advancing `#nextSlot`; each caller reserves the next slot and sleeps. **Default 5 req/s** (`src/youtrack.js:108`), exposed as `--rate` default 5 (`src/cli.js:150`, `README.md:173`). `rate: 0` disables (`:111`, `:118`). Tested `test/youtrack.test.js:133-143`.

**Global client-side limiter shared by all concurrent workers** — exactly the pattern for Go (`rate.NewLimiter(5, 1)`).

### Retry **[CLI]**
`src/youtrack.js:93`: `const RETRYABLE_STATUS = new Set([429, 500, 502, 503, 504]);`
`src/youtrack.js:127-160`:
- `maxRetries` default **4** (5 attempts total) — `:108`.
- **Network errors (thrown fetch) are retried too** (`:136-142`).
- On a retryable status: read `Retry-After`, **drain the body** (`:150`, *"Drain the body so the connection can be reused before retrying"*), sleep `retryAfter ?? backoff(attempt)`.
- Backoff (`:157-160`): `retryBaseMs * 2**attempt + random(0, retryBaseMs)`; `retryBaseMs` default 500 ms → 500ms, 1s, 2s, 4s + jitter.
- `retryAfterMs` (`:369-376`) handles **both seconds and HTTP-date**, clamps negatives to 0 (tested `test/youtrack.test.js:74-81`).
- Non-429 4xx fails on the first attempt (`test/youtrack.test.js:125-131`).

**[API-KNOWLEDGE]** YouTrack Cloud does throttle server-side and answers `429` with `Retry-After`. No documented fixed quota; 5 req/s is a sound conservative default — copy it.

### Job-level retry **[CLI]**
Second layer in the export queue: `DEFAULT_MAX_ATTEMPTS = 5`, `DEFAULT_BACKOFF_MS = 1000` (`src/project-export.js:21-22`), applied in `runJob` (`:483-497`) — a handler throw returns the job to the queue via `queue.fail(job, error, context.retry)` instead of aborting the run. Still-failed jobs are listed and the process exits 1 (`src/cli.js:183-187`).

### Batching **[CLI: mostly ABSENT]**
- No request batching, no multiplexed endpoint.
- Only **job** batching: `queue.enqueueMany(jobs)` (`src/project-export.js:111`, `:234`).
- Concurrency = `--concurrency`, default **4** in-process workers over one shared queue (`src/project-export.js:18`, `src/cli.js:148`, `runWorkers` `:446-481`). Doc comment `:9-16`: one process owns the queue and does every filesystem write, which keeps writes serialized and the export resumable.
- Effective outbound rate ceiling is the token bucket, not the worker count.

### Caching **[CLI]**
**No HTTP caching, no ETag/If-None-Match, no memoization.** Only filesystem idempotence:
- Attachment skip when a file of expected size exists (`src/project-export.js:204-210`).
- Job-key dedupe scoped per run: keys are `r{run}:{key}` (`scopeQueue`, `:409-427`), so a re-run re-fetches but an interrupted run resumes without repeating finished work (`resolveRun`, `:386-392`).
- `--since` (`updated: <date> .. Today`) is the incremental-sync mechanism, not a cache.

### `$top` / `$skip` **[CLI]**

| Context | Value | Source |
| --- | --- | --- |
| `issues list` / `kb list` default | `$top=20` | `youtrack.js:162`, `:192`; `cli.js:67`, `:97` |
| Project issue paging | `$top=100` | `project-export.js:19`; `youtrack.js:228` |
| Article paging | `$top=100` | `youtrack.js:274` |
| Activity paging | `$top=100` | `project-export.js:20` |
| Comments | **none sent** | `youtrack.js:172-174` — latent truncation bug |
| Attachments | **none sent** | `youtrack.js:243-247` |
| `listFailed` | 50 | `cli.js:199`, `project-export.js:351` |

Exhaustion detection, `src/project-export.js:113-122`:
```js
const seen = skip + page.length;
const exhausted = page.length < wanted || (limit && seen >= limit);
if (!exhausted) { /* enqueue next page at skip = seen */ }
```
A short page means the end. Articles use the weaker `page.length >= top` test (`:236`).

**[API-KNOWLEDGE]** YouTrack's server-side default page size is 42 when `$top` is omitted; practical max for `$top` is ~1000 (larger is accepted but slow and may time out with wide `fields`). Keep `$top` at 100–200 with a narrow selector. **Always pair `$skip` with an explicit `order by:`** — the repo's most important lesson (`src/youtrack.js:222-225`).

---

## 6. Milestones / versions / sprints / agiles

**[CLI: ABSENT].** The CLI touches **none** of this. Grep across `src/`, `test/`, `README.md`, `AGENTS.md`: no `/api/agiles`, no sprint endpoint, no version bundle.

Only traces:
- `"SprintCategory"` in the activity-category list (`src/youtrack.js:64`) — sprint *changes* appear in history, but sprints are never queried.
- Sprint/version values would surface generically as `customFields` named `Sprint`, `Fix versions`, `Affected versions`, flattened to strings by `renderFieldValue` (`src/markdown.js:114-118`). Nothing is sprint-aware.

**[API-KNOWLEDGE]** What git-in-track needs:
```
GET /api/agiles?fields=id,name,owner(login),projects(id,shortName,name),
      currentSprint(id,name),
      sprints(id,name,start,finish,archived,isDefault,goal),
      columnSettings(field(name),columns(id,presentation,isResolved,ordinal)),
      sprintsSettings(disableSprints,cardOnSeveralSprints,defaultSprint(id,name),explicitQuery)&$top=100
```
```json
[{ "id": "111-3", "name": "ACME Scrum Board",
   "projects": [{ "id": "0-1", "shortName": "ACME" }],
   "currentSprint": { "id": "112-9", "name": "Sprint 12" },
   "sprints": [ { "id": "112-9", "name": "Sprint 12", "start": 1767225600000,
                  "finish": 1768435200000, "archived": false, "goal": "Ship sync", "$type": "Sprint" } ],
   "$type": "Agile" }]
```
- One sprint: `GET /api/agiles/{aid}/sprints/{sid}?fields=id,name,start,finish,goal,archived`
- Sprint issues: `GET /api/agiles/{aid}/sprints/{sid}/issues?fields=idReadable,summary`, or query form `GET /api/issues?query=Board ACME Scrum Board: {Sprint 12}`
- Create sprint: `POST /api/agiles/{aid}/sprints` `{ "name": "Sprint 13", "start": <ms>, "finish": <ms> }`
- Move issue onto a sprint: set the board's sprint field via the customFields POST (`MultiVersionIssueCustomField` named after the board), or `POST /api/agiles/{aid}/sprints/{sid}/issues` `{ "id": "<issueInternalId>" }`
- **Versions (≈ milestones)** live in a *version bundle* on the project's `Fix versions` field:
  `GET /api/admin/projects/{pid}/customFieldSettings?fields=field(name),bundle(id,$type)` then
  `GET /api/admin/customFieldSettings/bundles/version/{bundleId}/values?fields=id,name,description,releaseDate,released,archived`.
  Create: `POST .../values` `{ "name": "1.5.0", "releaseDate": <ms>, "released": false }`.
  **This — not sprints — is the closest YouTrack analogue to a git-in-track "milestone".** Sprints are board-scoped scheduling buckets.
- `columnSettings.field` names the field the board is grouped by (usually `State`), so a Kanban card move maps to a `State` customField write.

---

## 7. CLI command surface and structure

### Structure **[CLI]**
`yt <scope> <action> [positionals] [--flags]`. Routing is a flat `if` chain in `main()` (`src/cli.js:31-52`): `setup` and `config` run before config loads; everything else does `loadConfig()` + `requireConfig()` first. `project` builds its own client because `--rate` is only known after subcommand parsing (comment `src/cli.js:42-44`).

Arg parsing is hand-rolled (`parseArgs`, `src/cli.js:307-328`): `--key=value`, `--key value`, bare `--flag` → `true`; keys camel-cased (`--no-attachments` → `noAttachments`, `toCamel` `:330-332`). Non-`--` tokens are positionals.

### Commands **[CLI]** (`printHelp`, `src/cli.js:345-374`)

| Command | HTTP calls |
| --- | --- |
| `yt setup --url <url> --token <token>` | none — writes config |
| `yt config` | none |
| `yt issues list [--query <q>] [--limit <n>] [--json]` | `GET /api/issues` |
| `yt issues get <idReadable> [--comments] [--json]` | `GET /api/issues/{id}` (+ `/comments`) |
| `yt issues export <idReadable> [--dependencies] [--comments] [--out <dir>]` | `GET /api/issues/{id}` ×(1 + N linked) |
| `yt issues apply <file.md> [--id <idReadable>]` | `POST /api/issues` or `POST /api/issues/{id}` |
| `yt kb list [--query <q>] [--limit <n>] [--json]` | `GET /api/articles` |
| `yt kb get <article-id> [--json]` | `GET /api/articles/{id}` |
| `yt kb export <article-id> [--out <dir>]` | `GET /api/articles/{id}` |
| `yt kb apply <file.md> [--id <article-id>]` | `POST /api/articles[/{id}]` |
| `yt project export <key> [--out] [--concurrency] [--rate] [--no-attachments] [--no-activities] [--articles] [--since] [--limit] [--fresh] [--force] [--json]` | whole job graph below |
| `yt project status [<key>] [--out <dir>] [--json]` | none — reads the local queue |

### Export job graph **[CLI]** (`handlers`, `src/project-export.js:83-281`)
```
project:index(skip)      GET /api/issues?query=project:{K} [+ since] order by: created asc, $skip, $top=100
  └─ issue:fetch(id)     GET /api/issues/{id}  +  GET /api/issues/{id}/comments
       ├─ issue:activities(cursor)   GET /api/issues/{id}/activitiesPage   (cursor loop)
       └─ issue:attachments(id)      GET /api/issues/{id}/attachments
             └─ asset:download       GET <base><attachment.url>  (signed, streamed)
article:index(skip)      GET /api/articles?query=project:{K}, $skip, $top=100     [--articles only]
  └─ article:fetch(id)   GET /api/articles/{id}
       └─ article:attachments        GET /api/articles/{id}/attachments
             └─ asset:download
```
Priorities: index jobs `20`, fetch jobs `10`, leaves default (`src/project-export.js:107`, `:120`, `:435`).

---

## 8. Endpoint cheat-sheet

`{base}` = normalized instance URL, no trailing slash, may include a context path. All calls carry `Authorization: Bearer perm:...` and `Accept: application/json`; writes add `Content-Type: application/json`. Source: **CLI** = implemented here (line ref), **API** = supplemental, unverified.

| # | Method | Path | Params | `fields` | Purpose | Source |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | GET | `/api/users/me` | — | `id,login,fullName,email,avatarUrl` | Validate token, identify user | API |
| 2 | GET | `/api/admin/projects` | `$top,$skip,query` | `id,shortName,name,archived` | List/search projects, autosuggest | API |
| 3 | GET | `/api/admin/projects/{key}` | — | `id,name,shortName,description` | Resolve shortName→id | CLI `youtrack.js:216` |
| 4 | GET | `/api/admin/projects/{id}/customFieldSettings` | `$top` | `field(name,fieldType(id)),bundle(id,$type),canBeEmpty` | Discover custom fields | API |
| 5 | GET | `/api/admin/customFieldSettings/bundles/enum/{bid}/values` | — | `id,name,description,ordinal` | Enum values (Type, Priority) | API |
| 6 | GET | `/api/admin/customFieldSettings/bundles/state/{bid}/values` | — | `id,name,isResolved` | State values | API |
| 7 | GET | `/api/admin/customFieldSettings/bundles/version/{bid}/values` | — | `id,name,releaseDate,released,archived` | **Milestones / versions** | API |
| 8 | GET | `/api/issues` | `query,$top,$skip` | see §3.1 | Search issues | CLI `youtrack.js:162,233` |
| 9 | GET | `/api/issues/{id}` | — | `ISSUE_FIELDS` | Read one issue | CLI `youtrack.js:167` |
| 10 | POST | `/api/issues` | `fields` | `ISSUE_FIELDS` | Create issue | CLI `youtrack.js:177` |
| 11 | POST | `/api/issues/{id}` | `fields` | `ISSUE_FIELDS` | **Update** issue (partial merge; no PUT) | CLI `youtrack.js:185` |
| 12 | DELETE | `/api/issues/{id}` | — | — | Delete issue | API |
| 13 | GET | `/api/issues/{id}/comments` | `$top,$skip` | `id,text,created,updated,author(id,login,fullName,email)` | Read comments | CLI `youtrack.js:173` |
| 14 | POST | `/api/issues/{id}/comments` | `fields` | `id,text,created,author(login)` | **Create comment** — `{"text":"..."}` | API |
| 15 | POST | `/api/issues/{id}/comments/{cid}` | `fields` | — | Edit comment | API |
| 16 | GET | `/api/issues/{id}/links` | — | `direction,linkType(id,name,sourceToTarget,targetToSource,aggregation),issues(idReadable,summary)` | Parent/subtask/relates graph | API (CLI reads via `fields`: `youtrack.js:18`) |
| 17 | POST | `/api/issues/{id}/links/{typeId}{s\|t}/issues` | — | — | Create link; `{"id":"<otherId>"}` | API |
| 18 | GET | `/api/issueLinkTypes` | `$top` | `id,name,sourceToTarget,targetToSource,directed,aggregation` | Enumerate link types | API |
| 19 | GET | `/api/issues/{id}/attachments` | — | `ATTACHMENT_FIELDS` | List attachments | CLI `youtrack.js:244` |
| 20 | POST | `/api/issues/{id}/attachments` | `fields` | `id,name,url` | Upload (multipart/form-data) | API |
| 21 | GET | `{base}{attachment.url}` | signed `?sign=` | — | Download binary | CLI `youtrack.js:292,387` |
| 22 | GET | `/api/issues/{id}/activitiesPage` | `categories,$top,reverse,cursor` | `activities(...),afterCursor,hasAfter` | Audit trail, cursor-paged | CLI `youtrack.js:251` |
| 23 | GET | `/api/articles` | `query,$top,$skip` | `id,idReadable,summary,updated,project(shortName)` | List/search KB articles | CLI `youtrack.js:193,275` |
| 24 | GET | `/api/articles/{id}` | — | `ARTICLE_FIELDS` | Read article (`content` = Markdown) | CLI `youtrack.js:197` |
| 25 | POST | `/api/articles` | `fields` | `ARTICLE_FIELDS` | Create article (needs `project`) | CLI `youtrack.js:201` |
| 26 | POST | `/api/articles/{id}` | `fields` | `ARTICLE_FIELDS` | Update article | CLI `youtrack.js:209` |
| 27 | GET | `/api/articles/{id}/childArticles` | `$top` | `id,idReadable,summary,ordinal,hasChildren` | Walk KB tree down | API |
| 28 | GET/POST | `/api/articles/{id}/comments` | `fields` | `id,text,created,author(login)` | Article comments | API |
| 29 | GET | `/api/articles/{id}/attachments` | — | `ATTACHMENT_FIELDS` | Article attachments | CLI `youtrack.js:269` |
| 30 | GET | `/api/admin/projects/{id}/articles` | `$top,$skip` | `id,idReadable,summary` | Articles of a project | API |
| 31 | GET | `/api/agiles` | `$top` | `id,name,projects(shortName),currentSprint(id,name),sprints(id,name,start,finish,archived,goal)` | Boards + **sprints** | API |
| 32 | GET | `/api/agiles/{aid}/sprints/{sid}/issues` | `$top` | `idReadable,summary` | Issues in a sprint | API |
| 33 | GET | `/api/savedQueries` | `$top` | `id,name,query,owner(login)` | Saved searches (UI presets) | API |
| 34 | GET | `/api/issueTags` | `$top` | `id,name,color(background,foreground)` | Tags | API |

---

## 9. Example JSON shapes

### Issue as the CLI receives it (`ISSUE_FIELDS`)
```json
{
  "id": "2-345", "idReadable": "ACME-42",
  "summary": "Login fails on Safari",
  "description": "Steps to reproduce...\n\n- [ ] confirm",
  "created": 1767225600000, "updated": 1767312000000, "resolved": null,
  "project": { "id": "0-1", "name": "Acme Platform", "shortName": "ACME" },
  "reporter": { "id": "1-1", "login": "jose", "fullName": "Jose F. Rives", "email": "jose@digio.es" },
  "customFields": [
    { "id": "110-1", "name": "Type",       "value": { "id": "70-1", "name": "Bug" } },
    { "id": "110-2", "name": "State",      "value": { "id": "71-3", "name": "In Progress" } },
    { "id": "110-3", "name": "Priority",   "value": { "id": "72-2", "name": "Critical" } },
    { "id": "110-4", "name": "Assignee",   "value": { "id": "1-7", "login": "ana", "fullName": "Ana Ruiz" } },
    { "id": "110-5", "name": "Estimation", "value": { "id": "0", "presentation": "3d" } },
    { "id": "110-6", "name": "Sprints",    "value": [ { "id": "112-9", "name": "Sprint 12" } ] },
    { "id": "110-7", "name": "Due Date",   "value": 1768435200000 }
  ],
  "links": [
    { "direction": "INWARD",
      "linkType": { "id": "97-1", "name": "Subtask", "sourceToTarget": "parent for", "targetToSource": "subtask of" },
      "issues": [ { "id": "2-300", "idReadable": "ACME-30", "summary": "Auth epic" } ] },
    { "direction": "OUTWARD",
      "linkType": { "id": "97-1", "name": "Subtask", "sourceToTarget": "parent for", "targetToSource": "subtask of" },
      "issues": [] },
    { "direction": "BOTH",
      "linkType": { "id": "97-2", "name": "Relates", "sourceToTarget": "relates to", "targetToSource": "relates to" },
      "issues": [ { "id": "2-350", "idReadable": "ACME-47", "summary": "Safari CSP header" } ] }
  ]
}
```
**Most `links[]` entries have `"issues": []`** and must be filtered (`src/markdown.js:136`).

### Comment
```json
[{ "id": "4-88", "text": "Reproduced on 17.4.", "created": 1767230000000, "updated": null,
   "author": { "id": "1-7", "login": "ana", "fullName": "Ana Ruiz", "email": "ana@example.com" } }]
```

### Attachment
```json
[{ "id": "8-12", "name": "screenshot.png", "size": 84213, "mimeType": "image/png",
   "extension": "png", "charset": null, "created": 1767230500000,
   "url": "/api/files/8-12?sign=MTc2NzIzMDUwMHwx...&updated=1767230500000",
   "author": { "login": "ana" }, "comment": null }]
```
`url` is **relative and signed**; resolve per `attachmentUrl()` (`src/youtrack.js:387-393`).

### Article
```json
{ "id": "42-7", "idReadable": "ACME-A-3",
  "summary": "Deployment handbook",
  "content": "## Prerequisites\n\nYou need `kubectl`...\n\nSee ACME-42 for context.",
  "ordinal": 2, "created": 1760000000000, "updated": 1767300000000,
  "project": { "id": "0-1", "name": "Acme Platform", "shortName": "ACME" },
  "parentArticle": { "id": "42-1", "idReadable": "ACME-A-1", "summary": "Operations" },
  "reporter": { "id": "1-1", "login": "jose", "fullName": "Jose F. Rives", "email": "jose@digio.es" } }
```

### Activity entry
```json
{ "activities": [
    { "id": "5-1.0-0", "timestamp": 1767312000000, "$type": "CustomFieldActivityItem",
      "category": { "id": "CustomFieldCategory" },
      "author": { "id": "1-7", "login": "ana", "fullName": "Ana Ruiz" },
      "field": { "id": "110-2", "name": "State", "presentation": "State" },
      "added":   [ { "id": "71-3", "name": "In Progress" } ],
      "removed": [ { "id": "71-1", "name": "Open" } ],
      "target":  { "id": "2-345", "idReadable": "ACME-42" } } ],
  "afterCursor": "eyJvIjoxMjN9", "hasAfter": true }
```

### Exported Markdown contract (`README.md:192-206`, `src/markdown.js:16-38`)
```markdown
---
kind: issue
id: 2-345
idReadable: ACME-42
summary: Login fails on Safari
project:
  id: 0-1
  name: Acme Platform
  shortName: ACME
created: 1767225600000
updated: 1767312000000
reporter:
  login: jose
assignee: Ana Ruiz
customFields:
  Type: Bug
  State: In Progress
  Priority: Critical
  Assignee: Ana Ruiz
  Estimation: 3d
links:
  - type: Subtask
    direction: INWARD
    issues:
      - idReadable: ACME-30
        summary: Auth epic
exportedAt: 2026-09-13T09:00:00.000Z
sourceUrl: https://yt.example.com/issue/ACME-42
---

# ACME-42 Login fails on Safari

Steps to reproduce...
```
`kind` is `issue` or `article` (`src/markdown.js:18`, `:42`). `exportedAt`/`sourceUrl` are CLI-added, not YouTrack fields. On apply, everything except `summary`, `description`, `project`, `customFields` is **discarded** (`src/markdown.js:58-70`).

---

## 10. Explicit gaps — what the CLI does NOT do

1. **No token validation** — no `GET /api/users/me`. §1
2. **No project list or search** — only get-by-key, and that method is dead code. §2
3. **No comment creation/editing/deletion.** Read-only. §3.6
4. **No `/api/issues/{id}/links` use, no link create/delete**, no link-type enumeration, no parent/subtask semantics — link types are opaque labels. §3.5
5. **No `$type` sent on custom-field writes, none requested on reads.** Works for simple enums, fragile elsewhere. §3.7
6. **No agiles, sprints, boards, or version bundles.** Nothing milestone-shaped. §6
7. **No attachment upload.** Download only. §3.9
8. **No `childArticles`** — KB tree walks up only. §4
9. **No paging on comments or attachments** (`$top` omitted) — silently truncates on big issues.
10. **No caching, no ETag/conditional requests, no request batching.** §5
11. **No custom-field schema discovery** — trusts frontmatter field names, lets the server reject.
12. **Articles paging is not order-stabilized** (unlike issues) — `listProjectArticles` (`src/youtrack.js:274-283`) sends `$skip`/`$top` with no `order by:`. Do not copy.
13. **Issue `description` vs article `content`** — different field names for the same concept; a common bug source when writing one adapter for both.