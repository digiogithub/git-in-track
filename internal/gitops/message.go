package gitops

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// DefaultTemplate is the shipped commit-message template
// (docs/06-git-sync.md section 3.3).
const DefaultTemplate = `pmngr: update {{.ItemID}} "{{.Title}}"`

// SubjectLimit is the length a subject line is truncated to. The full title
// stays in the body, so nothing is lost.
const SubjectLimit = 72

// ToolName is the `Tool:` trailer prefix. The caller appends its version.
const ToolName = "gintrack"

// Action is what a write did, as the `{{.Action}}` placeholder reports it.
type Action string

// The actions of docs/06 section 3.3.
const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionMove    Action = "move"
	ActionComment Action = "comment"
)

// String renders the action.
func (a Action) String() string {
	if a == "" {
		return string(ActionUpdate)
	}
	return string(a)
}

// Fields is the template context of docs/06 section 3.3 and docs/07 section
// 6.4. Every field is a plain string so that a template can never fail on a nil
// dereference; Count is the only number and is 1 for a single-item write.
type Fields struct {
	ItemID     string `json:"itemId,omitempty"`
	Title      string `json:"title,omitempty"`
	Type       string `json:"type,omitempty"`
	Status     string `json:"status,omitempty"`
	PrevStatus string `json:"prevStatus,omitempty"`
	ProjectKey string `json:"projectKey,omitempty"`
	Board      string `json:"board,omitempty"`
	Action     Action `json:"action,omitempty"`
	Count      int    `json:"count,omitempty"`
	User       string `json:"user,omitempty"`
	Date       string `json:"date,omitempty"`
	// Tool is the `Tool:` trailer value, for example
	// "gintrack 0.4.1 (companion)". It is not a template placeholder.
	Tool string `json:"tool,omitempty"`
	// Agent is the `Agent:` trailer written when the change came from the MCP
	// server (docs/07 section 6.4). Empty for a human edit.
	Agent string `json:"agent,omitempty"`
	// Extra carries trailers a caller wants beyond the standard set.
	Extra map[string]string `json:"extra,omitempty"`
}

// bulk reports whether the fields describe several items at once, which is what
// a bulk status change or a `updateMany` produces.
func (f Fields) bulk() bool { return f.Count > 1 && f.ItemID == "" }

// Template is a parsed, reusable commit-message template.
type Template struct {
	text string
	tpl  *template.Template
}

// Text returns the template source, so the settings surface can echo back what
// is configured.
func (t *Template) Text() string {
	if t == nil {
		return DefaultTemplate
	}
	return t.text
}

// ParseTemplate compiles a commit-message template. An empty text compiles the
// shipped default.
//
// Two spellings are accepted for the same data, because both are documented:
// the Go text/template field form (`{{.ItemID}}`, docs/06 section 3.3) and the
// short lowercase form (`{{action}} {{id}}: {{title}}`, story GIT-US-0020),
// which is implemented as niladic template functions over the same fields.
func ParseTemplate(text string) (*Template, error) {
	if strings.TrimSpace(text) == "" {
		text = DefaultTemplate
	}
	// The functions are re-bound per render; parsing only needs their names to
	// exist, so a placeholder map with the right arity is enough here.
	tpl, err := template.New("commit").Funcs(aliasFuncs(Fields{})).Parse(text)
	if err != nil {
		return nil, wrap("template", CodeTemplateInvalid, err,
			"the commit message template does not parse")
	}
	return &Template{text: text, tpl: tpl}, nil
}

// MustParseTemplate is ParseTemplate for a template known to be valid, such as
// the shipped default. It panics on a broken one, which can only be a bug.
func MustParseTemplate(text string) *Template {
	t, err := ParseTemplate(text)
	if err != nil {
		panic(err)
	}
	return t
}

