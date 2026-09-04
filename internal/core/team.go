package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SupportedTeamSchema is the highest team.yaml schema version this build
// understands. It is independent of a project's schema (docs/04 section 3.1).
const SupportedTeamSchema = 1

// TeamFileName is the discovery marker of a team repository (R-TEAM-LOC-1).
const TeamFileName = "team.yaml"

// DefaultKnowledgePath is the team knowledge-base folder when team.yaml
// declares none (docs/04 section 3.1).
const DefaultKnowledgePath = "knowledge"

// TeamKey is the prefix of every sprint and retrospective id of a team. Unlike
// a project key it may contain hyphens, e.g. "ACME-TEAM".
type TeamKey string

// String returns the key as a plain string.
func (k TeamKey) String() string { return string(k) }

// teamKeyRE is the grammar of a team key: [A-Z][A-Z0-9-]{1,15}.
var teamKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9-]{1,15}$`)

// ValidTeamKey reports whether k matches the team-key grammar.
func ValidTeamKey(k TeamKey) bool { return teamKeyRE.MatchString(string(k)) }

// memberHandleRE is the grammar of a member handle (R-MEM-1).
var memberHandleRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidMemberHandle reports whether h matches the member-handle grammar.
func ValidMemberHandle(h string) bool { return memberHandleRE.MatchString(h) }

// TeamConfig is the parsed form of team.yaml, the routing table of the whole
// product: everything the app knows about other repositories comes from here
// (docs/04 section 3).
type TeamConfig struct {
	Schema      int               `yaml:"schema"`
	Key         TeamKey           `yaml:"key"`
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Timezone    string            `yaml:"timezone,omitempty"`
	Knowledge   DocsConfig        `yaml:"knowledge"`
	Members     []Member          `yaml:"members"`
	Projects    []TeamProject     `yaml:"projects"`
	Defaults    TeamDefaults      `yaml:"defaults,omitempty"`
	Cadence     Cadence           `yaml:"cadence,omitempty"`
	Policies    map[string]string `yaml:"policies,omitempty"`
	Snapshots   SnapshotPolicy    `yaml:"snapshots,omitempty"`
}

// Member is one person, bot or agent of the team (docs/04 section 3.2).
type Member struct {
	Handle   string            `yaml:"handle" json:"handle"`
	Name     string            `yaml:"name,omitempty" json:"name,omitempty"`
	Role     string            `yaml:"role,omitempty" json:"role,omitempty"`
	Emails   []string          `yaml:"emails,omitempty" json:"emails,omitempty"`
	GitNames []string          `yaml:"git_names,omitempty" json:"gitNames,omitempty"`
	Handles  map[string]string `yaml:"handles,omitempty" json:"handles,omitempty"`
	Capacity float64           `yaml:"capacity,omitempty" json:"capacity,omitempty"`
	Active   bool              `yaml:"active" json:"active"`
}

// UnmarshalYAML decodes a member, defaulting `active` to true: a member is on
// the team unless the file says otherwise (R-MEM-3).
func (m *Member) UnmarshalYAML(node *yaml.Node) error {
	type rawMember Member
	raw := rawMember{Active: true}
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("decode member: %w", err)
	}
	*m = Member(raw)
	return nil
}

// TeamProject is one project repository the team owns (docs/04 section 3.3).
type TeamProject struct {
	Key           ProjectKey `yaml:"key" json:"key"`
	Name          string     `yaml:"name" json:"name"`
	Repo          string     `yaml:"repo" json:"repo"`
	DefaultBranch string     `yaml:"default_branch,omitempty" json:"defaultBranch,omitempty"`
	DocsPath      string     `yaml:"docs_path" json:"docsPath"`
	Host          string     `yaml:"host,omitempty" json:"host,omitempty"`
	WebURL        string     `yaml:"web_url,omitempty" json:"webUrl,omitempty"`
	Color         string     `yaml:"color,omitempty" json:"color,omitempty"`
	Archived      bool       `yaml:"archived,omitempty" json:"archived,omitempty"`
	LocalHints    []string   `yaml:"local_hints,omitempty" json:"localHints,omitempty"`
}

// Branch returns the branch snapshot links and blob URLs are built against,
// defaulting to "main" (docs/04 section 3.3).
func (p TeamProject) Branch() string {
	if p.DefaultBranch == "" {
		return "main"
	}
	return p.DefaultBranch
}

// TeamDefaults are the board and capacity defaults of a team.
type TeamDefaults struct {
	Board               string  `yaml:"board,omitempty" json:"board,omitempty"`
	SprintLengthDays    int     `yaml:"sprint_length_days,omitempty" json:"sprintLengthDays,omitempty"`
	CapacityHoursPerDay float64 `yaml:"capacity_hours_per_day,omitempty" json:"capacityHoursPerDay,omitempty"`
}

// Cadence describes the sprint rhythm of a team.
type Cadence struct {
	SprintLengthDays   int    `yaml:"sprint_length_days,omitempty" json:"sprintLengthDays,omitempty"`
	SprintStartWeekday string `yaml:"sprint_start_weekday,omitempty" json:"sprintStartWeekday,omitempty"`
	RetroAfterSprint   bool   `yaml:"retro_after_sprint,omitempty" json:"retroAfterSprint,omitempty"`
}

// SnapshotPolicy is the `snapshots` mapping of team.yaml (docs/04 section 6).
// The snapshots themselves are read and written by GIT-US-0019; this build only
// carries the policy so that the UI can explain what a remote card will show.
type SnapshotPolicy struct {
	Enabled       bool `yaml:"enabled" json:"enabled"`
	MaxAgeDays    int  `yaml:"max_age_days,omitempty" json:"maxAgeDays,omitempty"`
	IncludeClosed bool `yaml:"include_closed,omitempty" json:"includeClosed,omitempty"`
}

// DefaultTeamConfig returns a configuration with every documented default
// applied. Unmarshalling on top of it leaves absent keys at their default.
func DefaultTeamConfig() TeamConfig {
	return TeamConfig{
		Schema:   SupportedTeamSchema,
		Timezone: "UTC",
		Knowledge: DocsConfig{
			Path:      DefaultKnowledgePath,
			Wikilinks: true,
			Mermaid:   true,
			Footnotes: true,
			Callouts:  true,
		},
		Snapshots: SnapshotPolicy{Enabled: true, MaxAgeDays: 7},
	}
}

// LoadTeamConfig parses team.yaml and validates it.
//
// Like LoadProjectConfig it returns the parsed configuration even when
// validation fails, so that a caller can open the team repository read-only on
// an unsupported schema instead of losing the file. The error, when not nil,
// joins one *DiagnosticError per E-TEAM-* rule that was violated.
func LoadTeamConfig(data []byte) (*TeamConfig, error) {
	cfg := DefaultTeamConfig()
	// Schema defaults to "declared" only when the file omits it, which is an
	// error; start from zero so the rule can fire.
	cfg.Schema = 0
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", TeamFileName, err)
	}
	if cfg.Knowledge.Path == "" {
		cfg.Knowledge.Path = DefaultKnowledgePath
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

// Validate applies the rules of docs/04 section 3.5 and returns every finding,
// errors and warnings alike, in a deterministic order.
//
// The rules that need the file system (E-TEAM-BACKLOG-IN-TEAM-REPO) or a local
// clone (W-TEAM-KEY-MISMATCH, W-TEAM-HINT-DEAD) are not checked here: they
// belong to DiscoverTeam and to the workspace that opens the clones.
func (t *TeamConfig) Validate() []Diagnostic {
	var out []Diagnostic
	add := func(code Code, sev Severity, field, msg string) {
		out = append(out, Diagnostic{Code: code, Severity: sev, Path: TeamFileName, Field: field, Message: msg})
	}

	switch {
	case t.Schema == 0:
		add(CodeTeamSchema, SeverityError, "schema", "missing")
	case t.Schema > SupportedTeamSchema:
		add(CodeTeamSchema, SeverityError, "schema",
			fmt.Sprintf("schema %d is newer than the supported version %d; open read-only", t.Schema, SupportedTeamSchema))
	}

	if !ValidTeamKey(t.Key) {
		add(CodeTeamKey, SeverityError, "key",
			fmt.Sprintf("%q does not match [A-Z][A-Z0-9-]{1,15}", t.Key))
	}
	if strings.TrimSpace(t.Name) == "" {
		add(CodeTeamKey, SeverityError, "name", "missing")
	}

	handles := make(map[string]bool, len(t.Members))
	emails := make(map[string]string, len(t.Members))
	for _, m := range t.Members {
		if !ValidMemberHandle(m.Handle) {
			add(CodeTeamMemberFields, SeverityError, "members",
				fmt.Sprintf("handle %q does not match [a-z0-9][a-z0-9-]{0,31}", m.Handle))
			continue
		}
		if handles[m.Handle] {
			add(CodeTeamHandleDup, SeverityError, "members", fmt.Sprintf("duplicate member handle %q", m.Handle))
			continue
		}
		handles[m.Handle] = true
		for _, raw := range m.Emails {
			email := strings.ToLower(strings.TrimSpace(raw))
			if email == "" {
				continue
			}
			if owner, taken := emails[email]; taken {
				add(CodeTeamEmailDup, SeverityError, "members",
					fmt.Sprintf("email %q is claimed by both %q and %q", email, owner, m.Handle))
				continue
			}
			emails[email] = m.Handle
		}
	}
	if len(t.Members) == 0 {
		add(CodeTeamMemberFields, SeverityError, "members", "no member is declared")
	}

	keys := make(map[ProjectKey]bool, len(t.Projects))
	for _, p := range t.Projects {
		switch {
		case p.Key == "":
			add(CodeTeamProjectFields, SeverityError, "projects", "a project entry has no key")
			continue
		case !ValidProjectKey(p.Key):
			add(CodeTeamProjectFields, SeverityError, "projects",
				fmt.Sprintf("project key %q does not match [A-Z][A-Z0-9]{1,9}", p.Key))
			continue
		}
		if keys[p.Key] {
			add(CodeTeamKeyDup, SeverityError, "projects", fmt.Sprintf("duplicate project key %q", p.Key))
			continue
		}
		keys[p.Key] = true
		for field, value := range map[string]string{"name": p.Name, "repo": p.Repo, "docs_path": p.DocsPath} {
			if strings.TrimSpace(value) == "" {
				add(CodeTeamProjectFields, SeverityError, "projects",
					fmt.Sprintf("project %s has no %s", p.Key, field))
			}
		}
		if p.WebURL == "" && !strings.HasPrefix(p.Repo, "https://") {
			add(CodeTeamWebURL, SeverityWarning, "projects",
				fmt.Sprintf("project %s has no web_url and none can be derived from %q; blob links are disabled", p.Key, p.Repo))
		}
	}
	if len(t.Projects) == 0 {
		add(CodeTeamProjectFields, SeverityError, "projects", "no project is declared")
	}

	sortDiagnostics(out)
	return out
}

// Project returns the declaration of a project key.
func (t *TeamConfig) Project(key ProjectKey) (TeamProject, bool) {
	for _, p := range t.Projects {
		if p.Key == key {
			return p, true
		}
	}
	return TeamProject{}, false
}

// Member returns the member with the given handle.
func (t *TeamConfig) Member(handle string) (Member, bool) {
	for _, m := range t.Members {
		if m.Handle == handle {
			return m, true
		}
	}
	return Member{}, false
}

// MemberByEmail maps a git commit identity onto a handle (R-MEM-2). The
// comparison is case-insensitive because git identities are not normalised.
func (t *TeamConfig) MemberByEmail(email string) (Member, bool) {
	want := strings.ToLower(strings.TrimSpace(email))
	if want == "" {
		return Member{}, false
	}
	for _, m := range t.Members {
		for _, candidate := range m.Emails {
			if strings.EqualFold(strings.TrimSpace(candidate), want) {
				return m, true
			}
		}
	}
	return Member{}, false
}

// KnowledgePath returns the team knowledge-base folder, never empty.
func (t *TeamConfig) KnowledgePath() string {
	if t.Knowledge.Path == "" {
		return DefaultKnowledgePath
	}
	return t.Knowledge.Path
}

// Ref is a cross-repository reference to one backlog item: the string
// `<projectKey>/<itemId>` a board card, a sprint scope entry or a team-KB
// wikilink is made of (docs/04 sections 1 and 5).
type Ref struct {
	Project ProjectKey `json:"project"`
	Item    ItemID     `json:"item"`
}

// String renders the reference in its canonical `<KEY>/<ITEM-ID>` form.
func (r Ref) String() string { return string(r.Project) + "/" + string(r.Item) }

// ParseRef decodes `<projectKey>/<itemId>`.
//
// The project key must be the item id's own prefix: a reference that disagrees
// with itself would resolve differently depending on which half is trusted, so
// it is rejected instead of guessed.
func ParseRef(s string) (Ref, error) {
	raw := strings.TrimSpace(s)
	key, id, found := strings.Cut(raw, "/")
	if !found {
		return Ref{}, fmt.Errorf("parse ref %q: want <projectKey>/<itemId>", s)
	}
	if !ValidProjectKey(ProjectKey(key)) {
		return Ref{}, fmt.Errorf("parse ref %q: %q is not a project key", s, key)
	}
	idKey, _, _, err := ParseItemID(id)
	if err != nil {
		return Ref{}, fmt.Errorf("parse ref %q: %w", s, err)
	}
	if string(idKey) != key {
		return Ref{}, fmt.Errorf("parse ref %q: item %s does not belong to project %s", s, id, key)
	}
	return Ref{Project: ProjectKey(key), Item: ItemID(id)}, nil
}
