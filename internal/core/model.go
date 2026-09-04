package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// TimestampLayout is the only accepted serialization of a point in time: ISO 8601
// in UTC, second precision, "Z" suffix (R-TIME-1).
const TimestampLayout = "2006-01-02T15:04:05Z"

// DateLayout is the serialization of a date-only field such as due (R-TIME-2).
const DateLayout = "2006-01-02"

// ItemID is the human-speakable identifier of an item, e.g. "ACME-US-0042".
type ItemID string

// String returns the identifier as a plain string.
func (id ItemID) String() string { return string(id) }

// ItemType is the kind of item stored in a file.
type ItemType string

// The item types of a project backlog. Board, sprint and retro types exist only in
// the team repository and are therefore not part of this enumeration.
const (
	TypeEpic      ItemType = "epic"
	TypeStory     ItemType = "story"
	TypeTask      ItemType = "task"
	TypeMilestone ItemType = "milestone"
	TypeComment   ItemType = "comment"
)

// Valid reports whether t is one of the known item types.
func (t ItemType) Valid() bool {
	switch t {
	case TypeEpic, TypeStory, TypeTask, TypeMilestone, TypeComment:
		return true
	default:
		return false
	}
}

// ItemTypes lists every known item type in a stable order.
func ItemTypes() []ItemType {
	return []ItemType{TypeEpic, TypeStory, TypeTask, TypeMilestone, TypeComment}
}

// Status is the identifier of a workflow status declared in project.yaml.
type Status string

// StatusCategory is the coarse bucket a status belongs to. It is what makes
// projects with different workflows comparable on a team board.
type StatusCategory string

// The four status categories.
const (
	CategoryTodo       StatusCategory = "todo"
	CategoryInProgress StatusCategory = "in_progress"
	CategoryDone       StatusCategory = "done"
	CategoryCancelled  StatusCategory = "cancelled"
)

// Valid reports whether c is one of the four known categories.
func (c StatusCategory) Valid() bool {
	switch c {
	case CategoryTodo, CategoryInProgress, CategoryDone, CategoryCancelled:
		return true
	default:
		return false
	}
}

// Priority is the importance of an item.
type Priority string

// The four priorities. Reordering them in project.yaml is allowed, renaming is not.
const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityMedium   Priority = "medium"
	PriorityLow      Priority = "low"
)

// Valid reports whether p is one of the four known priorities.
func (p Priority) Valid() bool {
	switch p {
	case PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow:
		return true
	default:
		return false
	}
}

// Rev is the optimistic-concurrency token of a file: a content hash of its
// canonical bytes, e.g. "sha256:9f2b1c7d0a4e5b31". It is never stored in a file.
type Rev string

// LinkKind is the semantic of a typed relation between two items.
type LinkKind string

// The relation kinds accepted in the links field.
const (
	LinkBlocks       LinkKind = "blocks"
	LinkBlockedBy    LinkKind = "blocked_by"
	LinkRelatesTo    LinkKind = "relates_to"
	LinkDuplicates   LinkKind = "duplicates"
	LinkDuplicatedBy LinkKind = "duplicated_by"
)

// Valid reports whether k is one of the known relation kinds.
func (k LinkKind) Valid() bool {
	switch k {
	case LinkBlocks, LinkBlockedBy, LinkRelatesTo, LinkDuplicates, LinkDuplicatedBy:
		return true
	default:
		return false
	}
}

// Inverse returns the relation kind seen from the target item.
func (k LinkKind) Inverse() LinkKind {
	switch k {
	case LinkBlocks:
		return LinkBlockedBy
	case LinkBlockedBy:
		return LinkBlocks
	case LinkDuplicates:
		return LinkDuplicatedBy
	case LinkDuplicatedBy:
		return LinkDuplicates
	case LinkRelatesTo:
		return LinkRelatesTo
	default:
		return k
	}
}

// Link is one typed relation stored on the source item only; the inverse is
// computed by the index and never written to the target file (R-LINK-1).
type Link struct {
	Kind   LinkKind `json:"kind" yaml:"kind"`
	Target string   `json:"target" yaml:"target"`
	Note   string   `json:"note,omitempty" yaml:"note,omitempty"`
}

// Timestamp is an instant with second precision, always rendered in UTC as
// "2006-01-02T15:04:05Z". Its zero value means "absent".
type Timestamp struct {
	time.Time
}

// NewTimestamp returns t truncated to the second and converted to UTC.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp{Time: t.UTC().Truncate(time.Second)}
}

