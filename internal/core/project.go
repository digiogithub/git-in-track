package core

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SupportedSchema is the highest project.yaml schema version this build writes.
// A file declaring a higher version is readable but MUST NOT be written back
// (docs/03 section 6.1).
const SupportedSchema = 1

// ProjectFileName is the discovery marker of a project backlog (R-LOC-2).
const ProjectFileName = "project.yaml"

// BacklogDirName is the folder that holds the backlog inside the docs folder.
const BacklogDirName = ".pmngr"

// ProjectConfig is the parsed form of .pmngr/project.yaml.
type ProjectConfig struct {
	Schema       int                       `yaml:"schema"`
	Key          ProjectKey                `yaml:"key"`
	Name         string                    `yaml:"name"`
	Description  string                    `yaml:"description,omitempty"`
	Timezone     string                    `yaml:"timezone,omitempty"`
	Docs         DocsConfig                `yaml:"docs"`
	Workflow     Workflow                  `yaml:"workflow"`
	IDAllocation IDAllocation              `yaml:"id_allocation"`
	Labels       []Label                   `yaml:"labels,omitempty"`
	Priorities   []Priority                `yaml:"priorities,omitempty"`
	Estimation   Estimation                `yaml:"estimation"`
	Defaults     map[ItemType]ItemDefaults `yaml:"defaults,omitempty"`
	CustomFields []CustomField             `yaml:"custom_fields,omitempty"`
	People       []Person                  `yaml:"people,omitempty"`
	Team         *TeamRef                  `yaml:"team,omitempty"`
	Links        *LinksConfig              `yaml:"links,omitempty"`
}

// DocsConfig holds the knowledge-base rendering settings.
type DocsConfig struct {
	Path           string `yaml:"path,omitempty"`
	Wikilinks      bool   `yaml:"wikilinks"`
	Mermaid        bool   `yaml:"mermaid"`
	Math           bool   `yaml:"math"`
	Footnotes      bool   `yaml:"footnotes"`
	Callouts       bool   `yaml:"callouts"`
	AttachmentsDir string `yaml:"attachments_dir,omitempty"`
}

// Workflow is the status machine of a project.
type Workflow struct {
	Initial     Status              `yaml:"initial,omitempty"`
	Statuses    []StatusDef         `yaml:"statuses"`
	Transitions map[Status][]Status `yaml:"transitions,omitempty"`
}

// StatusDef is one declared status.
type StatusDef struct {
	ID       Status         `yaml:"id"`
	Name     string         `yaml:"name,omitempty"`
	Category StatusCategory `yaml:"category"`
	WIP      int            `yaml:"wip,omitempty"`
	Color    string         `yaml:"color,omitempty"`
	Terminal bool           `yaml:"terminal,omitempty"`
}

// IDAllocation configures how new ids are handed out. Counters are hints: the
// scan of the backlog always wins (docs/03 section 4.1).
type IDAllocation struct {
	Strategy      string                 `yaml:"strategy,omitempty"`
	WriteCounters bool                   `yaml:"write_counters"`
	Counters      map[ItemType]int       `yaml:"counters,omitempty"`
	Reserved      map[ItemType][]IDRange `yaml:"reserved,omitempty"`
	Redirects     map[ItemID]ItemID      `yaml:"redirects,omitempty"`
}

// IDRange is an inclusive range of item numbers reserved by one person while
// working offline. It is written as a two-element sequence: [200, 249].
type IDRange struct {
	From int
	To   int
}

// UnmarshalYAML decodes the [from, to] form.
func (r *IDRange) UnmarshalYAML(node *yaml.Node) error {
	var pair []int
	if err := node.Decode(&pair); err != nil {
		return fmt.Errorf("decode id range: %w", err)
	}
	if len(pair) != 2 {
		return fmt.Errorf("decode id range: want [from, to], got %d values", len(pair))
	}
	r.From, r.To = pair[0], pair[1]
	return nil
}

// MarshalYAML emits the [from, to] form.
func (r IDRange) MarshalYAML() (any, error) {
	return []int{r.From, r.To}, nil
}

