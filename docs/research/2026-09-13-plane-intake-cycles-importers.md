---
title: Research — Plane intake, cycles and importers
type: page
tags: [research, plane, inbox, cycles]
---

# Plane research report — Intake, Cycles, Importers, AI, UX

Repo: `/www/Github/plane` (monorepo). Django backend lives in **`apps/api`** (not `apiserver/`, the
directory was renamed). Frontends: `apps/web` (main app), `apps/space` (public/deploy boards),
shared packages in `packages/` (`types`, `constants`, `utils`, `services`, `propel` = design system).

---

## 1. Intake / Inbox

### 1.1 Data model

`/www/Github/plane/apps/api/plane/db/models/intake.py`

- `Intake(ProjectBaseModel)` — L12-35. Fields: `name` (L13), `description` (L14),
  `is_default` (L15), `view_props` JSON (L16), `logo_props` JSON (L17).
  Unique `(name, project)` while `deleted_at IS NULL` (L23-31). Table `intakes`.
  It is **per project**, and a project can in principle hold several intakes, but all
  the code paths do `Intake.objects.filter(project_id=...).first()` — effectively one.
- `SourceType(models.TextChoices)` — L38-39: only `IN_APP` in the OSS build.
- `IntakeIssueStatus(models.IntegerChoices)` — L42-47:
  `PENDING = -2`, `REJECTED = -1`, `SNOOZED = 0`, `ACCEPTED = 1`, `DUPLICATE = 2`.
  (Frontend calls `-1` **DECLINED**, same value — `packages/constants/src/intake.ts` L23-27.)
- `IntakeIssue(ProjectBaseModel)` — L50-84:
  - `intake` FK (L51), `issue` FK → `db.Issue` (L52) — **join model; the Issue exists from
    the moment of submission**, intake only wraps it with a triage status.
  - `status` int, default `-2` (L53-62)
  - `snoozed_till` DateTime null (L63)
  - `duplicate_to` FK → `db.Issue`, `SET_NULL` (L64-69)
  - `source` char, default `"IN_APP"` (L70), `source_email` text (L71)
  - `external_source` / `external_id` (L72-73) — for imported/integration-created intake items
  - `extra` JSON (L74)
  - Table `intake_issues`, ordered `-created_at`.

Project-level toggle: `Project.intake_view = BooleanField(default=False)`
(`apps/api/plane/db/models/project.py` L97). Public boards bind an intake:
`DeployBoard.intake` FK (`project.py` L304).

The **Triage state**: intake work items are created in a special `State` whose group is
`StateGroup.TRIAGE`. `State.triage_objects` is a dedicated manager; normal state lists exclude
triage (`apps/api/plane/app/views/state/base.py` L39, L115) and states with group `TRIAGE`
cannot be created through the API (`apps/api/plane/app/serializers/state.py` L32-33,
`apps/api/plane/api/serializers/state.py` L24-25). `Issue.issue_objects` excludes triage-group
issues from normal issue lists (`apps/api/plane/db/models/issue.py` L97) — **this is the
mechanism that keeps intake items out of the backlog until accepted.**

### 1.2 API endpoints

Internal app API — `/www/Github/plane/apps/api/plane/app/urls/intake.py`:
- `…/projects/<project_id>/intakes/` + `/<pk>/` → `IntakeViewSet` (L16-25)
- `…/projects/<project_id>/intake-issues/` + `/<pk>/` → `IntakeIssueViewSet` (L26-35)
- Legacy aliases `inboxes/` and `inbox-issues/` kept for back-compat (L36-55)
- `…/intake-work-items/<work_item_id>/description-versions/` (L56-64)

Public REST API — `apps/api/plane/api/urls/intake.py` L13-23:
`intake-issues/` (GET/POST) and `intake-issues/<issue_id>/` (GET/PATCH/DELETE)
→ `IntakeIssueListCreateAPIEndpoint` / `IntakeIssueDetailAPIEndpoint`
(`apps/api/plane/api/views/intake.py`). This is the **"form intake / external intake" surface**:
any API token can POST a work item straight into triage.

Public un-authenticated board API — `apps/api/plane/space/urls/intake.py` L14-29:
`anchor/<anchor>/intakes/<intake_id>/intake-issues/` → `IntakeIssuePublicViewSet`
(`apps/api/plane/space/views/intake.py`). This is the **public intake form**: anonymous/logged-in
visitors of a published project board can submit items.

**Email intake does not exist in the OSS repo** — only the `source_email` column (intake.py L71)
and `source` (L70) are reserved for it; `SourceType` has only `IN_APP`. Same for form intake
beyond the space board. Those live in Plane's closed "silo"/enterprise service (there is no
`apps/silo` in this checkout; `packages/constants/src/workspace.ts` L34 still reserves the slug
`"silo"`).

