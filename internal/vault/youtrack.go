package vault

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// This file is the import half of the YouTrack integration: the two core API
// methods "youtrack.import.preview" and "youtrack.import.run"
// (story GIT-US-0047).
//
// Every surface — REST, MCP, the CLI, the web app and the background sync job —
// calls these two, so the idempotence rule, the validation and the git writes
// have exactly one implementation. What lives here is plumbing: resolve the
// issues through the client, hand every translation to
// internal/youtrack/mapping, resolve the identifiers mapping deliberately did
// not resolve, and write through the ordinary FileStore path so the batch lands
// as normal commits, index updates and WebSocket events.
//
// Two rules shape the code:
//
//  1. The vault mutex is never held across a network call. Resolution runs
//     unlocked and produces a plan; only the write phase takes the lock. That
//     is why Dispatch answers these two methods before it locks.
//
//  2. The pair (external.system, external.id) decides create versus update.
//     An update keeps the git-in-track id the item already has, so re-importing
//     the same issue can never produce a second item.

// The two actions an import plan can take for one issue.
const (
	// YouTrackImportCreate allocates a new item for an issue nothing claims.
	YouTrackImportCreate = "create"
	// YouTrackImportUpdate patches the item that already carries the issue's
	// external reference, keeping its id.
	YouTrackImportUpdate = "update"
)

// YouTrackMaxDepth is the deepest subtask recursion an import accepts. Depth 0
// imports the selected issues only.
const YouTrackMaxDepth = 5

// YouTrackMaxIssues bounds one import. A query that selects more is truncated
// with a warning rather than turned into an unbounded walk of a tracker.
const YouTrackMaxIssues = 500

// YouTrackSource is the part of the YouTrack REST client an import reads. It is
// an interface rather than *youtrack.Client so that the transport stays in
// internal/youtrack, this package stays testable with a fake, and a host that
// has no client at all (the browser) simply installs none.
type YouTrackSource interface {
	// Issue reads one issue with the wide field selector the mapping needs.
	Issue(ctx context.Context, id string) (youtrack.Issue, error)
	// Article reads one knowledge-base article, which is what a KB status call
	// compares a page against when it is asked to look at the remote side.
	Article(ctx context.Context, id string) (youtrack.Article, error)
	// IssueLinks reads the link graph of one issue, for an instance whose issue
	// payload came back without it.
	IssueLinks(ctx context.Context, id string) ([]youtrack.IssueLink, error)
	// SearchAllIssues walks every page of an issue query.
	SearchAllIssues(ctx context.Context, query string, page youtrack.Page) ([]youtrack.Issue, error)
	// AllComments walks every page of one issue's comments.
	AllComments(ctx context.Context, id string) ([]youtrack.Comment, error)
	// Attachments lists the files attached to one issue.
	Attachments(ctx context.Context, id string) ([]youtrack.Attachment, error)
}

// YouTrackLink is the part of a project's `integrations.youtrack` block the
// import needs. It is a plain struct rather than config.YouTrackLink because
// internal/vault compiles to WebAssembly and must not depend on the file-system
// half of the configuration; the host translates one into the other.
type YouTrackLink struct {
	// BaseURL is the instance URL, context path included, with no trailing
	// slash. It only builds the `url` of an external reference.
	BaseURL string
	// Project is the YouTrack project short name, the "ACME" of ACME-42. It
	// scopes a query that does not scope itself.
	Project string
	// FieldMap overrides the default field names *and* the default value
	// translations, keyed as config.FieldMapKeys spells them.
	//
	// It carries mapping.FieldSpec rather than a flat field name because a
	// project configures both halves of the block — `{"status": {"field":
	// "State", "values": {"In Progress": "in_progress"}}}` — and a flat map
	// would let the names cross into the importer while every value map the
	// user configured stayed behind (GIT-T-0131).
	FieldMap map[string]mapping.FieldSpec
	// PushComments is the project's `integrations.youtrack.push_comments`
	// setting: YouTrackPushAuto queues every new comment for the tracker,
	// anything else (YouTrackPushManual, and the empty string a host that does
	// not set it leaves) queues nothing and leaves the manual action as the
	// only trigger.
	PushComments string
}

// The two values of `integrations.youtrack.push_comments`.
const (
	// YouTrackPushManual is the default: a comment reaches YouTrack only when
	// somebody asks for it.
	YouTrackPushManual = "manual"
	// YouTrackPushAuto queues a push for every comment written on a linked
	// item, on every surface.
	YouTrackPushAuto = "auto"
)

// The background job kinds this package asks the host to run. They are named
// after the core method that creates them, so a job in the queue can be read
// back to the call that made it.
const (
	// JobKindCommentPush pushes one comment file to the issue its item mirrors.
	JobKindCommentPush = "youtrack.comment.push"
	// JobKindKBPublish publishes one page, or one subtree, as articles.
	JobKindKBPublish = "youtrack.kb.publish"
	// JobKindKBPull writes one page, or one subtree, back from its articles.
	JobKindKBPull = "youtrack.kb.pull"
)