// Label is one entry of the label catalog.
type Label struct {
	Name        string `yaml:"name"`
	Color       string `yaml:"color,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// Estimation configures story points and hour tracking.
type Estimation struct {
	Scale      string    `yaml:"scale,omitempty"`
	Values     []float64 `yaml:"values,omitempty"`
	TrackHours bool      `yaml:"track_hours,omitempty"`
}

// ItemDefaults are the values materialized into a new item of a given type.
// They are applied at creation time only, never at read time (R-DEFAULT).
type ItemDefaults struct {
	Status    Status   `yaml:"status,omitempty"`
	Priority  Priority `yaml:"priority,omitempty"`
	Assignees []string `yaml:"assignees,omitempty"`
	Labels    []string `yaml:"labels,omitempty"`
}

// CustomField declares an extra front-matter field stored under custom:.
type CustomField struct {
	Key         string     `yaml:"key"`
	Type        string     `yaml:"type"`
	Values      []string   `yaml:"values,omitempty"`
	Items       string     `yaml:"items,omitempty"`
	AppliesTo   []ItemType `yaml:"applies_to,omitempty"`
	Default     any        `yaml:"default,omitempty"`
	Description string     `yaml:"description,omitempty"`
}

// Person is an optional local mirror of a team member.
type Person struct {
	Handle string `yaml:"handle"`
	Name   string `yaml:"name,omitempty"`
	Email  string `yaml:"email,omitempty"`
	Kind   string `yaml:"kind,omitempty"`
}

// TeamRef points at the team repository this project belongs to.
type TeamRef struct {
	Repo string `yaml:"repo,omitempty"`
	Key  string `yaml:"key,omitempty"`
}

// LinksConfig describes the git host, used to build blob URLs.
type LinksConfig struct {
	Host   string `yaml:"host,omitempty"`
	WebURL string `yaml:"web_url,omitempty"`
}

// DefaultProjectConfig returns a configuration with every documented default
// applied. Unmarshalling on top of it leaves absent keys at their default.
func DefaultProjectConfig() ProjectConfig {
	return ProjectConfig{
		Schema:   SupportedSchema,
		Timezone: "UTC",
		Docs: DocsConfig{
			Wikilinks:      true,
			Mermaid:        true,
			Math:           false,
			Footnotes:      true,
			Callouts:       true,
			AttachmentsDir: ".pmngr/attachments",
		},
		IDAllocation: IDAllocation{Strategy: "scan", WriteCounters: true},
		Priorities:   []Priority{PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow},
		Estimation:   Estimation{Scale: "fibonacci"},
	}
}

// LoadProjectConfig parses project.yaml and validates it.
//
// It returns the parsed configuration even when validation fails, so that a
// caller can fall back to read-only mode on an unsupported schema instead of
// losing the file (docs/03 section 6.3). The error, when not nil, joins one
// *DiagnosticError per E-PROJ-* rule that was violated.
func LoadProjectConfig(data []byte) (*ProjectConfig, error) {
	cfg := DefaultProjectConfig()
	// Schema defaults to "declared" only when the file omits it, which is an
	// error; start from zero so the rule can fire.
	cfg.Schema = 0
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", ProjectFileName, err)
	}
	var errs []error
	for _, d := range cfg.Validate() {
		if d.Severity == SeverityError {
			errs = append(errs, &DiagnosticError{Diagnostic: d})
		}
	}
	if len(errs) > 0 {
		return &cfg, errors.Join(errs...)
	}
	return &cfg, nil
}

// DiagnosticError adapts a Diagnostic to the error interface.
type DiagnosticError struct {
	Diagnostic Diagnostic
}

// Error implements the error interface.
func (e *DiagnosticError) Error() string { return e.Diagnostic.String() }

// Validate applies the rules of docs/03 section 6.3 and returns every finding,
// errors and warnings alike, in a deterministic order.
func (p *ProjectConfig) Validate() []Diagnostic {
	var out []Diagnostic
	add := func(code Code, sev Severity, field, msg string) {
		out = append(out, Diagnostic{Code: code, Severity: sev, Path: ProjectFileName, Field: field, Message: msg})
	}

	switch {
	case p.Schema == 0:
		add(CodeProjSchema, SeverityError, "schema", "missing")
	case p.Schema > SupportedSchema:
		add(CodeProjSchema, SeverityError, "schema",
			fmt.Sprintf("schema %d is newer than the supported version %d; open read-only", p.Schema, SupportedSchema))
	}

	if !ValidProjectKey(p.Key) {
		add(CodeProjKey, SeverityError, "key",
			fmt.Sprintf("%q does not match [A-Z][A-Z0-9]{1,9}", p.Key))
	}

	seen := make(map[Status]bool, len(p.Workflow.Statuses))
	hasDone := false
	if len(p.Workflow.Statuses) == 0 {
		add(CodeProjInitial, SeverityError, "workflow.statuses", "no status is declared")
	}
	for _, s := range p.Workflow.Statuses {
		if seen[s.ID] {
			add(CodeProjStatusDup, SeverityError, "workflow.statuses", fmt.Sprintf("duplicate status %q", s.ID))
			continue
		}
		seen[s.ID] = true
		if !s.Category.Valid() {
			add(CodeProjStatusCategory, SeverityError, "workflow.statuses",
				fmt.Sprintf("status %q has unknown category %q", s.ID, s.Category))
		}
		if s.Category == CategoryDone {
			hasDone = true
		}
	}
	if len(p.Workflow.Statuses) > 0 && !hasDone {
		add(CodeWarnNoDone, SeverityWarning, "workflow.statuses", "no status has category done; metrics will be meaningless")
	}
	if p.Workflow.Initial != "" && !seen[p.Workflow.Initial] {
		add(CodeProjInitial, SeverityError, "workflow.initial",
			fmt.Sprintf("%q is not a declared status", p.Workflow.Initial))
	}
	for from, targets := range p.Workflow.Transitions {
		if !seen[from] {
			add(CodeProjTransitionTarget, SeverityError, "workflow.transitions",
				fmt.Sprintf("%q is not a declared status", from))
		}
		for _, to := range targets {
			if !seen[to] {
				add(CodeProjTransitionTarget, SeverityError, "workflow.transitions",
					fmt.Sprintf("transition %q -> %q names an unknown status", from, to))
			}
		}
	}

	labels := make(map[string]bool, len(p.Labels))
	for _, l := range p.Labels {
		name := strings.ToLower(l.Name)
		if labels[name] {
			add(CodeWarnLabelDup, SeverityWarning, "labels", fmt.Sprintf("duplicate label %q", l.Name))
			continue
		}
		labels[name] = true
	}

	sortDiagnostics(out)
	return out
}

// sortDiagnostics orders findings by field then message so that two runs over
// the same file report them identically, whatever the map iteration order was.
func sortDiagnostics(d []Diagnostic) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && diagLess(d[j], d[j-1]); j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

func diagLess(a, b Diagnostic) bool {
	if a.Field != b.Field {
		return a.Field < b.Field
	}
	return a.Message < b.Message
}

// StatusDef returns the declaration of a status.
func (p *ProjectConfig) StatusDef(id Status) (StatusDef, bool) {
	for _, s := range p.Workflow.Statuses {
		if s.ID == id {
			return s, true
		}
	}
	return StatusDef{}, false
}

// InitialStatus returns the status a new item starts in: workflow.initial when
// set, otherwise the first declared status.
func (p *ProjectConfig) InitialStatus() Status {
	if p.Workflow.Initial != "" {
		return p.Workflow.Initial
	}
	if len(p.Workflow.Statuses) > 0 {
		return p.Workflow.Statuses[0].ID
	}
	return ""
}

// CategoryOf returns the coarse bucket of a status, or the empty category when
// the status is not declared.
func (p *ProjectConfig) CategoryOf(id Status) StatusCategory {
	if s, ok := p.StatusDef(id); ok {
		return s.Category
	}
	return ""
}

// HasLabel reports whether a label is declared in the catalog, comparing
// case-insensitively (R-LBL-2).
func (p *ProjectConfig) HasLabel(name string) bool {
	for _, l := range p.Labels {
		if strings.EqualFold(l.Name, name) {
			return true
		}
	}
	return false
}
