package vault

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the inbox half of the item surface: "inbox.list" and
// "inbox.triage" (story GIT-US-0056, ADR-033).
//
// An inbox item is an ordinary item whose status belongs to the reserved triage
// category; the `inbox:` block records how it arrived and what the triager
// decided. Every decision — what counts as triage, when a snooze has expired,
// which fields a triage state owns — is taken by internal/core. What lives here
// is the plumbing: decode the request, hold the vault mutex once, produce one
// WriteSet.

// InboxTriageAction is one of the four outcomes of triaging an inbox item.
type InboxTriageAction string

// The four triage outcomes (ADR-033). Accepting does not decide the item's
// state for the user: it clears triage and moves the item into the ordinary
// workflow, and the surface reopens the edit form so a person picks the rest.
const (
	TriageAccept    InboxTriageAction = "accept"
	TriageReject    InboxTriageAction = "reject"
	TriageSnooze    InboxTriageAction = "snooze"
	TriageDuplicate InboxTriageAction = "duplicate"
)

// Valid reports whether a is one of the four triage outcomes.
func (a InboxTriageAction) Valid() bool {
	switch a {
	case TriageAccept, TriageReject, TriageSnooze, TriageDuplicate:
		return true
	default:
		return false
	}
}

// InboxTriageActions lists the outcomes in a stable order, for a message that
// has to name them.
func InboxTriageActions() []InboxTriageAction {
	return []InboxTriageAction{TriageAccept, TriageReject, TriageSnooze, TriageDuplicate}
}

// NoTriageStatusCode is the machine code of a project that declares no status in
// the triage category, which is simply a project without an inbox (ADR-033).
const NoTriageStatusCode = "no_triage_status"

// inboxDraftParams is the `inbox` option of "item.create": it turns an ordinary
// draft into a submission waiting for triage.
type inboxDraftParams struct {
	// Source records where the submission came from: "web", "mcp", "youtrack",
	// the name of a form. It is free text and never an enumeration.
	Source string `json:"source,omitempty"`
	// Received is when the submission arrived, which is not necessarily when the
	// file was written. Empty stamps the vault clock.
	Received string `json:"received,omitempty"`
}

// inboxListParams is the input of "inbox.list". `status` filters on the triage
// state, not on the workflow status: the workflow status of every item this
// answers is a triage status by construction.
type inboxListParams struct {
	Project string     `json:"project,omitempty"`
	Status  stringList `json:"status,omitempty"`
	Type    stringList `json:"type,omitempty"`
	Label   stringList `json:"label,omitempty"`
	// Assignee and Text are the two filters a triage queue actually uses beyond
	// the triage state itself.
	Assignee string   `json:"assignee,omitempty"`
	Text     string   `json:"text,omitempty"`
	Sort     string   `json:"sort,omitempty"`
	Order    string   `json:"order,omitempty"`
	Limit    int      `json:"limit,omitempty"`
	Cursor   string   `json:"cursor,omitempty"`
	Fields   []string `json:"fields,omitempty"`
}

// InboxPage is the answer of "inbox.list": one page of the triage queue plus the
// counts the sidebar badge and the `inbox.changed` event need.
//
// Counts are computed over the whole queue, not over the page, and snooze expiry
// is resolved against the vault clock — a snoozed item whose date has arrived is
// counted as pending, because that is what a reader sees.
type InboxPage struct {
	Items      []core.Item              `json:"items"`
	NextCursor string                   `json:"nextCursor,omitempty"`
	Total      int                      `json:"total"`
	Counts     map[core.InboxStatus]int `json:"counts"`
	Pending    int                      `json:"pending"`
}

// InboxTriageParams is the input of "inbox.triage": one item, the rev it was
// read at, and exactly one action with the fields that action owns.
type InboxTriageParams struct {
	ID     string `json:"id"`
	Rev    string `json:"rev,omitempty"`
	Action string `json:"action"`
	// Status is the workflow status `accept` moves the item to. Empty picks the
	// initial status of the project's workflow, which is what "into the ordinary
	// backlog" means.
	Status string `json:"status,omitempty"`
	// Type is accepted for symmetry with the edit form and must match the item's
	// own type: an item id pins its type for life (R-ID-3).
	Type string `json:"type,omitempty"`
	// Parent is the epic or story `accept` files the item under. Empty leaves the
	// parent alone.
	Parent string `json:"parent,omitempty"`
	// SnoozedUntil is the date a `snooze` brings the item back on, `YYYY-MM-DD`.
	SnoozedUntil string `json:"snoozedUntil,omitempty"`
	// DuplicateOf is the item a `duplicate` repeats.
	DuplicateOf string `json:"duplicateOf,omitempty"`
}

