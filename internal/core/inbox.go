package core

import (
	"errors"
	"sort"
	"strings"
)

// This file holds the inbox: the triage metadata an item carries while it is
// waiting to be accepted into the backlog (ADR-033).
//
// An inbox item is an ordinary item file. What makes it an inbox item is its
// status, which belongs to the reserved CategoryTriage; the `inbox:` block only
// records how it got there and what the triager decided. There is no inbox item
// type, no inbox folder and no stored boolean: the category is the truth.

// InboxStatus is the triage state of an item sitting in the inbox.
type InboxStatus string

// The triage states. Pending is the zero value in spirit: an inbox item without
// an explicit state is pending, because nobody has looked at it yet.
const (
	InboxPending   InboxStatus = "pending"
	InboxAccepted  InboxStatus = "accepted"
	InboxRejected  InboxStatus = "rejected"
	InboxSnoozed   InboxStatus = "snoozed"
	InboxDuplicate InboxStatus = "duplicate"
)

// Valid reports whether s is one of the five triage states.
func (s InboxStatus) Valid() bool {
	switch s {
	case InboxPending, InboxAccepted, InboxRejected, InboxSnoozed, InboxDuplicate:
		return true
	default:
		return false
	}
}

// InboxStatuses lists every triage state in a stable order.
func InboxStatuses() []InboxStatus {
	return []InboxStatus{InboxPending, InboxAccepted, InboxRejected, InboxSnoozed, InboxDuplicate}
}

// InboxScope decides what a query does with items in the triage category.
type InboxScope string

// The triage scopes. The zero value excludes the inbox, which is what makes
// every filter written before the inbox existed keep its meaning.
const (
	InboxExclude InboxScope = ""
	InboxOnly    InboxScope = "only"
	InboxInclude InboxScope = "include"
)

// Valid reports whether s is one of the three scopes.
func (s InboxScope) Valid() bool {
	switch s {
	case InboxExclude, InboxOnly, InboxInclude:
		return true
	default:
		return false
	}
}

// ParseInboxScope decodes the scope a surface received as a string. An empty
// string is the default scope, not an error.
func ParseInboxScope(s string) (InboxScope, bool) {
	scope := InboxScope(strings.ToLower(strings.TrimSpace(s)))
	switch scope {
	case InboxExclude, InboxOnly, InboxInclude:
		return scope, true
	case "exclude":
		return InboxExclude, true
	default:
		return InboxExclude, false
	}
}

// ItemInbox is the `inbox:` front-matter block of an item in triage.
//
// Unknown keys inside the block are preserved in Extra and written back
// verbatim, exactly as Item.Extra preserves unknown top-level keys (R-FMT-6), so
// a file written by a newer binary survives a round trip through an older one.
type ItemInbox struct {
	// Status is the triage decision. An empty value reads as pending.
	Status InboxStatus `json:"status,omitempty" yaml:"status,omitempty"`
	// SnoozedUntil is the date the item comes back to the pending queue. It is
	// required for, and only for, status snoozed. Expiry is a query-time
	// comparison against Filter.SnoozeAsOf: there is no scheduler anywhere.
	SnoozedUntil Date `json:"snoozedUntil,omitempty" yaml:"snoozed_until,omitempty"`
	// DuplicateOf names the item this one repeats. It is required for, and only
	// meaningful with, status duplicate.
	DuplicateOf ItemID `json:"duplicateOf,omitempty" yaml:"duplicate_of,omitempty"`
	// Source records where the submission came in from: "web", "mcp",
	// "youtrack", the name of a form. It is free text and never an enumeration.
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	// Received is when the submission arrived, which is not necessarily when the
	// file was created.
	Received Timestamp `json:"received,omitempty" yaml:"received,omitempty"`

	// Extra preserves the keys inside the block this version does not know.
	Extra map[string]any `json:"extra,omitempty" yaml:"-"`
}

// inboxKnownKeys is the set of keys inside the block this version understands.
// Everything else lands in ItemInbox.Extra and is written back after them,
// sorted lexicographically.
var inboxKnownKeys = map[string]bool{
	"status": true, "snoozed_until": true, "duplicate_of": true,
	"source": true, "received": true,
}

// inboxKeyOrder is the order the writer emits the known keys of the block in.
var inboxKeyOrder = []string{"status", "snoozed_until", "duplicate_of", "source", "received"}

// EffectiveStatus returns the triage state as a query sees it at the instant
// asOf: a snoozed item whose snoozed_until has arrived reads as pending again.
// A nil block, or one with no status, is pending.
//
// A zero asOf means "do not expire anything": the caller did not supply a clock,
// and core never reads one (the package compiles to WebAssembly and must stay
// deterministic).
func (in *ItemInbox) EffectiveStatus(asOf Timestamp) InboxStatus {
	if in == nil || in.Status == "" {
		return InboxPending
	}
	if in.Status != InboxSnoozed {
		return in.Status
	}
	if in.SnoozedUntil.IsZero() || asOf.IsZero() {
		return InboxSnoozed
	}
	if !in.SnoozedUntil.After(asOf.Time) {
		return InboxPending
	}
	return InboxSnoozed
}

// IsEmpty reports whether the block carries nothing worth writing.
func (in *ItemInbox) IsEmpty() bool {
	return in == nil ||
		(in.Status == "" && in.SnoozedUntil.IsZero() && in.DuplicateOf == "" &&
			in.Source == "" && in.Received.IsZero() && len(in.Extra) == 0)
}