// aliasFuncs binds the short lowercase placeholders to the fields.
func aliasFuncs(f Fields) template.FuncMap {
	return template.FuncMap{
		"id":         func() string { return f.ItemID },
		"title":      func() string { return f.Title },
		"type":       func() string { return f.Type },
		"status":     func() string { return f.Status },
		"prevStatus": func() string { return f.PrevStatus },
		"project":    func() string { return f.ProjectKey },
		"board":      func() string { return f.Board },
		"action":     func() string { return f.Action.String() },
		"count":      func() int { return f.count() },
		"user":       func() string { return f.User },
		"date":       func() string { return f.Date },
	}
}

// count is Count with the 1 a single-item write leaves implicit.
func (f Fields) count() int {
	if f.Count < 1 {
		return 1
	}
	return f.Count
}

// Render produces the commit message: the subject from the template and the
// body from the machine-readable trailers of docs/06 section 3.3.
//
// The subject never contains a newline and is truncated to SubjectLimit; a
// truncated title is repeated in full in the body, so the information survives.
func (t *Template) Render(f Fields) (Message, error) {
	if t == nil {
		t = MustParseTemplate(DefaultTemplate)
	}
	f = f.normalise()

	subject, err := t.subject(f)
	if err != nil {
		return Message{}, err
	}

	body := trailers(f)
	if len(subject) > SubjectLimit {
		body = append([]string{"Title: " + f.Title, ""}, body...)
		subject = truncate(subject, SubjectLimit)
	}
	return Message{Subject: subject, Body: strings.Join(body, "\n")}, nil
}

// subject renders the template, or the built-in bulk subject when the fields
// describe several items and therefore have no single id to interpolate.
func (t *Template) subject(f Fields) (string, error) {
	if f.bulk() {
		return fmt.Sprintf("pmngr: %s %d items", f.Action.String(), f.count()), nil
	}
	var buf bytes.Buffer
	tpl, err := t.tpl.Clone()
	if err != nil {
		return "", wrap("template", CodeTemplateInvalid, err, "the commit message template could not be prepared")
	}
	if err := tpl.Funcs(aliasFuncs(f)).Execute(&buf, f); err != nil {
		return "", wrap("template", CodeTemplateInvalid, err,
			"the commit message template does not render")
	}
	subject := collapse(buf.String())
	if subject == "" {
		return "", failf("template", CodeTemplateInvalid,
			"the commit message template rendered an empty subject")
	}
	return subject, nil
}

// normalise fills the defaults a template may rely on.
func (f Fields) normalise() Fields {
	if f.Action == "" {
		f.Action = ActionUpdate
	}
	if f.Count < 1 {
		f.Count = 1
	}
	if f.Date == "" {
		f.Date = time.Now().UTC().Format("2006-01-02")
	}
	return f
}

// trailers builds the machine-readable body: the fixed keys of docs/06 in a
// fixed order, then any extra key sorted, so a message is reproducible.
func trailers(f Fields) []string {
	var out []string
	add := func(key, value string) {
		if value != "" {
			out = append(out, key+": "+value)
		}
	}
	add("Item", f.ItemID)
	add("Type", f.Type)
	switch {
	case f.PrevStatus != "" && f.Status != "" && f.PrevStatus != f.Status:
		add("Status", f.PrevStatus+" -> "+f.Status)
	default:
		add("Status", f.Status)
	}
	add("Project", f.ProjectKey)
	add("Board", f.Board)
	if f.Count > 1 {
		add("Items", strconv.Itoa(f.Count))
	}
	add("Tool", f.Tool)
	add("Agent", f.Agent)

	keys := make([]string, 0, len(f.Extra))
	for k := range f.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		add(k, f.Extra[k])
	}
	return out
}

// collapse folds a rendered subject into one line and trims it.
func collapse(in string) string {
	in = strings.ReplaceAll(in, "\r\n", " ")
	in = strings.ReplaceAll(in, "\n", " ")
	in = strings.ReplaceAll(in, "\r", " ")
	return strings.TrimSpace(strings.Join(strings.Fields(in), " "))
}

// truncate shortens a subject to n characters, ending it with a single ellipsis
// so that the reader can tell it was cut.
func truncate(in string, n int) string {
	runes := []rune(in)
	if len(runes) <= n {
		return in
	}
	return strings.TrimRight(string(runes[:n-1]), " ") + "…"
}