// InboxTriageResult is the answer of "inbox.triage".
type InboxTriageResult struct {
	Item   core.Item         `json:"item"`
	Action InboxTriageAction `json:"action"`
	// Pending is the number of items still waiting after this decision, so that
	// a badge never needs a second call.
	Pending int      `json:"pending"`
	Writes  WriteSet `json:"writes"`
}

// ------------------------------------------------------------------ list ----

// inboxList answers the triage queue of one project, or of every project the
// vault holds when none is named. The caller holds the lock.
func (v *Vault) inboxList(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[inboxListParams](raw)
	if err != nil {
		return nil, err
	}
	filter, err := v.inboxFilter(p)
	if err != nil {
		return nil, err
	}
	page, err := v.index.Inbox(ctx, filter)
	if err != nil {
		return nil, failf("invalid_request", "%v", err)
	}
	items := page.Items
	if items == nil {
		items = []core.Item{}
	}
	if !wantsBody(p.Fields) {
		for i := range items {
			items[i].Body = ""
		}
	}
	counts, err := v.inboxCounts(ctx, filter)
	if err != nil {
		return nil, err
	}
	return InboxPage{
		Items:      items,
		NextCursor: page.NextCursor,
		Total:      page.Total,
		Counts:     counts,
		Pending:    counts[core.InboxPending],
	}, nil
}

// inboxFilter builds the core filter of a triage listing. SnoozeAsOf is always
// the vault clock: a surface that forgets it silently expires nothing, so it is
// set here rather than left to the caller (ADR-033).
func (v *Vault) inboxFilter(p inboxListParams) (core.Filter, error) {
	f := core.Filter{
		Inbox:      core.InboxOnly,
		SnoozeAsOf: core.NewTimestamp(v.now()),
		Assignees:  nonEmpty(p.Assignee),
		Labels:     p.Label,
		Text:       p.Text,
		Sort:       sortSpec(p.Sort, p.Order),
		Limit:      p.Limit,
		Cursor:     p.Cursor,
		Fields:     p.Fields,
	}
	if p.Project != "" {
		f.Projects = []core.ProjectKey{core.ProjectKey(p.Project)}
	}
	for _, t := range p.Type {
		f.Types = append(f.Types, core.ItemType(t))
	}
	for _, raw := range p.Status {
		status, err := parseInboxStatus(raw)
		if err != nil {
			return core.Filter{}, err
		}
		f.InboxStatuses = append(f.InboxStatuses, status)
	}
	return f, nil
}

// inboxCounts tallies the whole queue by triage state, ignoring the state filter
// and the pagination of the listing that asked for it.
func (v *Vault) inboxCounts(ctx context.Context, filter core.Filter) (map[core.InboxStatus]int, error) {
	counts := make(map[core.InboxStatus]int, len(core.InboxStatuses()))
	for _, status := range core.InboxStatuses() {
		probe := filter
		probe.InboxStatuses = []core.InboxStatus{status}
		probe.Cursor = ""
		probe.Fields = nil
		// One item is enough: the page carries the total of the whole match.
		probe.Limit = 1
		page, err := v.index.Inbox(ctx, probe)
		if err != nil {
			return nil, failf("invalid_request", "%v", err)
		}
		counts[status] = page.Total
	}
	return counts, nil
}

// parseInboxStatus decodes one triage state of a filter.
func parseInboxStatus(raw string) (core.InboxStatus, error) {
	status := core.InboxStatus(strings.ToLower(strings.TrimSpace(raw)))
	if !status.Valid() {
		return "", failf("invalid_request",
			"%q is not a triage state: pending, accepted, rejected, snoozed or duplicate", raw)
	}
	return status, nil
}

// ---------------------------------------------------------------- triage ----