### 1.3 Workflow

**Create** (`apps/api/plane/app/views/intake/base.py` L229-331, allowed for ADMIN/MEMBER/**GUEST**):
1. validate name + priority (L230-241)
2. find or create the project's Triage state (L246-258) and force `state_id` onto the payload (L259)
3. create the `Issue` with `allow_triage_state: True` in serializer context (L262-270)
4. create `IntakeIssue(status=-2 default, source=IN_APP)` (L272-278)
5. fire `issue_activity.delay(type="issue.activity.created", …, intake=<intake_issue_id>)` (L280-291)
   and `issue_description_version_task.delay(...)` (L293-298).

The public-board variant is `apps/api/plane/space/views/intake.py` L108-186 — same shape, plus
HTML sanitisation of `description_html` (`validate_html_content`, L153-156) and a check that the
supplied `intake_id` actually belongs to the anchor (L116-122).

**Update / triage** (`apps/api/plane/app/views/intake/base.py` L335-503):
- Two independent serializers in one PATCH: the nested `issue` payload (`IssueCreateSerializer`,
  L414-416) and the intake-level payload (`IntakeIssueSerializer`, L425) which carries
  `status`, `snoozed_till`, `duplicate_to`.
- Permissions are layered: guests may only edit `name` / `description_html` / `description_json`
  of their own submission (L402-407); only role > MEMBER or workspace admin may change the
  intake status fields (L423-430).
- A status change emits an `intake.activity.created` activity
  (L462-474 → `create_intake_activity` in `apps/api/plane/bgtasks/issue_activities_task.py`
  L1466-1492, field `"intake"`, comment `"updated the intake status"`).

**What happens on accept.** The backend does *not* move the issue out of Triage by itself.
The web client does it in two steps — `apps/web/core/components/inbox/content/inbox-issue-header.tsx`:
- `canMarkAsAccepted/Declined/Duplicate` only when current status is `PENDING (-2)` or
  `SNOOZED (0)` (L98-100); once in `[-1, 1, 2]` the item is "accepted or declined" (L111)
  and the action bar collapses.
- "Accept" opens the normal `CreateUpdateIssueModal` retitled *Move to project*
  (L256-271) with `beforeFormSubmit={handleInboxIssueAccept}`; the user picks a real state,
  assignee, cycle, module, and the form submit writes the issue with a non-triage state,
  which makes it appear in the normal backlog. `handleInboxIssueAccept` (L138-143) does
  `updateInboxIssueStatus(EInboxIssueStatus.ACCEPTED)` then navigates to the next intake item.
- Decline → `DeclineIssueModal` → `updateInboxIssueStatus(DECLINED)` (L145-150).
- Snooze → `InboxIssueSnoozeModal` (date picker) → `updateInboxIssueSnoozeTill(date)` (L152-157);
  un-snooze passes `undefined` and the store flips the status back to `PENDING` (L169-176).
- Duplicate → `SelectDuplicateInboxIssueModal` (issue search combobox) →
  `updateInboxIssueDuplicateTo(issueId)` (L158-160).
- Delete → `DeleteInboxIssueModal`.

Store: `/www/Github/plane/apps/web/core/store/inbox/inbox-issue.store.ts`
- `updateInboxIssueStatus` L99-142: optimistic set, PATCHes `{status}`, maintains the project's
  `intake_count` badge (L114-127) and the paginated `total_results` (L129-133); on ACCEPTED it
  pushes the issue into the normal issue store so it shows up in the backlog immediately (L135-139);
  rolls back on error (L140-141).
- `updateInboxIssueDuplicateTo` L145-178 — sets status to `DUPLICATE` **and** `duplicate_to` in one PATCH.
- `updateInboxIssueSnoozeTill` L182-215 — `status = date ? SNOOZED : PENDING`, `snoozed_till = date|null`.

**Delete semantics** (`apps/api/plane/app/views/intake/base.py` L551-569): if the intake status is
in `[-2, -1, 0, 2]` (i.e. *not* accepted) the **underlying Issue is deleted too**; if accepted,
only the IntakeIssue row is removed and the work item survives in the project.

**Snooze expiry**: the list query filters `Q(snoozed_till__gte=now) | Q(snoozed_till__isnull=True)`
(`apps/api/plane/api/views/intake.py` L77, L251) — a snoozed item simply reappears when the date
passes; there is no cron job.

**Automation interplay**: auto-archive/auto-close tasks only touch issues whose intake status is
accepted/rejected/duplicate or which have no intake row at all
(`apps/api/plane/bgtasks/issue_automation_task.py` L50-53, L111-113).

**Webhooks**: `intake_issue` is a first-class webhook entity
(`apps/api/plane/bgtasks/webhook_task.py` L67, L79).

### 1.4 Frontend UI

Root: `/www/Github/plane/apps/web/core/components/inbox/` (route is `/intake`, the folder kept the
old name).
- `root.tsx` — two-pane layout.
- `sidebar/root.tsx`, `sidebar/inbox-list.tsx`, `sidebar/inbox-list-item.tsx` — the queue list with
  infinite scroll.
- `content/root.tsx`, `content/issue-root.tsx`, `content/issue-properties.tsx`,
  `content/inbox-issue-header.tsx` (desktop action bar), `content/inbox-issue-mobile-header.tsx`.
- `inbox-issue-status.tsx`, `inbox-status-icon.tsx` — status pill/icon.
- `inbox-filter/root.tsx`, `inbox-filter/filters/{status,priority,state,labels,members,date,filter-selection}.tsx`,
  `inbox-filter/applied-filters/*`, `inbox-filter/sorting/order-by.tsx`.
- `modals/{snooze-issue-modal,select-duplicate,decline-issue-modal,delete-issue-modal}.tsx`
  and `modals/create-modal/*` (title / description / properties / `create-root.tsx`).
- Loaders: `core/components/ui/loader/layouts/project-inbox/*`.
- Activity renderer for intake status changes:
  `core/components/issues/issue-detail/issue-activity/activity/actions/inbox.tsx`.

Stores: `core/store/inbox/project-inbox.store.ts` (tabs, filters, cursor pagination — `currentTab`
L89 defaults to `OPEN`; tab → status set mapping at L159-165 and L271-276; cursor pagination with
`next_cursor` at L382-396) and `core/store/inbox/inbox-issue.store.ts` (per-item actions).

Constants: `packages/constants/src/intake.ts` — `INBOX_STATUS` L10-46 (pending/declined/snoozed/
accepted/duplicate), `INBOX_ISSUE_ORDER_BY_OPTIONS` L48-61, sort options L63-72.
Open tab = statuses `[-2, 0]`, Closed tab = `[-1, 1, 2]`
(`core/components/inbox/inbox-filter/filters/status.tsx` L35-37).

There is also an intake-state dropdown at `apps/web/core/components/dropdowns/intake-state/`.

---

## 2. Cycles

### 2.1 Data model

`/www/Github/plane/apps/api/plane/db/models/cycle.py`

- `Cycle(ProjectBaseModel)` L60-101: `name` (L61), `description` (L62), `start_date` /
  `end_date` DateTime nullable (L63-64), `owned_by` FK user (L65-69), `view_props` JSON (L70),
  `sort_order` float (L71, new cycles get `min - 10000` in `save()` L88-97 → manual ordering),
  `external_source` / `external_id` (L72-73), `progress_snapshot` JSON (L74),
  `archived_at` (L75), `logo_props` (L76), `timezone` (L78-79), `version` int default 1 (L80).
- `CycleIssue(ProjectBaseModel)` L104-127 — plain join `issue` ↔ `cycle`, **unique
  `(cycle, issue)` while not deleted** (L113-120). Note: a `CycleIssue` has no `sort_order`; the
  intake/issue queryset takes `cycle_id` as a `Subquery(... [:1])`
  (`apps/api/plane/app/views/intake/base.py` L115-118) — i.e. **an issue belongs to at most one
  cycle at a time**, unlike Modules which are many-to-many.
- `CycleUserProperties` L130-157 — per-user filters/display filters/display properties/rich filters
  for the cycle's issue view.

`version` is a data-format flag: `version === 2` cycles carry a pre-computed `progress[]` time
series, `version === 1` fall back to the snapshot/analytics endpoints
(`apps/web/core/store/cycle.store.ts` L248-249).

### 2.2 Constraints

- **No overlapping cycles per project.** `CycleDateCheckEndpoint`
  (`apps/api/plane/app/views/cycle/base.py` L520-556): any cycle whose range intersects
  `[start, end]` (three `Q` clauses, L542-546), excluding the cycle being edited, blocks the save
  with *"You have a cycle already on the given dates, if you want to create a draft cycle you can
  do that by removing dates"* (L549-554). Dates are converted to UTC through the **project's
  timezone** (`convert_to_utc`, L532-536).
- **Both dates or neither.** `CycleViewSet.create` L271-333: rejects a payload with only one of
  `start_date` / `end_date` (L331-333). A cycle with no dates is a **draft**.
- Archived cycles cannot be edited (`partial_update` L336+ returns early when `cycle.archived_at`).
- Therefore "one active cycle" is an emergent property, not an explicit constraint: since ranges
  cannot overlap, at most one cycle can be `CURRENT`.

### 2.3 Derived status

`apps/api/plane/app/views/cycle/base.py`, inside `CycleViewSet.get_queryset` (offset ~L153-165 of
the file; the annotation is a `Case`):
```
CURRENT   : start_date <= now <= end_date
UPCOMING  : start_date >  now
COMPLETED : end_date   <  now
DRAFT     : otherwise (no dates)
```
`now` is first converted to the project's timezone then back to UTC (same function, L84-89 of the
queryset). Frontend mirror: `TCycleGroups = "current" | "upcoming" | "completed" | "draft"`
(`packages/types/src/cycle/cycle.ts` L10); labels/colors in `packages/constants/src/cycle.ts`
L8-48; ordering helper `orderCycles` in `packages/utils/src/cycle.ts` L21-42 (current → upcoming →
draft, completed excluded from the "active" list); `shouldFilterCycle` L50-71.

### 2.4 Endpoints

`/www/Github/plane/apps/api/plane/app/urls/cycle.py`:
| path | view | line |
|---|---|---|
| `cycles/` , `cycles/<pk>/` | `CycleViewSet` | L22-38 |
| `cycles/<cycle_id>/cycle-issues/` , `…/<issue_id>/` | `CycleIssueViewSet` | L39-55 |
| `cycles/date-check/` | `CycleDateCheckEndpoint` | L56-60 |
| `user-favorite-cycles/` | `CycleFavoriteViewSet` | L61-70 |
| `cycles/<cycle_id>/transfer-issues/` | `TransferCycleIssueEndpoint` | L71-75 |
| `cycles/<cycle_id>/user-properties/` | `CycleUserPropertiesEndpoint` | L76-80 |
| `cycles/<cycle_id>/archive/`, `archived-cycles/`, `archived-cycles/<pk>/` | `CycleArchiveUnarchiveEndpoint` | L81-95 |
| `cycles/<cycle_id>/progress/` | `CycleProgressEndpoint` | L96-100 |
| `cycles/<cycle_id>/analytics/` | `CycleAnalyticsEndpoint` | L101-105 |

Public REST API equivalent: `apps/api/plane/api/views/cycle.py` (1266 lines).
Archive views: `apps/api/plane/app/views/cycle/archive.py`; cycle-issue views:
`apps/api/plane/app/views/cycle/issue.py`.

### 2.5 Transfer issues

`TransferCycleIssueEndpoint` — `apps/api/plane/app/views/cycle/base.py` L594-622: takes
`new_cycle_id`, delegates to `transfer_cycle_issues(...)`.

`/www/Github/plane/apps/api/plane/utils/cycle_transfer_issues.py` (478 lines):
1. Reject if the destination cycle is already completed (`end_date < now`) — L58-63.
2. Aggregate the source cycle's counts by state group (total / completed / cancelled / started /
   unstarted / backlog) — L65-120+.
3. Build the **assignee distribution** (L280-346) and **label distribution** (L348-393), each with
   total / completed / pending counts.
4. Build a burndown via `burndown_plot(...)` (L396-402) — implementation in
   `apps/api/plane/utils/analytics_plot.py` L123+: expands `start_date..end_date` into a day array,
   buckets `completed_at` by day (or sums `estimate_point__value` for `plot_type == "points"`),
   producing the ideal/actual chart series.
5. **Freeze it into `cycle.progress_snapshot`** (L405-432, saved with
   `save(update_fields=["progress_snapshot"])`). From then on the progress and analytics endpoints
   read the snapshot instead of live-querying
   (`CycleProgressEndpoint` L712-718, `CycleAnalyticsEndpoint` L821-822) — because the issues have
   physically left the cycle.
6. Move only the **incomplete** issues (`state__group in [backlog, unstarted, started]`) via
   `CycleIssue.objects.bulk_update(..., ["cycle_id"], batch_size=100)` (L433-458).
7. Emit one `cycle.activity.created` activity carrying `updated_cycle_issues`
   `[{old_cycle_id, new_cycle_id, issue_id}]` (L460-477).

### 2.6 Progress / analytics

- `CycleProgressEndpoint` (`apps/api/plane/app/views/cycle/base.py` L658-784) returns per state
  group: `*_issues` counts and `*_estimate_points` (points come from
  `estimate_point__estimate__type == "points"`, `Cast(value, FloatField)`, summed with
  `Case/When` per state group — L665-710). Snapshot wins when present (L712-718).
- `CycleAnalyticsEndpoint` (L786+) — `?type=issues|points`, returns the label/assignee/burndown
  distribution, again preferring `progress_snapshot["distribution"]` (L821-822).
- Types: `packages/types/src/cycle/cycle.ts` — `TProgressSnapshot` L66-81,
  `TCycleDistribution` L42-46, `TCycleProgress` (date/started/actual/pending/ideal/scope/…) L53-64,
  `ICycle` L87-114.
- Client services: `packages/services/src/cycle/{cycle,cycle-analytics,cycle-operations,cycle-archive,sites-cycle}.service.ts`
  (e.g. `cycle-analytics.service.ts` L30-43 analytics, L53-62 progress).

### 2.7 Frontend

`/www/Github/plane/apps/web/core/components/cycles/`:
- List: `list/root.tsx`, `list/cycles-list-map.tsx`, `list/cycles-list-item.tsx`,
  `list/cycle-list-item-action.tsx`, `list/cycle-list-group-header.tsx` (grouped by derived status),
  `cycles-view.tsx`, `cycles-view-header.tsx`.
- Active cycle: `active-cycle/root.tsx`, `active-cycle/progress.tsx`,
  `active-cycle/productivity.tsx`, `active-cycle/cycle-stats.tsx`,
  `active-cycle/use-cycles-details.ts`; workspace-level upgrade teaser at
  `core/components/active-cycles/workspace-active-cycles-upgrade.tsx`.
- Detail sidebar with charts: `analytics-sidebar/root.tsx`, `issue-progress.tsx`,
  `progress-stats.tsx`, `sidebar-chart.tsx`, `sidebar-details.tsx`, `sidebar-header.tsx`.
- Create/edit: `modal.tsx`, `form.tsx`; `delete-modal.tsx`; `quick-actions.tsx`;
  `cycle-peek-overview.tsx`.
- Transfer: `transfer-issues.tsx` (the "Completed cycles are not editable" banner + *Transfer work
  items* button, L18-42) and `transfer-issues-modal.tsx` (searchable list of
  `currentProjectIncompleteCycleIds`, success/error toasts at L47-60).
- Archived: `archived-cycles/{root,view,header,modal}.tsx`.
- Filters/dropdowns: `applied-filters/{root,status,date}.tsx`,
  `dropdowns/filters/{root,status,start-date,end-date}.tsx`,
  `dropdowns/estimate-type-dropdown.tsx`; cycle picker for issues at
  `core/components/dropdowns/cycle/{index,cycle-options}.tsx`.
- Stores: `core/store/cycle.store.ts`, `core/store/cycle_filter.store.ts`; hooks
  `core/hooks/store/use-cycle.ts`, `use-cycle-filter.ts`.

---

## 3. Cycles vs sprints — recommendation for git-in-track

**How Plane's cycle differs from a Scrum sprint**

| | Plane cycle | Classic sprint |
|---|---|---|
| Cardinality | An issue is in **at most one** cycle (unique `(cycle, issue)` + `Subquery[:1]`) | same |
| Overlap | **Forbidden** at project level (date-check endpoint) | usually enforced by convention |
| Draft | A cycle **without dates** is a draft and is exempt from the overlap rule | no equivalent |
| Ceremonies | None. No commitment, no sprint goal field, no planning/review/retro objects | central |
| Velocity | Not modelled. Only per-cycle burndown + estimate-point sums | velocity across sprints |
| Rollover | Explicit, destructive-ish `transfer-issues` that **snapshots** the source cycle before moving incomplete items | "move to next sprint" |
| Lifecycle | Purely **derived from dates** (draft/upcoming/current/completed) — no start/stop buttons | explicit start/complete |
| Archival | `archived_at` + separate archived list | sprint closed |
| Cross-project | Cycles are project-scoped; "active cycles" workspace view is a paid add-on | — |

Plane also has **Modules** as the *other* grouping (many-to-many, no dates) — cycles are
deliberately the time-boxed one, modules the thematic one. git-in-track's epics/stories play the
module role.

**Recommendation: extend sprints rather than add a second entity.**
git-in-track already has sprints on scrum boards; a "cycle" is a sprint with (a) dates as the
single source of truth for state, (b) a hard no-overlap rule, and (c) a frozen progress snapshot
on rollover. Those are three fields and two rules, not a new noun. Concretely, borrow:

1. **Derive state from dates** (`draft` = no dates, `upcoming`, `current`, `completed`) instead of
   an explicit status field. It removes a whole class of "someone forgot to close the sprint" bugs
   and makes the Markdown files declarative — exactly the git-native model.
2. **Draft sprints = no dates.** Lets planners stage work without booking calendar space.
3. **No-overlap validation** on the sprint's date range within a board/project, with the
   "remove the dates to make it a draft" escape hatch — Plane's error message is worth copying
   verbatim in spirit.
4. **`transfer-issues` semantics**: on rollover, write a `progress_snapshot` block into the closing
   sprint's front-matter (totals by state, per-assignee and per-label distribution, burndown series),
   then move only items in backlog/unstarted/started. In a git-native product the snapshot is
   *especially* valuable: it makes historical velocity readable from the file itself without
   replaying history, and it survives the items leaving the sprint.
5. **`version` field** on the sprint front-matter so the snapshot format can evolve
   (Plane's `Cycle.version` 1 vs 2).
6. Skip for now: `view_props` / per-user cycle properties, workspace-level "active cycles".

Adding a *separate* Cycle entity alongside Sprint would force every item to answer "which of the
two time boxes am I in", duplicate the board plumbing, and invite the exact ambiguity Plane avoids
by keeping cycles (time) and modules (theme) orthogonal.

---

## 4. Integrations / import

**Important finding: the actual importers are NOT in this OSS repo.** What remains:

- `Importer` model — `/www/Github/plane/apps/api/plane/db/models/importer.py` L13-40:
  `service` ∈ {`github`, `jira`} (L14), `status` ∈ {`queued`, `processing`, `completed`, `failed`}
  default `queued` (L15-24), `initiated_by` FK user (L25), `metadata` JSON (credentials/handles,
  L26), `config` JSON (L27), `data` JSON (L28), `token` FK → `APIToken` (L29),
  `imported_data` JSON (L30). Table `importers`.
  **This is the job-record pattern to copy**: one row per import run, four-state machine,
  three JSON blobs (metadata = connection, config = options, data = user-supplied mapping),
  and an API token so the worker can call the product's own public API rather than reach into the DB.
- Serializer `apps/api/plane/app/serializers/importer.py`.
- Frontend services still call the (now-removed) endpoints:
  `apps/web/core/services/integrations/jira.service.ts` L18 (`GET /api/workspaces/<slug>/importers/jira`),
  L28 (`POST /api/workspaces/<slug>/projects/importers/jira/`);
  `.../github.service.ts` L29, L39; `.../integration.service.ts` L42-43 (list), L66-67 (delete).
- Form shapes worth copying — `packages/types/src/importer/jira-importer.ts`:
  `IJiraImporterForm = { metadata: {cloud_hostname, api_token, project_key, email},
  config: {epics_to_modules: boolean}, data: {users[], invite_users, total_issues, total_labels,
  total_states, total_modules}, project_id }` (L7-38), and the pre-flight `IJiraResponse`
  `{issues, modules, labels, states, users[]}` (L40-46) — i.e. **"connect → probe counts →
  map users → confirm → enqueue"**.
  `github-importer.ts` L7-24 is the same shape with `config: {sync: boolean}` (continuous sync).
  Note `epics_to_modules` — the importer explicitly asks how to translate the foreign hierarchy.
- Menu entries: `packages/constants/src/workspace.ts` L139-146 (`importer.github.*`,
  `importer.jira.*`), L34 reserves the `silo` slug, L50 the `importers` settings page.

**The external-id contract (this is the reusable part).** Every importable model carries
`external_source` + `external_id` char fields:
`issue.py` L162-163 (Issue), L402-403 (IssueComment), L472-473, L705-706;
`state.py` L92-93, `label.py` L23-24, `module.py` L96-97, `cycle.py` L72-73,
`issue_type.py` L23-24, `page.py` L57-58, `asset.py` L61-62, `draft.py` L68-69,
`project.py` L118-119, `intake.py` L72-73. `Issue.objects` copies them into archived/clone rows
(`issue.py` L766-767).

They are used for **idempotent upsert** in the public API — `apps/api/plane/api/views/issue.py`:
- `GET …/issues/?external_id=&external_source=` looks up by the pair (L331-337)
- `POST` refuses to create a duplicate when `(external_id, external_source)` already exists and
  returns the existing issue (L468-481)
- `PUT`/upsert-by-external-id path requires both and 400s otherwise (L625-637, L745)
- update guards against changing the pair (L789-795)
- comments do the same (L929-930).

So: **the importer is just a client of the public REST API, authenticated with an `APIToken`,
using `(external_source, external_id)` as the idempotency key.** That is why re-running an import
is safe and why incremental sync works.

**Batching / job pattern in the repo.** Celery is the queue (`apps/api/plane/celery.py`,
tasks in `apps/api/plane/bgtasks/`). The closest live analogue to an import job is the
**exporter**: `ExporterHistory` (`apps/api/plane/db/models/exporter.py` L24-50) with the identical
`queued/processing/completed/failed` status field (L37-46), a `reason` text for the failure
message (L47), `key`/`url` for the produced artifact (L48-49) and a unique `token` (L50);
driven by `apps/api/plane/bgtasks/export_task.py` and expired by
`apps/api/plane/bgtasks/exporter_expired_task.py`. Bulk DB writes use
`bulk_create` / `bulk_update(..., batch_size=100)` (e.g. `cycle_transfer_issues.py` L458,
`dummy_data_task.py` L361). Progress is **polled** (the client refetches the history row); there is
no streaming progress channel in OSS.

**For git-in-track's YouTrack import, the pattern to copy is:**
1. An `Import` record in the repo (or `.gintrack/imports/<id>.md`) with
   `source`, `status(queued|processing|completed|failed)`, `metadata` (host + token ref),
   `config` (e.g. `subtasks_to_stories`, `boards_to_sprints`), `data` (user map, state map,
   counts from the pre-flight probe), `reason`, `imported_data` (id map).
2. A **pre-flight probe** endpoint that returns counts (issues/states/labels/users/sprints) and the
   list of foreign users so the UI can render the mapping step before anything is written.
3. `external_source: "youtrack"` + `external_id: "<PROJ-123>"` in every generated item's
   front-matter, plus the original URL as a link — and use the pair as the idempotency key so
   re-import updates rather than duplicates.
4. Map: YouTrack project → git-in-track project; issue type/subtask hierarchy → epic/story/task
   (make it a `config` choice as Plane did with `epics_to_modules`); YouTrack states →
   your state groups (backlog/unstarted/started/completed/cancelled — Plane's five groups are a
   good normalisation target, plus `triage` for anything landing in the inbox); tags → labels;
   sprints/agile boards → sprints; comments → comments with their own `external_id`;
   attachments → assets with `external_id`.
5. Batch the writes (Plane uses 100) and make the job resumable off `imported_data`.

---

## 5. AI / agent features

OSS Plane has exactly one AI surface: a **"GPT assistant"** text-rewriter.

- Endpoints — `/www/Github/plane/apps/api/plane/app/urls/external.py` L14-23:
  `workspaces/<slug>/projects/<project_id>/ai-assistant/` → `GPTIntegrationEndpoint`,
  `workspaces/<slug>/ai-assistant/` → `WorkspaceGPTIntegrationEndpoint`.
  (Same file also exposes `unsplash/`.)
- Implementation — `/www/Github/plane/apps/api/plane/app/views/external/base.py`:
  `LLMProvider` base (L26-39) with `OpenAIProvider` (L42-45, models gpt-3.5-turbo … o1-preview,
  default `gpt-4o-mini`), `AnthropicProvider` (L48-60, default `claude-3-sonnet-20240229`),
  `GeminiProvider` (L63-66); registry L70+; `get_llm_config()` L76-120 reads key/provider/model
  from instance configuration with `LLM_PROVIDER` env default (L89) and validates the model against
  the provider's list (L111-118); `get_llm_response(task, prompt, api_key, model, provider)`
  L123-135 concatenates `task + "\n" + prompt` into a single user message (litellm-style call).
  `GPTIntegrationEndpoint.post` L148+.
- Client: `apps/web/core/services/ai.service.ts` L30 and
  `packages/services/src/ai/ai.service.ts` L48 — both `POST …/ai-assistant/`.
- UI: `apps/web/core/components/core/modals/gpt-assistant-popover.tsx` (a popover over the rich-text
  editor: prompt → response preview (`#ai-assistant-response`, L244) → insert/replace).
- **"Pi Chat"** is a *link out*, not code: sidebar item `key: "pi-chat"` →
  `/${workspaceSlug}/pi-chat/` (`apps/web/core/components/workspace/sidebar/user-menu.tsx` L55-61);
  the icon lives in `packages/propel/src/icons/sub-brand/pi-chat.tsx`. The actual Pi product is
  closed-source. There is **no agent/tool-calling/MCP code in this repo.**

Takeaway for git-in-track: the only genuinely reusable idea is the **provider-abstraction +
instance-configured key** (`LLMProvider` subclasses with an allowed-model list and a default),
and the **inline "ask AI about this text" popover** anchored to the editor selection — which maps
neatly onto git-in-track's existing feedback-on-selection mode.

---

## 6. UX patterns worth borrowing

1. **Two-pane triage with keyboard-driven "next item" flow.** The intake header computes the next
   or previous item *before* mutating, then navigates after the action resolves
   (`inbox-issue-header.tsx` L126-160, `redirectIssue()` / `handleRedirection()`). Accept/decline
   never leaves you staring at an empty pane. Very worth copying for an inbox.
2. **Accept = open the full create/edit form, retitled.** Rather than a bespoke "accept" dialog,
   Plane reuses `CreateUpdateIssueModal` with `modalTitle: "Move PROJ-123"` and
   `primaryButtonText: "Add to project"` (`inbox-issue-header.tsx` L256-271). One form, two contexts.
3. **Optimistic mutate + rollback + badge arithmetic.** `inbox-issue.store.ts` L99-142 shows the
   full pattern: snapshot previous value, set optimistically, PATCH, reconcile derived counters
   (`intake_count`, `total_results`), restore on `catch`.
4. **Derived status everywhere, stored status nowhere.** Cycle status is a SQL `Case` on the server
   and a pure function on the client (`packages/utils/src/cycle.ts` L21-42). Filters operate on the
   derived value (`shouldFilterCycle` L50-71).
5. **Filter architecture**: `filters/` (selection UI) + `applied-filters/` (removable chips) +
   a `*_filter.store.ts`, with each filter dimension its own small file
   (`core/components/inbox/inbox-filter/`, `core/components/cycles/applied-filters/`). Cheap to
   extend, and the "chips row" makes state legible.
6. **Comboboxes/autosuggest**: `packages/propel/src/combobox/combobox.tsx` (+ `.stories.tsx`) is the
   primitive; `packages/propel/src/command/` is the cmd-k palette; the app-level pickers are
   `apps/web/core/components/dropdowns/{project,cycle,module,member,state,priority,estimate,date,intake-state}/`
   — each is a *button that renders the current value* and opens a searchable list, not a `<select>`.
   `modals/select-duplicate.tsx` is the issue-search variant (search-as-you-type against the
   workspace search endpoint) — that is the shape git-in-track wants for "mark duplicate" and for
   parent/epic pickers.
7. **Toasts** — `packages/propel/src/toast/toast.tsx`, used as
   `setToast({type: TOAST_TYPE.SUCCESS|ERROR, title, message})`
   (`transfer-issues-modal.tsx` L47-60). Every mutation reports both outcomes.
8. **Inline warning banner as the gate for a destructive bulk action**: `transfer-issues.tsx` L18-42
   — "Completed cycles are not editable" + a single primary CTA. Better than hiding the action.
9. **Bulk operations are behind the paywall in OSS** — `core/components/issues/bulk-operations/root.tsx`
   only renders an upgrade `Banner`. But the *selection* machinery is open and worth studying:
   `core/hooks/use-multiple-select.ts` + `core/hooks/store/use-multiple-select-store.ts`
   (shift-click ranges, `isSelectionActive`, `selectionHelpers` passed down to rows), plus
   `core/components/core/modals/bulk-delete-issues-modal.tsx` as a worked example.
10. **Cursor pagination in the store**, not page numbers: `next_cursor` of the form
    `"<per_page>:<page>:<offset>"` (`project-inbox.store.ts` L382-396) — fits an append-on-scroll queue.
11. **Empty states as first-class assets** — `apps/web/app/assets/empty-state/{cycle,cycle-issues,active-cycle}`
    and `packages/propel/src/empty-state/`.
12. **Per-user view properties persisted server-side** (`CycleUserProperties`,
    `IntakeIssue` `view_props`) so filters/layout follow the user across devices.

---

## Verified path index

- `apps/api/plane/db/models/intake.py`, `cycle.py`, `importer.py`, `exporter.py`, `project.py`
- `apps/api/plane/app/views/intake/base.py` (640 L), `apps/api/plane/api/views/intake.py` (499 L),
  `apps/api/plane/space/views/intake.py` (294 L)
- `apps/api/plane/app/views/cycle/{base.py (1049 L), archive.py (611 L), issue.py (344 L)}`,
  `apps/api/plane/api/views/cycle.py` (1266 L)
- `apps/api/plane/utils/cycle_transfer_issues.py` (478 L), `apps/api/plane/utils/analytics_plot.py`
- `apps/api/plane/app/urls/{intake,cycle,external}.py`, `apps/api/plane/api/urls/intake.py`,
  `apps/api/plane/space/urls/intake.py`
- `apps/api/plane/app/views/external/base.py`
- `apps/api/plane/bgtasks/{issue_activities_task,issue_automation_task,export_task,webhook_task}.py`
- `apps/web/core/components/inbox/**`, `apps/web/core/components/cycles/**`
- `apps/web/core/store/inbox/{inbox-issue,project-inbox}.store.ts`,
  `apps/web/core/store/{cycle,cycle_filter}.store.ts`
- `packages/types/src/cycle/cycle.ts`, `packages/types/src/importer/*.ts`,
  `packages/constants/src/{intake,cycle,workspace}.ts`, `packages/utils/src/cycle.ts`,
  `packages/services/src/cycle/*.ts`, `packages/propel/src/{combobox,command,toast,empty-state}/`