// YouTrackJob is one unit of work the vault hands to the host's background
// engine instead of doing inline.
//
// Every outbound write to YouTrack is queued rather than performed in the call
// that triggered it: a comment saved in the web app must not wait on a remote
// tracker, and an HTTP response that ends must not cancel the push it started.
// Key is the coalescing key — the engine folds two queued jobs that share a
// kind and a key into one — so a burst of edits on the same comment or the
// same page produces exactly one push.
type YouTrackJob struct {
	// Kind is one of the JobKind constants above.
	Kind string `json:"kind"`
	// Key is the coalescing key: the comment path or the page path.
	Key string `json:"key"`
	// Project is the git-in-track project key the job runs against.
	Project string `json:"project,omitempty"`
	// Payload is the job's own arguments, encoded by the host.
	Payload any `json:"payload,omitempty"`
}

// YouTrackEnqueuer hands a job to the host's background engine and returns the
// id it was given. The vault never runs a job itself: it only decides that one
// is needed, which is what keeps every network call out of the write path and
// out of internal/core.
//
// A host that has no engine installs none, and the queueing methods then fail
// with `unavailable` rather than pretending to have queued something.
type YouTrackEnqueuer func(ctx context.Context, job YouTrackJob) (string, error)

// SetYouTrackEnqueuer installs the background engine seam. Passing nil removes
// it, which is what a browser-only session leaves in place.
func (v *Vault) SetYouTrackEnqueuer(e YouTrackEnqueuer) {
	v.seams.Lock()
	defer v.seams.Unlock()
	v.enqueue = e
}

// youtrackEnqueuer returns the installed enqueuer, nil when there is none.
func (v *Vault) youtrackEnqueuer() YouTrackEnqueuer {
	v.seams.Lock()
	defer v.seams.Unlock()
	return v.enqueue
}

// YouTrackProvider hands the import the client and the link configuration of
// one project. The host owns both: the companion process holds the token and
// the parsed project.yaml, and the vault never reads either.
//
// The project key is the git-in-track one, empty when the vault holds a single
// project. A project that is not linked is an error, not an empty link.
type YouTrackProvider func(ctx context.Context, project string) (YouTrackSource, YouTrackLink, error)

// SetYouTrackProvider installs the provider the import resolves its client
// through. Passing nil removes it, which is what a browser-only session leaves
// in place: "youtrack.import.*" then fails with `unavailable` instead of
// pretending to have a tracker.
func (v *Vault) SetYouTrackProvider(p YouTrackProvider) {
	v.seams.Lock()
	defer v.seams.Unlock()
	v.youtrack = p
}

// youtrackProvider returns the installed provider, nil when there is none.
//
// The seams have a mutex of their own rather than sharing the vault mutex, so
// that a method already holding the vault lock can still ask whether a host
// installed them. Installing a seam is a host lifecycle event; it never races
// with a call in flight in a way the vault lock would have to arbitrate.
func (v *Vault) youtrackProvider() YouTrackProvider {
	v.seams.Lock()
	defer v.seams.Unlock()
	return v.youtrack
}

// YouTrackImportParams is the input of both import methods.
type YouTrackImportParams struct {
	// Project is the git-in-track project key the issues are imported into. It
	// may be empty when the vault holds exactly one project.
	Project string `json:"project,omitempty"`
	// Query is a YouTrack issue query. Exactly one of Query and IDs is given.
	Query string `json:"query,omitempty"`
	// IDs are readable issue ids such as "ACME-42".
	IDs []string `json:"ids,omitempty"`
	// Depth bounds the subtask recursion: 0 imports the selected issues only,
	// 1 their children, and so on up to YouTrackMaxDepth.
	Depth int `json:"depth,omitempty"`
	// IncludeLinks imports the non-hierarchy relations as `links[]`.
	IncludeLinks bool `json:"includeLinks,omitempty"`
	// IncludeComments writes the issue's comment thread as comment files.
	IncludeComments bool `json:"includeComments,omitempty"`
	// IncludeAttachments records the attachment paths on the item. The binaries
	// themselves are downloaded by the sync engine, not by this call.
	IncludeAttachments bool `json:"includeAttachments,omitempty"`
}

// validate checks the parameters and names the offending field in every
// message, so that a form can mark the input that was wrong.
func (p YouTrackImportParams) validate() error {
	query := strings.TrimSpace(p.Query)
	ids := trimmedIDs(p.IDs)
	switch {
	case query == "" && len(ids) == 0:
		return failf("invalid_request",
			`"query" or "ids": name the issues to import, by query or by readable id`)
	case query != "" && len(ids) > 0:
		return failf("invalid_request",
			`"query" and "ids": give one or the other, not both`)
	}
	if p.Depth < 0 || p.Depth > YouTrackMaxDepth {
		return failf("invalid_request",
			`"depth": %d is out of range: 0 imports the selected issues only, %d is the deepest recursion`,
			p.Depth, YouTrackMaxDepth)
	}
	return nil
}

