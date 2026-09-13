package mapping

import (
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// CommentDraft is one YouTrack comment ready to be written.
//
// It wraps core.CommentDraft rather than being one because core.CommentDraft
// has no `external` field: core.Comment does, but the draft that creates it
// does not, so the external reference travels beside the draft and the
// importer records it. Updated travels beside it for the same reason.
type CommentDraft struct {
	// Draft is what core.Store.AddComment takes.
	Draft core.CommentDraft `json:"draft"`
	// External is the reference to the YouTrack comment, keyed on the comment's
	// internal id. Its url is the issue's, with the comment as a fragment,
	// which is the addressable form YouTrack uses.
	External core.External `json:"external"`
	// Updated is the edit timestamp YouTrack reported, zero when the comment
	// was never edited.
	Updated core.Timestamp `json:"updated,omitempty"`
}

// CommentsToDrafts maps an issue's comments onto comment drafts, preserving the
// original author and the original timestamp: an imported thread must read as
// the conversation it was, not as a burst of writes by the importer.
//
// Each draft carries the login as the handle, the full name and the email as
// the git identity, the created timestamp converted from unix milliseconds,
// kind "comment", and the body run through NormalizeDescription.
//
// Deleted comments are skipped, and a comment with an empty body is skipped
// with a warning because core.Store.AddComment refuses one. Drafts come back in
// created order, so the file names ADR-012 derives from the timestamps are
// allocated in the order the conversation happened; the file-name rule itself
// is the caller's concern.
func CommentsToDrafts(comments []youtrack.Comment, issueID string, opts Options) ([]CommentDraft, []Warning) {
	ordered := make([]youtrack.Comment, 0, len(comments))
	for _, c := range comments {
		if c.Deleted {
			continue
		}
		ordered = append(ordered, c)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Created < ordered[j].Created })

	var (
		out      []CommentDraft
		warnings []Warning
	)
	for _, c := range ordered {
		body, ws := normalizeBody(c.Text, opts.ItemID, opts.attachmentPrefix())
		warnings = append(warnings, ws...)
		if strings.TrimSpace(body) == "" {
			warnings = append(warnings, Warning{
				Field:  "comments",
				Value:  c.ID,
				Reason: "the comment has an empty body and was skipped, because a git-in-track comment cannot be empty",
			})
			continue
		}
		if c.Created.IsZero() {
			warnings = append(warnings, Warning{
				Field:  "comments",
				Value:  c.ID,
				Reason: "the comment came back without a created timestamp, so the store will stamp it at write time",
			})
		}
		out = append(out, CommentDraft{
			Draft: core.CommentDraft{
				Author:      userHandle(c.Author),
				AuthorName:  strings.TrimSpace(c.Author.FullName),
				AuthorEmail: strings.TrimSpace(c.Author.Email),
				Body:        body,
				Kind:        core.CommentKindComment,
				Created:     timestamp(c.Created),
			},
			External: opts.commentExternal(issueID, c.ID),
			Updated:  timestamp(c.Updated),
		})
	}
	return out, sortWarnings(warnings)
}

// commentExternal builds the external reference of one comment: the comment's
// own internal id, addressed at the issue url with the comment as a fragment.
func (o Options) commentExternal(issueID, commentID string) core.External {
	e := o.External(commentID, "issue")
	issue := strings.TrimSpace(issueID)
	base := strings.TrimRight(strings.TrimSpace(o.BaseURL), "/")
	if base != "" && issue != "" {
		e.URL = base + "/issue/" + issue + "#focus=Comments-" + strings.TrimSpace(commentID)
	} else {
		e.URL = ""
	}
	return core.NormalizeExternal(e)
}

// timestamp converts a YouTrack millisecond timestamp into a core timestamp,
// keeping the zero value for "not set" — a nil or null `updated` is normal on a
// comment that was never edited.
func timestamp(m youtrack.Millis) core.Timestamp {
	if m.IsZero() {
		return core.Timestamp{}
	}
	return core.NewTimestamp(m.Time())
}