// Clone returns a deep copy, including the preserved unknown keys.
func (in *ItemInbox) Clone() *ItemInbox {
	if in == nil {
		return nil
	}
	out := *in
	out.Extra = cloneMap(in.Extra)
	return &out
}

// Equal reports whether two blocks carry the same known fields. It ignores Extra,
// which is opaque to this version.
func (in *ItemInbox) Equal(other *ItemInbox) bool {
	if in == nil || other == nil {
		return in == nil && other == nil
	}
	return in.Status == other.Status &&
		in.SnoozedUntil.Equal(other.SnoozedUntil.Time) &&
		in.DuplicateOf == other.DuplicateOf &&
		in.Source == other.Source &&
		in.Received.Equal(other.Received.Time)
}

// TriageStatus returns the first status the workflow declares in the reserved
// triage category, or the empty status when the project declares none — which is
// simply a project without an inbox (ADR-033, negative consequence 2).
func (w Workflow) TriageStatus() Status {
	for _, s := range w.Statuses {
		if s.Category == CategoryTriage {
			return s.ID
		}
	}
	return ""
}

// TriageStatuses returns every status in the triage category, in declaration
// order. A project may declare more than one triage lane.
func (w Workflow) TriageStatuses() []Status {
	var out []Status
	for _, s := range w.Statuses {
		if s.Category == CategoryTriage {
			out = append(out, s.ID)
		}
	}
	return out
}

// IsTriageStatus reports whether a status belongs to the triage category of this
// project. An unknown status is not triage: the inbox is opt-in.
func (p *ProjectConfig) IsTriageStatus(id Status) bool {
	return p != nil && p.CategoryOf(id) == CategoryTriage
}

// validateInbox applies the E-INBOX-* rules to the block of an item.
//
// Resolution of duplicate_of against the rest of the vault is not done here: a
// single file cannot know what else exists. The index raises W-INBOX-DUP-DEAD
// for a target nothing declares.
func validateInbox(d *diagSet, item *Item, cfg *ProjectConfig) {
	in := item.Inbox
	if in == nil {
		return
	}
	if in.Status != "" && !in.Status.Valid() {
		d.errorf("inbox.status", CodeInboxStatus, "unknown inbox status %q (want one of %s)",
			in.Status, inboxStatusNames())
	}
	switch {
	case in.Status == InboxSnoozed && in.SnoozedUntil.IsZero():
		d.errorf("inbox.snoozed_until", CodeInboxSnooze, "status snoozed needs a snoozed_until date")
	case in.Status != InboxSnoozed && !in.SnoozedUntil.IsZero():
		d.errorf("inbox.snoozed_until", CodeInboxSnooze,
			"snoozed_until is only allowed with status snoozed, not with %q", in.EffectiveStatus(Timestamp{}))
	}
	if in.DuplicateOf != "" {
		if in.DuplicateOf == item.ID {
			d.errorf("inbox.duplicate_of", CodeInboxDuplicate, "an item cannot be a duplicate of itself")
		} else if _, _, _, err := ParseItemID(string(in.DuplicateOf)); err != nil {
			d.errorf("inbox.duplicate_of", CodeInboxDuplicate,
				"%q does not match <KEY>-<EP|US|T|M>-<NNNN>", in.DuplicateOf)
		}
	}
	if in.Status == InboxDuplicate && in.DuplicateOf == "" {
		d.errorf("inbox.duplicate_of", CodeInboxDuplicate, "status duplicate needs a duplicate_of reference")
	}
	// An inbox block outside the triage category is a warning, not an error: the
	// block survives acceptance, where it records how the item arrived.
	if cfg == nil || len(cfg.Workflow.Statuses) == 0 || item.Status == "" {
		return
	}
	if _, known := cfg.StatusDef(item.Status); !known {
		return
	}
	if !cfg.IsTriageStatus(item.Status) {
		d.warnf("inbox", CodeWarnInboxCategory,
			"an inbox block on status %q, which is not in the triage category, is ignored by the inbox", item.Status)
	}
}

// inboxStatusNames renders the accepted triage states for a diagnostic message.
func inboxStatusNames() string {
	names := make([]string, 0, len(InboxStatuses()))
	for _, s := range InboxStatuses() {
		names = append(names, string(s))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ErrNoTriageStatus is returned when something asks for the inbox of a project
// that declares no status in the reserved triage category. Such a project
// simply has no inbox (ADR-033, negative consequence 2); the fix is a status in
// project.yaml, never a retry.
var ErrNoTriageStatus = errors.New("the project declares no status in the triage category")

// InboxLandingStatus returns the status a newly created item is written with,
// given whether the caller wants it to land in the triage queue.
//
// It is the one decision an importer makes about where work arrives, and it is
// here rather than in the importer so that every entry point — the YouTrack
// import of GIT-EP-0012, an agent's create_inbox_item, a web submission —
// answers it the same way:
//
//   - landInInbox false: the workflow's initial status, which is where work has
//     always arrived.
//   - landInInbox true: the project's first triage status, or ErrNoTriageStatus
//     when it declares none.
//
// The error matters. "Put a thousand imported issues somewhere for review" and
// "this project has no place to review them" is a configuration mistake, and
// silently landing them in the backlog instead would be the one outcome the
// option exists to prevent.
func InboxLandingStatus(cfg *ProjectConfig, landInInbox bool) (Status, error) {
	if cfg == nil {
		return "", ErrNoTriageStatus
	}
	if !landInInbox {
		return cfg.InitialStatus(), nil
	}
	triage := cfg.Workflow.TriageStatus()
	if triage == "" {
		return "", ErrNoTriageStatus
	}
	return triage, nil
}
