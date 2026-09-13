package youtrack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Millis is a YouTrack timestamp: milliseconds since the Unix epoch, or JSON
// null for "not set".
type Millis int64

// UnmarshalJSON accepts a number or null.
func (m *Millis) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*m = 0
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("youtrack: decoding a timestamp: %w", err)
	}
	*m = Millis(n)
	return nil
}

// IsZero reports whether the timestamp was absent or null.
func (m Millis) IsZero() bool { return m == 0 }

// Time converts to UTC. A zero Millis converts to the zero time.
func (m Millis) Time() time.Time {
	if m == 0 {
		return time.Time{}
	}
	return time.UnixMilli(int64(m)).UTC()
}

// User is the subset of a YouTrack user this package requests.
type User struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	FullName  string `json:"fullName"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl"`
	Type      string `json:"$type"`
}

// Project is a YouTrack project. ShortName is the key used in issue ids and in
// the "project: {KEY}" query clause.
type Project struct {
	ID          string `json:"id"`
	ShortName   string `json:"shortName"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	Type        string `json:"$type"`
}

// FieldType names the kind of a custom field, for example "enum[1]".
type FieldType struct {
	ID string `json:"id"`
}

// CustomFieldDefinition is the instance-wide definition a project setting
// points at.
type CustomFieldDefinition struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	FieldType FieldType `json:"fieldType"`
}

// Bundle identifies the set of allowed values behind an enum, state or version
// field. Type is the "$type" discriminator, for example "VersionBundle".
type Bundle struct {
	ID   string `json:"id"`
	Type string `json:"$type"`
}

// CustomFieldSetting is one entry of a project's customFieldSettings: which
// field the project uses and which bundle backs it.
type CustomFieldSetting struct {
	ID             string                `json:"id"`
	Field          CustomFieldDefinition `json:"field"`
	Bundle         Bundle                `json:"bundle"`
	CanBeEmpty     bool                  `json:"canBeEmpty"`
	EmptyFieldText string                `json:"emptyFieldText"`
	Type           string                `json:"$type"`
}

// VersionValue is one value of a version bundle. Version bundles are the
// closest YouTrack analog to a git-in-track milestone; sprints are
// board-scoped scheduling buckets and are a different concept.
type VersionValue struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate Millis `json:"releaseDate"`
	Released    bool   `json:"released"`
	Archived    bool   `json:"archived"`
	Type        string `json:"$type"`
}

// FieldValue is the decoded form of one custom-field value. YouTrack returns a
// different shape per field kind, so every optional key is present here and the
// ones the server did not send stay empty. Scalar holds the raw JSON for simple
// fields (text, integer, float, date) that are not objects.
type FieldValue struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Login         string          `json:"login"`
	FullName      string          `json:"fullName"`
	LocalizedName string          `json:"localizedName"`
	Presentation  string          `json:"presentation"`
	IDReadable    string          `json:"idReadable"`
	Text          string          `json:"text"`
	Minutes       *int            `json:"minutes"`
	IsResolved    *bool           `json:"isResolved"`
	Type          string          `json:"$type"`
	Scalar        json.RawMessage `json:"-"`
}

// CustomFieldValue keeps the raw JSON of a custom-field value, because the
// shape depends on the field kind and lossy flattening belongs to the caller,
// not here.
type CustomFieldValue struct {
	Raw json.RawMessage
}

// UnmarshalJSON stores the raw bytes.
func (v *CustomFieldValue) UnmarshalJSON(b []byte) error {
	v.Raw = append(v.Raw[:0], b...)
	return nil
}

// MarshalJSON returns the raw bytes, or null when there are none.
func (v CustomFieldValue) MarshalJSON() ([]byte, error) {
	if len(v.Raw) == 0 {
		return []byte("null"), nil
	}
	return v.Raw, nil
}

// IsNull reports whether the field has no value.
func (v CustomFieldValue) IsNull() bool {
	trimmed := bytes.TrimSpace(v.Raw)
	return len(trimmed) == 0 || string(trimmed) == "null"
}

// IsMulti reports whether the value is a JSON array, which is how multi-value
// fields come back.
func (v CustomFieldValue) IsMulti() bool {
	trimmed := bytes.TrimSpace(v.Raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}

// Values decodes the value into zero or more FieldValue entries: none for null,
// one for an object or a scalar, and one per element for a multi-value field.
func (v CustomFieldValue) Values() ([]FieldValue, error) {
	if v.IsNull() {
		return nil, nil
	}
	trimmed := bytes.TrimSpace(v.Raw)
	if trimmed[0] == '[' {
		var raws []json.RawMessage
		if err := json.Unmarshal(trimmed, &raws); err != nil {
			return nil, fmt.Errorf("youtrack: decoding a multi-value custom field: %w", err)
		}
		out := make([]FieldValue, 0, len(raws))
		for _, raw := range raws {
			one, err := decodeFieldValue(raw)
			if err != nil {
				return nil, err
			}
			out = append(out, one)
		}
		return out, nil
	}
	one, err := decodeFieldValue(trimmed)
	if err != nil {
		return nil, err
	}
	return []FieldValue{one}, nil
}

// decodeFieldValue decodes one element, falling back to Scalar for anything
// that is not a JSON object.
func decodeFieldValue(raw json.RawMessage) (FieldValue, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return FieldValue{Scalar: append(json.RawMessage(nil), trimmed...)}, nil
	}
	var out FieldValue
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return FieldValue{}, fmt.Errorf("youtrack: decoding a custom-field value: %w", err)
	}
	return out, nil
}

// CustomField is one custom field of an issue.
type CustomField struct {
	ID    string           `json:"id"`
	Name  string           `json:"name"`
	Type  string           `json:"$type"`
	Value CustomFieldValue `json:"value"`
}

// IssueRef is the narrow issue shape returned inside links.
type IssueRef struct {
	ID         string `json:"id"`
	IDReadable string `json:"idReadable"`
	Summary    string `json:"summary"`
}

// LinkType describes a kind of issue link. Aggregation marks the
// hierarchy-forming type, which is "Subtask" on a default instance.
type LinkType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SourceToTarget string `json:"sourceToTarget"`
	TargetToSource string `json:"targetToSource"`
	Directed       bool   `json:"directed"`
	Aggregation    bool   `json:"aggregation"`
}

// Link directions as YouTrack spells them.
const (
	DirectionOutward = "OUTWARD"
	DirectionInward  = "INWARD"
	DirectionBoth    = "BOTH"
)

// IssueLink is one (linkType, direction) pair of an issue. YouTrack returns one
// entry per pair including the pairs that hold no issues, so most entries of a
// real response have an empty Issues slice; see NonEmptyLinks.
type IssueLink struct {
	ID        string     `json:"id"`
	Direction string     `json:"direction"`
	LinkType  LinkType   `json:"linkType"`
	Issues    []IssueRef `json:"issues"`
}

// NonEmptyLinks drops the (linkType, direction) pairs that carry no issues. A
// single "relates to" link comes back among roughly twenty empty entries, so
// every caller needs this.
func NonEmptyLinks(links []IssueLink) []IssueLink {
	out := make([]IssueLink, 0, len(links))
	for _, link := range links {
		if len(link.Issues) > 0 {
			out = append(out, link)
		}
	}
	return out
}

// Issue is a YouTrack issue as returned by the wide field selector.
// Description is untrusted third-party Markdown: sanitize before rendering.
type Issue struct {
	ID           string        `json:"id"`
	IDReadable   string        `json:"idReadable"`
	Summary      string        `json:"summary"`
	Description  string        `json:"description"`
	Created      Millis        `json:"created"`
	Updated      Millis        `json:"updated"`
	Resolved     Millis        `json:"resolved"`
	Project      Project       `json:"project"`
	Reporter     User          `json:"reporter"`
	CustomFields []CustomField `json:"customFields"`
	Links        []IssueLink   `json:"links"`
	Tags         []Tag         `json:"tags"`
	Type         string        `json:"$type"`
}

// CustomField returns the field with the given name, and whether it was found.
func (i Issue) CustomField(name string) (CustomField, bool) {
	for _, field := range i.CustomFields {
		if field.Name == name {
			return field, true
		}
	}
	return CustomField{}, false
}

// Tag is an issue tag.
type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Comment is an issue comment. Text is untrusted third-party Markdown.
type Comment struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Created Millis `json:"created"`
	Updated Millis `json:"updated"`
	Author  User   `json:"author"`
	Deleted bool   `json:"deleted"`
	Type    string `json:"$type"`
}

// ArticleRef is the narrow article shape used for the knowledge-base tree.
type ArticleRef struct {
	ID          string `json:"id"`
	IDReadable  string `json:"idReadable"`
	Summary     string `json:"summary"`
	Ordinal     int    `json:"ordinal"`
	HasChildren bool   `json:"hasChildren"`
}

// Article is a knowledge-base article. Content is the article body in Markdown
// and is untrusted third-party content: sanitize before rendering. Note that
// the body field is "content" here, not "description" as on an issue.
type Article struct {
	ID            string     `json:"id"`
	IDReadable    string     `json:"idReadable"`
	Summary       string     `json:"summary"`
	Content       string     `json:"content"`
	Ordinal       int        `json:"ordinal"`
	Created       Millis     `json:"created"`
	Updated       Millis     `json:"updated"`
	HasChildren   bool       `json:"hasChildren"`
	Project       Project    `json:"project"`
	ParentArticle ArticleRef `json:"parentArticle"`
	Reporter      User       `json:"reporter"`
	Type          string     `json:"$type"`
}

// Attachment is a file attached to an issue or an article. URL is a signed and
// usually relative URL; resolve it with Client.AttachmentURL.
type Attachment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mimeType"`
	Extension string `json:"extension"`
	Charset   string `json:"charset"`
	Created   Millis `json:"created"`
	URL       string `json:"url"`
	Author    User   `json:"author"`
	Type      string `json:"$type"`
}