// ParseTimestamp parses the canonical form, and tolerates the other RFC 3339
// spellings a human or another tool may have written.
func ParseTimestamp(s string) (Timestamp, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{TimestampLayout, time.RFC3339Nano, time.RFC3339, DateLayout} {
		if t, err := time.Parse(layout, s); err == nil {
			return NewTimestamp(t), nil
		}
	}
	return Timestamp{}, fmt.Errorf("parse timestamp %q: want %s", s, TimestampLayout)
}

// String renders the canonical form, or the empty string when absent.
func (ts Timestamp) String() string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(TimestampLayout)
}

// MarshalYAML emits an unquoted ISO 8601 timestamp (R-TIME-4).
func (ts Timestamp) MarshalYAML() (any, error) {
	if ts.IsZero() {
		return nil, nil
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: ts.String()}, nil
}

// UnmarshalYAML accepts both the YAML timestamp form and the quoted string form.
func (ts *Timestamp) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		*ts = Timestamp{}
		return nil
	}
	parsed, err := ParseTimestamp(node.Value)
	if err != nil {
		return err
	}
	*ts = parsed
	return nil
}

// MarshalJSON renders the canonical form, or null when absent.
func (ts Timestamp) MarshalJSON() ([]byte, error) {
	if ts.IsZero() {
		return []byte("null"), nil
	}
	data, err := json.Marshal(ts.String())
	if err != nil {
		return nil, fmt.Errorf("encode timestamp: %w", err)
	}
	return data, nil
}

// UnmarshalJSON accepts the canonical form and null.
func (ts *Timestamp) UnmarshalJSON(data []byte) error {
	var s *string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("decode timestamp: %w", err)
	}
	if s == nil || *s == "" {
		*ts = Timestamp{}
		return nil
	}
	parsed, err := ParseTimestamp(*s)
	if err != nil {
		return err
	}
	*ts = parsed
	return nil
}

// Date is a calendar date without a time zone, rendered as "2006-01-02".
// Its zero value means "absent".
type Date struct {
	time.Time
}

// NewDate returns the date part of t.
func NewDate(t time.Time) Date {
	y, m, d := t.UTC().Date()
	return Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

// ParseDate parses "YYYY-MM-DD", and tolerates a full timestamp by keeping its
// date part, which is what a YAML timestamp scalar decodes to.
func ParseDate(s string) (Date, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{DateLayout, TimestampLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return NewDate(t), nil
		}
	}
	return Date{}, fmt.Errorf("parse date %q: want %s", s, DateLayout)
}

// String renders "YYYY-MM-DD", or the empty string when absent.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.UTC().Format(DateLayout)
}

// MarshalYAML emits an unquoted ISO 8601 date.
func (d Date) MarshalYAML() (any, error) {
	if d.IsZero() {
		return nil, nil
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: d.String()}, nil
}

// UnmarshalYAML accepts both the YAML timestamp form and the quoted string form.
func (d *Date) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(node.Value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// MarshalJSON renders "YYYY-MM-DD", or null when absent.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	data, err := json.Marshal(d.String())
	if err != nil {
		return nil, fmt.Errorf("encode date: %w", err)
	}
	return data, nil
}