// trimmedIDs drops the blank entries of an id list.
func trimmedIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, id := range raw {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// YouTrackImportPlanItem is what "youtrack.import.preview" says about one
// issue: what it would become and what it could not resolve.
type YouTrackImportPlanItem struct {
	YouTrackID string        `json:"youtrackId"`
	Title      string        `json:"title"`
	MappedType core.ItemType `json:"mappedType"`
	// Action is YouTrackImportCreate or YouTrackImportUpdate.
	Action string `json:"action"`
	// TargetID is the git-in-track item an update would patch, empty for a
	// create.
	TargetID string `json:"targetId,omitempty"`
	// Parent and Milestone are git-in-track ids once they resolve. A parent
	// that is inside this import but not written yet is reported as the
	// YouTrack id it still is, and one outside the import set is left empty
	// with a warning.
	Parent    string `json:"parent,omitempty"`
	Milestone string `json:"milestone,omitempty"`
	// Depth is how far down the subtask recursion the issue was found.
	Depth int `json:"depth"`
	// Comments is how many comments would be written, already excluding the
	// ones a previous import wrote.
	Comments int               `json:"comments"`
	Warnings []mapping.Warning `json:"warnings,omitempty"`
}

// YouTrackImportPreview is the answer of "youtrack.import.preview": the plan,
// and nothing written.
type YouTrackImportPreview struct {
	Project string                   `json:"project"`
	Issues  []YouTrackImportPlanItem `json:"issues"`
	// Warnings are the findings about the import as a whole rather than about
	// one issue, such as a truncated selection.
	Warnings []mapping.Warning `json:"warnings,omitempty"`
}

// YouTrackImportIssueResult is what one issue of a run produced. A failure is
// recorded here and never aborts the batch: the other issues still land.
type YouTrackImportIssueResult struct {
	YouTrackID string `json:"youtrackId"`
	ItemID     string `json:"itemId,omitempty"`
	Action     string `json:"action"`
	// Comments counts the comment files written for this issue.
	Comments int               `json:"comments"`
	Warnings []mapping.Warning `json:"warnings,omitempty"`
	// Error is the failure message, empty when the issue landed.
	Error string `json:"error,omitempty"`
}

// YouTrackImportResult is the answer of "youtrack.import.run".
type YouTrackImportResult struct {
	Project  string                      `json:"project"`
	Issues   []YouTrackImportIssueResult `json:"issues"`
	Created  int                         `json:"created"`
	Updated  int                         `json:"updated"`
	Failed   int                         `json:"failed"`
	Warnings []mapping.Warning           `json:"warnings,omitempty"`
	// Writes is the one WriteSet of the whole batch, which is what the host
	// commits and what the browser persists.
	Writes WriteSet `json:"writes"`
}

// youtrackDispatch answers the two import methods. Dispatch routes them here
// before it takes the vault mutex, because resolving an import talks to a
// remote tracker and a lock held across the network would stall every read of
// the vault for as long as YouTrack takes to answer.
func (v *Vault) youtrackDispatch(ctx context.Context, method string, raw []byte) (any, error) {
	p, err := decodeParams[YouTrackImportParams](raw)
	if err != nil {
		return nil, err
	}
	if method == "youtrack.import.preview" {
		return v.YouTrackImportPreview(ctx, p)
	}
	return v.YouTrackImportRun(ctx, p)
}

// YouTrackImportPreview resolves an import and returns what it would do,
// writing nothing. It shares every step with YouTrackImportRun up to the
// writes, so the two can never disagree about what an issue becomes.
func (v *Vault) YouTrackImportPreview(
	ctx context.Context, p YouTrackImportParams,
) (YouTrackImportPreview, error) {
	plan, err := v.youtrackResolve(ctx, p)
	if err != nil {
		return YouTrackImportPreview{}, err
	}
	out := YouTrackImportPreview{
		Project:  plan.project,
		Issues:   make([]YouTrackImportPlanItem, 0, len(plan.issues)),
		Warnings: plan.warnings,
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	targets := v.resolveTargets(plan)
	for _, entry := range plan.issues {
		decision := v.youtrackDecide(entry, plan, targets)
		out.Issues = append(out.Issues, YouTrackImportPlanItem{
			YouTrackID: entry.id(),
			Title:      decision.title,
			MappedType: decision.itemType,
			Action:     decision.action,
			TargetID:   string(decision.target),
			Parent:     decision.parent,
			Milestone:  string(decision.milestone),
			Depth:      entry.depth,
			Comments:   v.youtrackPendingComments(ctx, decision.target, entry, plan),
			Warnings:   decision.warnings,
		})
	}
	return out, nil
}

// YouTrackImportRun resolves an import and writes it. Every issue is written
// on its own, so one that fails validation is reported and the rest still
// land, and the whole batch is reported as one WriteSet.
func (v *Vault) YouTrackImportRun(
	ctx context.Context, p YouTrackImportParams,
) (YouTrackImportResult, error) {
	plan, err := v.youtrackResolve(ctx, p)
	if err != nil {
		return YouTrackImportResult{}, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	store, err := v.storeFor(core.ProjectKey(plan.project))
	if err != nil {
		return YouTrackImportResult{}, err
	}
	out := YouTrackImportResult{
		Project:  plan.project,
		Issues:   make([]YouTrackImportIssueResult, 0, len(plan.issues)),
		Warnings: plan.warnings,
	}

	v.fs.begin()
	// Every id is decided before anything is written, so that a parent or a
	// link pointing at another issue of the same batch resolves to the id that
	// issue is about to get instead of being reported as dangling. An id
	// allocated for an issue that then fails to write leaves a gap in the
	// counter, which the id rules allow and expect.
	targets := v.resolveTargets(plan)
	refused := map[string]error{}
	for _, entry := range plan.issues {
		id := entry.id()
		if id == "" || targets.exists(id) {
			continue
		}
		decision := v.youtrackDecide(entry, plan, targets)
		allocated, allocErr := store.Allocator().Next(ctx, decision.itemType)
		if allocErr != nil {
			refused[id] = allocErr
			continue
		}
		targets.byIssue[id] = allocated
	}

	for _, entry := range plan.issues {
		if allocErr, bad := refused[entry.id()]; bad {
			out.Issues = append(out.Issues, YouTrackImportIssueResult{
				YouTrackID: entry.id(), Action: YouTrackImportCreate, Error: allocErr.Error(),
			})
			out.Failed++
			continue
		}
		decision := v.youtrackDecide(entry, plan, targets)
		result, writeErr := v.youtrackWrite(ctx, store, entry, decision, plan)
		if writeErr != nil {
			out.Issues = append(out.Issues, youtrackFailure(entry, decision, writeErr))
			out.Failed++
			continue
		}
		switch result.Action {
		case YouTrackImportCreate:
			out.Created++
		default:
			out.Updated++
		}
		out.Issues = append(out.Issues, result)
	}

	for _, missing := range plan.unreadable {
		out.Issues = append(out.Issues, YouTrackImportIssueResult{
			YouTrackID: missing.id, Error: missing.reason,
		})
		out.Failed++
	}

	writes, err := v.commit(ctx)
	if err != nil {
		return YouTrackImportResult{}, err
	}
	out.Writes = writes
	return out, nil
}

// youtrackFailure records an issue that could not be written.
func youtrackFailure(
	entry youtrackEntry, decision youtrackDecision, err error,
) YouTrackImportIssueResult {
	message := err.Error()
	if classified, ok := AsError(err); ok {
		message = classified.Message
	}
	return YouTrackImportIssueResult{
		YouTrackID: entry.id(),
		ItemID:     string(decision.target),
		Action:     decision.action,
		Warnings:   decision.warnings,
		Error:      message,
	}
}

// ------------------------------------------------------------ resolution ----

// youtrackEntry is one issue of an import with everything the remote side had
// to say about it.
type youtrackEntry struct {
	issue youtrack.Issue
	// depth is how far down the subtask recursion the issue was reached: 0 for
	// a selected issue, 1 for a child of one, and so on.
	depth       int
	comments    []youtrack.Comment
	attachments []youtrack.Attachment
}

// id is the readable YouTrack id, which is the identity an import is keyed on.
func (e youtrackEntry) id() string { return strings.TrimSpace(e.issue.IDReadable) }

// youtrackPlan is everything a resolution produced: it is pure data, so the
// write phase needs no network and the preview needs no writes.
type youtrackPlan struct {
	project  string
	params   YouTrackImportParams
	link     YouTrackLink
	fieldMap mapping.FieldMap
	issues   []youtrackEntry
	// unreadable are the issues the tracker would not hand over. They are
	// reported per issue by a run and as warnings by a preview, because one
	// unreachable issue must not cost the whole batch.
	unreadable []youtrackUnreadable
	warnings   []mapping.Warning
}

// youtrackUnreadable is one issue the tracker refused or does not have.
type youtrackUnreadable struct {
	id     string
	reason string
}

// options is what the mapping package needs for one item of this plan.
func (p youtrackPlan) options(itemID core.ItemID, now core.Timestamp) mapping.Options {
	return mapping.Options{
		BaseURL:  p.link.BaseURL,
		FieldMap: p.fieldMap,
		ItemID:   string(itemID),
		SyncedAt: now,
	}
}

// youtrackResolve reads everything the import needs from the tracker and takes
// no lock: it is the half of an import that talks to the network.
func (v *Vault) youtrackResolve(
	ctx context.Context, p YouTrackImportParams,
) (youtrackPlan, error) {
	if err := p.validate(); err != nil {
		return youtrackPlan{}, err
	}
	provider := v.youtrackProvider()
	if provider == nil {
		return youtrackPlan{}, failf("unavailable",
			"this host has no YouTrack client: an import runs where the credentials are, "+
				"which is the companion process")
	}
	source, link, err := provider(ctx, p.Project)
	if err != nil {
		return youtrackPlan{}, err
	}
	if source == nil {
		return youtrackPlan{}, failf("unavailable",
			"project %q has no YouTrack client installed", p.Project)
	}

	plan := youtrackPlan{
		project:  p.Project,
		params:   p,
		link:     link,
		fieldMap: mapping.DefaultFieldMap().WithFields(link.FieldMap),
	}

	seeds, unreadable, warnings, err := youtrackSeeds(ctx, source, p, link)
	plan.unreadable = append(plan.unreadable, unreadable...)
	plan.warnings = append(plan.warnings, warnings...)
	if err != nil {
		return youtrackPlan{}, err
	}

	entries, expanded, expandWarnings := youtrackExpand(ctx, source, seeds, p.Depth)
	plan.unreadable = append(plan.unreadable, expanded...)
	plan.warnings = append(plan.warnings, expandWarnings...)
	plan.issues = entries
	for _, missing := range plan.unreadable {
		plan.warnings = append(plan.warnings, mapping.Warning{
			Field: "issues", Value: missing.id, Reason: missing.reason,
		})
	}

	for i := range plan.issues {
		id := plan.issues[i].id()
		if p.IncludeComments {
			comments, commentErr := source.AllComments(ctx, id)
			if commentErr != nil {
				return youtrackPlan{}, fmt.Errorf("read the comments of %s: %w", id, commentErr)
			}
			plan.issues[i].comments = comments
		}
		if p.IncludeAttachments {
			attachments, attachErr := source.Attachments(ctx, id)
			if attachErr != nil {
				return youtrackPlan{}, fmt.Errorf("read the attachments of %s: %w", id, attachErr)
			}
			plan.issues[i].attachments = attachments
		}
	}
	return plan, nil
}

// youtrackSeeds reads the issues the caller selected, by id or by query. A
// query returns the narrow list shape, so every hit is read again with the wide
// selector the mapping needs.
func youtrackSeeds(
	ctx context.Context, source YouTrackSource, p YouTrackImportParams, link YouTrackLink,
) ([]youtrack.Issue, []youtrackUnreadable, []mapping.Warning, error) {
	ids := trimmedIDs(p.IDs)
	var (
		warnings   []mapping.Warning
		unreadable []youtrackUnreadable
	)
	if len(ids) == 0 {
		found, err := source.SearchAllIssues(ctx, scopeYouTrackQuery(p.Query, link.Project), youtrack.Page{})
		if err != nil {
			return nil, nil, warnings, fmt.Errorf("search issues: %w", err)
		}
		for _, issue := range found {
			ids = append(ids, strings.TrimSpace(issue.IDReadable))
		}
		ids = trimmedIDs(ids)
	}
	if len(ids) > YouTrackMaxIssues {
		warnings = append(warnings, mapping.Warning{
			Field: "issues",
			Reason: fmt.Sprintf(
				"the selection holds %d issues and an import is bounded at %d; the rest were left out",
				len(ids), YouTrackMaxIssues),
		})
		ids = ids[:YouTrackMaxIssues]
	}

	seen := map[string]bool{}
	out := make([]youtrack.Issue, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		issue, err := source.Issue(ctx, id)
		if err != nil {
			// A single unreachable issue is reported and skipped: the rest of
			// the selection still imports.
			unreadable = append(unreadable, youtrackUnreadable{
				id: id, reason: fmt.Sprintf("the issue could not be read: %v", err),
			})
			continue
		}
		out = append(out, issue)
	}
	return out, unreadable, warnings, nil
}

// scopeYouTrackQuery scopes a query to the linked YouTrack project unless the
// caller already scoped it, so that a bare "State: Open" cannot drag another
// project's backlog into this repository.
func scopeYouTrackQuery(query, project string) string {
	trimmed := strings.TrimSpace(query)
	if project == "" || strings.Contains(strings.ToLower(trimmed), "project:") {
		return trimmed
	}
	return "project: " + project + " " + trimmed
}

// youtrackExpand walks the subtask graph outward up to depth levels. It is
// cycle-safe: an issue is visited once, keyed on its readable id, so a subtask
// graph that loops terminates and says so.
func youtrackExpand(
	ctx context.Context, source YouTrackSource, seeds []youtrack.Issue, depth int,
) ([]youtrackEntry, []youtrackUnreadable, []mapping.Warning) {
	var (
		out        []youtrackEntry
		unreadable []youtrackUnreadable
		warnings   []mapping.Warning
		visited    = map[string]bool{}
		queue      = make([]youtrackEntry, 0, len(seeds))
	)
	for _, issue := range seeds {
		queue = append(queue, youtrackEntry{issue: issue})
	}

	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]
		id := entry.id()
		if id == "" {
			warnings = append(warnings, mapping.Warning{
				Field:  "external",
				Reason: "an issue came back without a readable id and was skipped",
			})
			continue
		}
		if visited[id] {
			// Either the same issue was selected twice or the subtask graph
			// loops back onto itself. Both end here, which is what makes the
			// recursion terminate.
			continue
		}
		visited[id] = true

		if links, err := youtrackLinksOf(ctx, source, entry.issue); err != nil {
			warnings = append(warnings, mapping.Warning{
				Field: "links", Value: id,
				Reason: fmt.Sprintf("the link graph could not be read (%v), so the issue "+
					"was imported without its relations", err),
			})
		} else {
			entry.issue.Links = links
		}
		out = append(out, entry)

		if entry.depth >= depth || len(out) >= YouTrackMaxIssues {
			continue
		}
		_, children, _, _ := mapping.MapLinks(entry.issue)
		for _, child := range children {
			child = strings.TrimSpace(child)
			if child == "" || visited[child] {
				continue
			}
			issue, childErr := source.Issue(ctx, child)
			if childErr != nil {
				unreadable = append(unreadable, youtrackUnreadable{
					id: child, reason: fmt.Sprintf("the subtask could not be read: %v", childErr),
				})
				continue
			}
			queue = append(queue, youtrackEntry{issue: issue, depth: entry.depth + 1})
		}
	}
	if len(out) >= YouTrackMaxIssues {
		warnings = append(warnings, mapping.Warning{
			Field: "issues",
			Reason: fmt.Sprintf("the recursion reached the bound of %d issues and stopped there",
				YouTrackMaxIssues),
		})
	}
	return out, unreadable, warnings
}

// youtrackLinksOf returns the link graph of an issue, reading the dedicated
// sub-resource only for an instance whose issue payload carried none.
func youtrackLinksOf(
	ctx context.Context, source YouTrackSource, issue youtrack.Issue,
) ([]youtrack.IssueLink, error) {
	if len(youtrack.NonEmptyLinks(issue.Links)) > 0 {
		return issue.Links, nil
	}
	links, err := source.IssueLinks(ctx, strings.TrimSpace(issue.IDReadable))
	if err != nil {
		return nil, fmt.Errorf("read the links of %s: %w", issue.IDReadable, err)
	}
	return links, nil
}

// -------------------------------------------------------------- decisions ---

// youtrackDecision is what one issue becomes, with every identifier already
// resolved. It is produced identically by the preview and by the run.
type youtrackDecision struct {
	action    string
	target    core.ItemID
	itemType  core.ItemType
	title     string
	draft     core.ItemDraft
	patch     core.ItemPatch
	status    core.Status
	parent    string
	milestone core.ItemID
	warnings  []mapping.Warning
}

// youtrackTargets is the item every issue of an import maps to, and whether
// that item already exists. The two are not the same thing: a create is given
// its id before anything is written, so that a parent or a link pointing at
// another issue of the same batch resolves, and that id must not then look like
// an item the index already holds.
type youtrackTargets struct {
	byIssue  map[string]core.ItemID
	existing map[string]bool
}

// id returns the item an issue maps to, empty when it has none yet.
func (t youtrackTargets) id(issue string) core.ItemID { return t.byIssue[issue] }

// exists reports an issue that (system, id) already resolves to an item, which
// is what makes an import an update rather than a create.
func (t youtrackTargets) exists(issue string) bool { return t.existing[issue] }

// resolveTargets looks every issue of the plan up by (external.system,
// external.id). The caller holds the vault lock.
func (v *Vault) resolveTargets(plan youtrackPlan) youtrackTargets {
	out := youtrackTargets{
		byIssue:  make(map[string]core.ItemID, len(plan.issues)),
		existing: make(map[string]bool, len(plan.issues)),
	}
	for _, entry := range plan.issues {
		id := entry.id()
		if id == "" {
			continue
		}
		if item, ok := v.index.ItemByExternal(mapping.System, id); ok {
			out.byIssue[id] = item.ID
			out.existing[id] = true
		}
	}
	return out
}

// youtrackDecide maps one issue and resolves everything the mapping package
// deliberately left as a YouTrack identifier. The caller holds the vault lock.
func (v *Vault) youtrackDecide(
	entry youtrackEntry, plan youtrackPlan, targets youtrackTargets,
) youtrackDecision {
	id := entry.id()
	target, exists := targets.id(id), targets.exists(id)
	// An item found by (system, id) is updated in place and keeps the
	// git-in-track id it already has; anything else is a create.
	action := YouTrackImportCreate
	if exists {
		action = YouTrackImportUpdate
	}
	opts := plan.options(target, core.NewTimestamp(v.now()))

	decision := youtrackDecision{action: action, target: target}
	var relations mapping.Relations
	if action == YouTrackImportUpdate {
		patch, rel, warnings := mapping.IssueToPatch(entry.issue, opts)
		decision.patch, relations, decision.warnings = patch, rel, warnings
		if patch.Title != nil {
			decision.title = *patch.Title
		}
		if patch.Status != nil {
			decision.status = *patch.Status
		}
		// A patch carries no type: the type of an existing item is its own.
		if item, lookupErr := v.index.Item(target); lookupErr == nil && item != nil {
			decision.itemType = item.Type
			if decision.title == "" {
				decision.title = item.Title
			}
		}
	} else {
		draft, rel, warnings := mapping.IssueToDraft(entry.issue, opts)
		decision.draft, relations, decision.warnings = draft, rel, warnings
		decision.itemType, decision.title, decision.status = draft.Type, draft.Title, draft.Status
	}

	decision.warnings = append(decision.warnings, v.youtrackResolveRelations(
		&decision, entry, plan, relations, targets)...)
	decision.warnings = append(decision.warnings, youtrackAttachments(&decision, entry, plan)...)
	return decision
}

// youtrackResolveRelations turns the YouTrack identifiers of a mapped issue
// into git-in-track ids and writes them onto the draft or the patch. Anything
// that does not resolve becomes a warning: an import never writes a reference
// to an item that does not exist (R-EXT-4).
func (v *Vault) youtrackResolveRelations(
	decision *youtrackDecision, entry youtrackEntry, plan youtrackPlan,
	relations mapping.Relations, targets youtrackTargets,
) []mapping.Warning {
	var warnings []mapping.Warning

	if parent := strings.TrimSpace(relations.Parent); parent != "" {
		if id := targets.id(parent); id != "" {
			decision.parent = string(id)
			if decision.action == YouTrackImportCreate {
				decision.draft.Parent = id
			} else {
				decision.patch.Parent = &id
			}
		} else if youtrackInSet(parent, plan) {
			// The parent belongs to this import but has not been allocated an
			// id yet; the second pass of the run writes it.
			decision.parent = parent
		} else {
			warnings = append(warnings, mapping.Warning{
				Field: "parent", Value: parent,
				Reason: "the parent issue is outside the imported set and no item carries it, " +
					"so the item was written without a parent",
			})
		}
	}

	if version := strings.TrimSpace(relations.Milestone); version != "" {
		if item, ok := v.index.ItemByExternal(mapping.System, version); ok && item.Type == core.TypeMilestone {
			decision.milestone = item.ID
			if decision.action == YouTrackImportCreate {
				decision.draft.Milestone = item.ID
			} else {
				milestone := item.ID
				decision.patch.Milestone = &milestone
			}
		} else {
			warnings = append(warnings, mapping.Warning{
				Field: "milestone", Value: version,
				Reason: "no milestone carries this YouTrack version, so the item was written " +
					"without a milestone",
			})
		}
	}

	if !plan.params.IncludeLinks {
		return warnings
	}
	resolved := make([]core.Link, 0, len(relations.Links))
	for _, link := range relations.Links {
		id := targets.id(strings.TrimSpace(link.Target))
		if id == "" {
			warnings = append(warnings, mapping.Warning{
				Field: "links", Value: link.Target,
				Reason: "the link target is outside the imported set and no item carries it, " +
					"so the link was not written",
			})
			continue
		}
		resolved = append(resolved, core.Link{Kind: link.Kind, Target: string(id)})
	}
	sort.SliceStable(resolved, func(i, j int) bool {
		if resolved[i].Kind != resolved[j].Kind {
			return resolved[i].Kind < resolved[j].Kind
		}
		return resolved[i].Target < resolved[j].Target
	})
	if len(resolved) == 0 {
		return warnings
	}
	if decision.action == YouTrackImportCreate {
		decision.draft.Links = resolved
	} else {
		// AddLinks is a set operation, so a relation a person added on this
		// side survives a re-import.
		decision.patch.AddLinks = resolved
	}
	return warnings
}

// youtrackInSet reports whether a YouTrack id belongs to this import.
func youtrackInSet(id string, plan youtrackPlan) bool {
	for _, entry := range plan.issues {
		if entry.id() == id {
			return true
		}
	}
	return false
}

// youtrackAttachments records the attachments of an issue on the item.
//
// The entries are **bare file names**, which is what docs/03 §13.4 says
// `attachments[]` holds everywhere else in the product: the folder is
// `.pmngr/attachments/<ITEM-ID>/` by convention, derived from the item's own
// id, and repeating it inside every entry only creates a second place for it to
// be wrong (R-YT-7, R-ATT-4). The download job builds the same folder from the
// item id rather than from these entries, so the two cannot disagree.
//
// The binaries are not downloaded here: fetching them is the sync engine's
// work, and this call stays one synchronous batch of file writes. An item
// imported over MCP or over the CLI can therefore list a file that is not on
// disk yet (`W-ATT-MISSING`) until the job runs.
func youtrackAttachments(
	decision *youtrackDecision, entry youtrackEntry, plan youtrackPlan,
) []mapping.Warning {
	if !plan.params.IncludeAttachments || len(entry.attachments) == 0 {
		return nil
	}
	names := make([]string, 0, len(entry.attachments))
	for _, attachment := range entry.attachments {
		name := youtrackAttachmentName(attachment.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	if decision.action == YouTrackImportCreate {
		decision.draft.Attachments = names
	} else {
		decision.patch.AddAttachments = names
	}
	return []mapping.Warning{{
		Field: "attachments",
		Reason: fmt.Sprintf("%d attachments were recorded; the files themselves are "+
			"downloaded by the synchronization job, not by the import", len(names)),
	}}
}

// youtrackAttachmentName reduces the name a tracker reports to the plain file
// name an `attachments[]` entry may hold. A name carrying a separator, or
// naming a parent directory, would resolve outside the item's attachment folder
// once the entry is read back as a relative name, so only the base is kept —
// the same reduction the download job applies to the file it writes.
func youtrackAttachmentName(raw string) string {
	name := path.Base(strings.TrimSpace(strings.ReplaceAll(raw, `\`, "/")))
	switch name {
	case "", ".", "..", "/":
		return ""
	}
	return name
}

// ------------------------------------------------------------- the writes ---

// youtrackWrite writes one decided issue: the item first, then its comments.
// The caller holds the vault lock and has already called v.fs.begin.
func (v *Vault) youtrackWrite(
	ctx context.Context, store *core.FileStore,
	entry youtrackEntry, decision youtrackDecision, plan youtrackPlan,
) (YouTrackImportIssueResult, error) {
	out := YouTrackImportIssueResult{
		YouTrackID: entry.id(),
		Action:     decision.action,
		Warnings:   decision.warnings,
	}

	var item *core.Item
	var err error
	if decision.action == YouTrackImportCreate {
		draft := decision.draft
		draft.ID = decision.target
		item, err = store.Create(ctx, draft)
	} else {
		patch := decision.patch
		// The status is applied separately: the remote tracker is the authority
		// on the state of an issue it owns, so a transition this project's
		// workflow does not declare is forced rather than refused.
		patch.Status = nil
		item, err = store.Update(ctx, decision.target, patch, "")
		if err == nil && decision.status != "" && item.Status != decision.status {
			item, err = store.MoveWith(ctx, item.ID, decision.status, "", core.MoveOptions{Force: true})
		}
	}
	if err != nil {
		return out, fmt.Errorf("write %s as %s: %w", entry.id(), decision.target, err)
	}
	out.ItemID = string(item.ID)

	written, commentWarnings, err := v.youtrackComments(ctx, store, item.ID, entry, plan)
	out.Warnings = append(out.Warnings, commentWarnings...)
	if err != nil {
		return out, err
	}
	out.Comments = written
	return out, nil
}

// youtrackComments writes the comment thread of one issue, skipping every
// comment a previous import already wrote. The files land where ADR-012 puts
// them, with the original author and the original timestamp, because an
// imported thread must read as the conversation it was.
func (v *Vault) youtrackComments(
	ctx context.Context, store *core.FileStore, id core.ItemID,
	entry youtrackEntry, plan youtrackPlan,
) (int, []mapping.Warning, error) {
	if !plan.params.IncludeComments || len(entry.comments) == 0 {
		return 0, nil, nil
	}
	opts := plan.options(id, core.NewTimestamp(v.now()))
	drafts, warnings := mapping.CommentsToDrafts(entry.comments, entry.id(), opts)
	if len(drafts) == 0 {
		return 0, warnings, nil
	}
	already, err := youtrackCommentIDs(ctx, store, id)
	if err != nil {
		return 0, warnings, err
	}

	written := 0
	for _, draft := range drafts {
		if already[draft.External.ID] {
			continue
		}
		comment, addErr := store.AddComment(ctx, id, draft.Draft)
		if addErr != nil {
			return written, warnings, fmt.Errorf("write a comment of %s: %w", entry.id(), addErr)
		}
		// core.CommentDraft carries no external reference, so the file is
		// rewritten once with it: without that reference a re-import could not
		// tell this comment from a new one and would duplicate the thread.
		comment.External = []core.External{draft.External}
		comment.Updated = draft.Updated
		data, serErr := core.SerializeComment(comment)
		if serErr != nil {
			return written, warnings, fmt.Errorf("serialize a comment of %s: %w", entry.id(), serErr)
		}
		if writeErr := v.fs.WriteFile(comment.Path, data); writeErr != nil {
			return written, warnings, fmt.Errorf("write %s: %w", comment.Path, writeErr)
		}
		already[draft.External.ID] = true
		written++
	}
	return written, warnings, nil
}

// youtrackPendingComments counts the comments a run would write for one issue,
// which is what the preview reports. It never fails: an item whose thread
// cannot be read is reported as if none of it had been imported.
func (v *Vault) youtrackPendingComments(
	ctx context.Context, id core.ItemID, entry youtrackEntry, plan youtrackPlan,
) int {
	if !plan.params.IncludeComments || len(entry.comments) == 0 {
		return 0
	}
	opts := plan.options(id, core.NewTimestamp(v.now()))
	drafts, _ := mapping.CommentsToDrafts(entry.comments, entry.id(), opts)
	if id == "" {
		return len(drafts)
	}
	store, err := v.storeFor(core.ProjectKey(plan.project))
	if err != nil {
		return len(drafts)
	}
	already, err := youtrackCommentIDs(ctx, store, id)
	if err != nil {
		return len(drafts)
	}
	pending := 0
	for _, draft := range drafts {
		if !already[draft.External.ID] {
			pending++
		}
	}
	return pending
}

// youtrackCommentIDs is the set of YouTrack comment ids an item's thread
// already carries, which is what makes a re-import skip instead of duplicate.
func youtrackCommentIDs(
	ctx context.Context, store *core.FileStore, id core.ItemID,
) (map[string]bool, error) {
	comments, err := store.ListComments(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read the comments of %s: %w", id, err)
	}
	out := make(map[string]bool, len(comments))
	for _, comment := range comments {
		for _, ref := range comment.External {
			if strings.EqualFold(ref.System, mapping.System) && ref.ID != "" {
				out[ref.ID] = true
			}
		}
	}
	return out, nil
}