// inboxTriage applies exactly one triage decision to one item, inside the vault
// mutex and as a single rev-checked write. The caller holds the lock.
func (v *Vault) inboxTriage(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[InboxTriageParams](raw)
	if err != nil {
		return nil, err
	}
	action := InboxTriageAction(strings.ToLower(strings.TrimSpace(p.Action)))
	if !action.Valid() {
		return nil, failf("invalid_request", "%q is not a triage action: %s",
			p.Action, triageActionNames())
	}
	id := core.ItemID(strings.TrimSpace(p.ID))
	if id == "" {
		return nil, failf("invalid_request", "inbox.triage needs an item id")
	}
	store, err := v.storeForItem(id)
	if err != nil {
		return nil, err
	}
	current, err := store.Get(ctx, id)
	if err != nil {
		return nil, failf("not_found", "no item %s in this repository", id)
	}
	key, cfg, err := v.projectConfigOf(id)
	if err != nil {
		return nil, err
	}

	patch, target, err := v.triagePatch(action, p, current, cfg)
	if err != nil {
		return nil, err
	}

	v.fs.begin()
	it, err := store.Update(ctx, id, patch, core.Rev(p.Rev))
	if err != nil {
		return nil, fmt.Errorf("triage %s: %w", id, err)
	}
	if target != nil {
		if err := v.writeDuplicateInverse(ctx, id, *target); err != nil {
			return nil, err
		}
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	pending, err := v.inboxCounts(ctx, core.Filter{
		Inbox: core.InboxOnly, SnoozeAsOf: core.NewTimestamp(v.now()),
		Projects: []core.ProjectKey{key},
	})
	if err != nil {
		return nil, err
	}
	return InboxTriageResult{
		Item: *it, Action: action, Pending: pending[core.InboxPending], Writes: writes,
	}, nil
}

// triagePatch builds the single patch one action applies, and the duplicate
// target whose inverse link has to be written afterwards.
//
// Each action writes only the fields it owns: the `inbox:` block is rebuilt from
// the one already on the file, so `source` and `received` — how the submission
// arrived — survive every decision.
func (v *Vault) triagePatch(
	action InboxTriageAction, p InboxTriageParams, current *core.Item, cfg *core.ProjectConfig,
) (core.ItemPatch, *core.ItemID, error) {
	block := current.Inbox.Clone()
	if block == nil {
		block = &core.ItemInbox{}
	}
	// A decision replaces whatever the previous one recorded; only the arrival
	// facts are carried over.
	block.SnoozedUntil = core.Date{}
	block.DuplicateOf = ""

	patch := core.ItemPatch{}
	switch action {
	case TriageAccept:
		status, err := acceptStatus(p.Status, cfg)
		if err != nil {
			return core.ItemPatch{}, nil, err
		}
		if p.Type != "" && core.ItemType(p.Type) != current.Type {
			return core.ItemPatch{}, nil, failf("invalid_request",
				"%s is a %s and an item id pins its type for life: create a %s and mark this one duplicate",
				current.ID, current.Type, p.Type)
		}
		if parent := strings.TrimSpace(p.Parent); parent != "" {
			id := core.ItemID(parent)
			patch.Parent = &id
		}
		block.Status = core.InboxAccepted
		patch.Status = &status
	case TriageReject:
		status, err := rejectStatus(cfg)
		if err != nil {
			return core.ItemPatch{}, nil, err
		}
		block.Status = core.InboxRejected
		patch.Status = &status
	case TriageSnooze:
		date, err := parseSnoozeDate(p.SnoozedUntil)
		if err != nil {
			return core.ItemPatch{}, nil, err
		}
		block.Status = core.InboxSnoozed
		block.SnoozedUntil = date
	case TriageDuplicate:
		target, err := v.duplicateTarget(p.DuplicateOf, current.ID)
		if err != nil {
			return core.ItemPatch{}, nil, err
		}
		block.Status = core.InboxDuplicate
		block.DuplicateOf = target
		patch.AddLinks = []core.Link{{Kind: core.LinkDuplicates, Target: string(target)}}
		patch.Inbox = block
		return patch, &target, nil
	}
	patch.Inbox = block
	return patch, nil, nil
}

// acceptStatus is the workflow status an accepted submission lands in: the one
// the caller chose, or the project's initial status. It is never a triage status
// — accepting means leaving the inbox.
func acceptStatus(requested string, cfg *core.ProjectConfig) (core.Status, error) {
	if chosen := core.Status(strings.TrimSpace(requested)); chosen != "" {
		if cfg.IsTriageStatus(chosen) {
			return "", failf("invalid_request",
				"%q is a triage status: accepting an item moves it out of the inbox", chosen)
		}
		return chosen, nil
	}
	if initial := cfg.InitialStatus(); initial != "" && !cfg.IsTriageStatus(initial) {
		return initial, nil
	}
	if backlog := core.BacklogStatus(cfg); backlog != "" && !cfg.IsTriageStatus(backlog) {
		return backlog, nil
	}
	return "", failf("invalid_request",
		"the workflow of %s declares no status outside triage to accept into", cfg.Key)
}

// rejectStatus is the status a rejected submission is moved to: the first
// status of the cancelled category. Rejection is a status, never a deletion
// (ADR-026).
func rejectStatus(cfg *core.ProjectConfig) (core.Status, error) {
	for _, def := range cfg.Workflow.Statuses {
		if def.Category == core.CategoryCancelled {
			return def.ID, nil
		}
	}
	return "", failf("invalid_request",
		"the workflow of %s declares no cancelled status to reject into", cfg.Key)
}

// parseSnoozeDate decodes the day a snoozed item comes back. Expiry is a
// query-time comparison against the clock, never a scheduler.
func parseSnoozeDate(raw string) (core.Date, error) {
	if strings.TrimSpace(raw) == "" {
		return core.Date{}, failf("invalid_request", "snoozing needs a snoozedUntil date")
	}
	date, err := core.ParseDate(strings.TrimSpace(raw))
	if err != nil {
		return core.Date{}, failf("invalid_request", "snoozedUntil: %v", err)
	}
	return date, nil
}

// duplicateTarget validates the item a duplicate points at: it has to parse, it
// cannot be the item itself, and it has to exist — a dead pointer would only be
// found later, by the index warning.
func (v *Vault) duplicateTarget(raw string, self core.ItemID) (core.ItemID, error) {
	target := core.ItemID(strings.TrimSpace(raw))
	if target == "" {
		return "", failf("invalid_request", "marking a duplicate needs the duplicateOf item")
	}
	if target == self {
		return "", failf("invalid_request", "an item cannot be a duplicate of itself")
	}
	if _, _, _, err := core.ParseItemID(string(target)); err != nil {
		return "", failf("invalid_request", "duplicateOf: %v", err)
	}
	if _, err := v.index.Item(target); err != nil {
		return "", failf("not_found", "no item %s to be a duplicate of", target)
	}
	return target, nil
}

// writeDuplicateInverse records the `duplicated_by` half of a duplicate link on
// the target, so that the relation reads the same from both ends
// (core.LinkKind.Inverse). It joins the WriteSet the triage already opened.
//
// The link is written unconditionally: the caller quoted the rev of the item it
// triaged, not of the item it pointed at, and adding a link is idempotent.
func (v *Vault) writeDuplicateInverse(ctx context.Context, from, target core.ItemID) error {
	store, err := v.storeForItem(target)
	if err != nil {
		return err
	}
	inverse := core.Link{Kind: core.LinkDuplicates.Inverse(), Target: string(from)}
	if _, err := store.Update(ctx, target, core.ItemPatch{AddLinks: []core.Link{inverse}}, ""); err != nil {
		return fmt.Errorf("link %s as duplicated by %s: %w", target, from, err)
	}
	return nil
}

// ---------------------------------------------------------------- create ----

// inboxDraft folds the `inbox` option of "item.create" into a draft: the item is
// forced into the project's triage status and stamped as a pending submission.
// A project that declares no triage status has no inbox, and says so.
func (v *Vault) inboxDraft(draft *core.ItemDraft, p *inboxDraftParams, cfg *core.ProjectConfig) error {
	triage := cfg.Workflow.TriageStatus()
	if triage == "" {
		return failf(NoTriageStatusCode,
			"project %s declares no status in the triage category, so it has no inbox: "+
				"add one to %s to receive submissions", cfg.Key, core.ProjectFileName)
	}
	received := core.NewTimestamp(v.now())
	if stamped := strings.TrimSpace(p.Received); stamped != "" {
		parsed, err := core.ParseTimestamp(stamped)
		if err != nil {
			return failf("invalid_request", "received: %v", err)
		}
		received = parsed
	}
	// The triage status is forced rather than defaulted: an item in the inbox is
	// one whose status is triage, and nothing else makes it one.
	draft.Status = triage
	draft.Inbox = &core.ItemInbox{
		Status:   core.InboxPending,
		Source:   strings.TrimSpace(p.Source),
		Received: received,
	}
	return nil
}

// --------------------------------------------------------------- helpers ----

// projectConfigOf returns the configuration of the project that owns an item.
// The caller holds the lock.
func (v *Vault) projectConfigOf(id core.ItemID) (core.ProjectKey, *core.ProjectConfig, error) {
	key, _, _, err := core.ParseItemID(string(id))
	if err != nil {
		return v.projectConfig("")
	}
	return v.projectConfig(key)
}

// projectConfig returns the configuration of one project of this repository. An
// empty key is allowed when the repository holds exactly one project, which is
// the browser-only common case. The caller holds the lock.
func (v *Vault) projectConfig(key core.ProjectKey) (core.ProjectKey, *core.ProjectConfig, error) {
	var found []core.ProjectRef
	for _, p := range v.projects {
		if p.Team {
			continue
		}
		if key == "" || p.Key == key {
			found = append(found, p)
		}
	}
	switch {
	case len(found) == 0:
		return "", nil, failf("not_found", "project %q is not open in this repository", key)
	case len(found) > 1:
		return "", nil, failf("invalid_request",
			"this repository holds %d projects: name one", len(found))
	case found[0].Config == nil:
		return "", nil, failf("invalid_request",
			"project %s has no readable %s", found[0].Key, core.ProjectFileName)
	}
	return found[0].Key, found[0].Config, nil
}

// triageActionNames renders the accepted actions for a refusal message.
func triageActionNames() string {
	names := make([]string, 0, len(InboxTriageActions()))
	for _, a := range InboxTriageActions() {
		names = append(names, string(a))
	}
	return strings.Join(names, ", ")
}
