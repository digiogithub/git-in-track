package mapping

import (
	"fmt"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// System is the value written into the `system` key of every external
// reference this package produces. It is lower case because core normalises
// external systems to lower case (ADR-031).
const System = "youtrack"

// DefaultAttachmentPrefix is the vault-relative folder attachment references
// are rewritten under. The final path of an embed is
// "<prefix>/<ITEM-ID>/<filename>" (docs/03 section on attachments).
const DefaultAttachmentPrefix = ".pmngr/attachments"

// Warning is one value this package could not map. It is returned alongside a
// result and never instead of one: an import reports its warnings and finishes,
// because a single unknown enum value must not cost the whole issue.
type Warning struct {
	// Field names what was being mapped: a YouTrack custom-field name such as
	// "State", or one of the pseudo-fields "links", "description", "tags",
	// "comments" and "external".
	Field string `json:"field"`
	// Value is the YouTrack value that was not understood, empty when the
	// warning is about the shape of a payload rather than about one value.
	Value string `json:"value,omitempty"`
	// Fallback is what the mapper used instead, empty when nothing was used.
	Fallback string `json:"fallback,omitempty"`
	// Reason is a complete English sentence, safe to show to a human.
	Reason string `json:"reason"`
}

// String renders the warning as one line, which is what a CLI import report
// prints and what the golden tests compare.
func (w Warning) String() string {
	var b strings.Builder
	b.WriteString(w.Field)
	if w.Value != "" {
		b.WriteString(": ")
		b.WriteString(w.Value)
	}
	b.WriteString(": ")
	b.WriteString(w.Reason)
	return b.String()
}

// Options is everything the mapper needs beyond the issue itself. Its zero
// value is usable: the field map falls back to DefaultFieldMap and the
// attachment prefix to DefaultAttachmentPrefix.
type Options struct {
	// BaseURL is the YouTrack instance base URL, context path included, with no
	// trailing slash — the same string the client was configured with. It is
	// only used to build the `url` of an external reference; an empty BaseURL
	// produces an external reference with no url, which core accepts.
	BaseURL string

	// FieldMap translates YouTrack field names and values into git-in-track
	// ones. A zero FieldMap is filled in from DefaultFieldMap, and a partially
	// filled one keeps every entry the caller set and defaults the rest.
	FieldMap FieldMap

	// ItemID is the git-in-track id the issue is being imported as. It is used
	// only to build attachment paths, so it may be empty when the id has not
	// been allocated yet — attachment embeds are then left untouched and a
	// warning says so.
	ItemID string

	// AttachmentPrefix overrides DefaultAttachmentPrefix.
	AttachmentPrefix string

	// SyncedAt is stamped onto the external reference. This package never reads
	// a clock, so a caller that wants a synced_at must pass one; the zero value
	// leaves the key out.
	SyncedAt core.Timestamp
}

// attachmentPrefix returns the configured prefix or the default.
func (o Options) attachmentPrefix() string {
	if p := strings.Trim(strings.TrimSpace(o.AttachmentPrefix), "/"); p != "" {
		return p
	}
	return DefaultAttachmentPrefix
}

// fieldMap returns the caller's field map with every unset entry defaulted.
func (o Options) fieldMap() FieldMap { return o.FieldMap.withDefaults() }

// Relations are the parts of a mapped issue that name other items by their
// YouTrack identity, because this package cannot resolve them: only the
// importer knows which git-in-track items already exist.
//
// Resolve every one of these through core.Index.ItemByExternal (system
// "youtrack") before writing them onto an item. They are deliberately kept off
// the draft so that an unresolved YouTrack id can never be written into a
// `parent`, `milestone` or `links[].target` field as if it were an item id.
type Relations struct {
	// Parent is the idReadable of the issue that is this issue's parent, from
	// the inward half of the Subtask link. Empty when the issue has no parent.
	Parent string `json:"parent,omitempty"`
	// Children are the idReadable values of the issues this issue is the parent
	// of, from the outward half of the Subtask link.
	Children []string `json:"children,omitempty"`
	// Milestone is the name of the version-bundle value that stands in for a
	// milestone, from the field FieldMap.MilestoneField names. Empty when the
	// issue is not assigned to a version.
	Milestone string `json:"milestone,omitempty"`
	// Links are the non-hierarchy relations. Each Target holds a YouTrack
	// idReadable, never a git-in-track item id.
	Links []core.Link `json:"links,omitempty"`
}

// External returns the external reference every item and comment mapped from
// this instance carries. id is the YouTrack identifier — an issue's idReadable
// or a comment's internal id — and path is the URL path segment it lives
// under, "issue" or "article".
func (o Options) External(id, path string) core.External {
	e := core.External{System: System, ID: strings.TrimSpace(id), SyncedAt: o.SyncedAt}
	base := strings.TrimRight(strings.TrimSpace(o.BaseURL), "/")
	if base != "" && e.ID != "" {
		e.URL = fmt.Sprintf("%s/%s/%s", base, path, e.ID)
	}
	return core.NormalizeExternal(e)
}

// sortWarnings orders warnings by field then value then reason, so that a
// result is reproducible whatever order the mappers ran in. It sorts in place
// and returns nil for an empty slice, which keeps golden files tidy.
func sortWarnings(ws []Warning) []Warning {
	if len(ws) == 0 {
		return nil
	}
	sort.SliceStable(ws, func(i, j int) bool {
		a, b := ws[i], ws[j]
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		return a.Reason < b.Reason
	})
	return ws
}