// UnmarshalJSON accepts "YYYY-MM-DD" and null.
func (d *Date) UnmarshalJSON(data []byte) error {
	var s *string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("decode date: %w", err)
	}
	if s == nil || *s == "" {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(*s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Item is one epic, story, task or milestone: the typed form of a Markdown file
// with YAML front matter under .pmngr/. Fields map one-to-one to front-matter
// keys; the three trailing fields are derived and never stored in the file.
//
// A nil-able numeric field (Estimate, Effort, Spent) distinguishes "absent" from
// the legitimate value zero.
type Item struct {
	// Identity and classification.
	ID       ItemID   `json:"id" yaml:"id"`
	Type     ItemType `json:"type" yaml:"type"`
	Title    string   `json:"title" yaml:"title"`
	Status   Status   `json:"status,omitempty" yaml:"status,omitempty"`
	Priority Priority `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Hierarchy. Parent is the owning epic of a story, or the owning story (or
	// epic) of a task. Epic is the deprecated alias kept for files that use it.
	Parent    ItemID `json:"parent,omitempty" yaml:"parent,omitempty"`
	Epic      ItemID `json:"epic,omitempty" yaml:"epic,omitempty"`
	Milestone ItemID `json:"milestone,omitempty" yaml:"milestone,omitempty"`
	Sprint    string `json:"sprint,omitempty" yaml:"sprint,omitempty"`

	// People and labels.
	Assignees []string `json:"assignees,omitempty" yaml:"assignees,omitempty"`
	Author    string   `json:"author,omitempty" yaml:"author,omitempty"`
	Owner     string   `json:"owner,omitempty" yaml:"owner,omitempty"` // milestones only
	Labels    []string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Numbers. Estimate is in story points, Effort and Spent in hours.
	Estimate *float64 `json:"estimate,omitempty" yaml:"estimate,omitempty"`
	Effort   *float64 `json:"effort,omitempty" yaml:"effort,omitempty"`
	Spent    *float64 `json:"spent,omitempty" yaml:"spent,omitempty"`

	// Dates. Start is the milestone-only counterpart of Due.
	Created Timestamp `json:"created,omitempty" yaml:"created,omitempty"`
	Updated Timestamp `json:"updated,omitempty" yaml:"updated,omitempty"`
	Started Timestamp `json:"started,omitempty" yaml:"started,omitempty"`
	Closed  Timestamp `json:"closed,omitempty" yaml:"closed,omitempty"`
	Start   Date      `json:"start,omitempty" yaml:"start,omitempty"`
	Due     Date      `json:"due,omitempty" yaml:"due,omitempty"`

	// Relations and extras.
	Links       []Link         `json:"links,omitempty" yaml:"links,omitempty"`
	Attachments []string       `json:"attachments,omitempty" yaml:"attachments,omitempty"`
	Custom      map[string]any `json:"custom,omitempty" yaml:"custom,omitempty"`
	Deleted     bool           `json:"deleted,omitempty" yaml:"deleted,omitempty"`

	// Extra holds every front-matter key this version does not know, including
	// the "x-" keys reserved for third-party tools (R-CF-4). It is written back
	// verbatim so that a newer file is never damaged by an older binary.
	Extra map[string]any `json:"extra,omitempty" yaml:"-"`

	// Derived fields. None of them is stored in the file.
	Body string `json:"body" yaml:"-"` // Markdown after the front matter
	Path string `json:"path" yaml:"-"` // vault-relative, forward slashes
	Rev  Rev    `json:"rev" yaml:"-"`  // content hash of the canonical bytes
}

// Comment is one entry under .pmngr/comments/<ITEM-ID>/. Comments have no id of
// their own: they are addressed as "<ITEM-ID>#<file-stem>" (R-ID-4).
type Comment struct {
	Item        ItemID              `json:"item" yaml:"item"`
	Author      string              `json:"author" yaml:"author"`
	Created     Timestamp           `json:"created,omitempty" yaml:"created,omitempty"`
	Updated     Timestamp           `json:"updated,omitempty" yaml:"updated,omitempty"`
	InReplyTo   string              `json:"inReplyTo,omitempty" yaml:"in_reply_to,omitempty"`
	Kind        CommentKind         `json:"kind,omitempty" yaml:"kind,omitempty"`
	Reactions   map[string][]string `json:"reactions,omitempty" yaml:"reactions,omitempty"`
	Attachments []string            `json:"attachments,omitempty" yaml:"attachments,omitempty"`

	// Extra preserves unknown keys, as on Item.
	Extra map[string]any `json:"extra,omitempty" yaml:"-"`

	// Derived fields.
	Body string `json:"body" yaml:"-"`
	Path string `json:"path" yaml:"-"`
	Rev  Rev    `json:"rev" yaml:"-"`
}

// CommentKind distinguishes human comments from machine-written entries.
type CommentKind string

// The comment kinds.
const (
	CommentKindComment      CommentKind = "comment"
	CommentKindStatusChange CommentKind = "status_change"
	CommentKindSystem       CommentKind = "system"
)

// Valid reports whether k is one of the known comment kinds.
func (k CommentKind) Valid() bool {
	switch k {
	case CommentKindComment, CommentKindStatusChange, CommentKindSystem:
		return true
	default:
		return false
	}
}

// Ref returns the "<ITEM-ID>#<file-stem>" reference of the comment, derived from
// its path. It returns the empty string when the comment has no path.
func (c *Comment) Ref() string {
	if c.Path == "" {
		return ""
	}
	stem := c.Path
	if i := strings.LastIndex(stem, "/"); i >= 0 {
		stem = stem[i+1:]
	}
	stem = strings.TrimSuffix(stem, ".md")
	return fmt.Sprintf("%s#%s", c.Item, stem)
}
