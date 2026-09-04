package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SprintsDirName is the folder of the team `.pmngr/` that holds sprints
// (docs/04 section 8).
const SprintsDirName = "sprints"

// SprintTypeCode is the type code of a sprint id, between the team key and the
// number: `ACME-TEAM-S-0007`.
const SprintTypeCode = "S"

// SprintState is the lifecycle of a sprint (docs/04 section 8.2).
type SprintState string

// The three sprint states. A board scopes to the sprint its `sprint:` names,
// whatever its state; exactly one sprint per board should be active.
const (
	SprintPlanned SprintState = "planned"
	SprintActive  SprintState = "active"
	SprintClosed  SprintState = "closed"
)

// Valid reports whether s is one of the known sprint states.
func (s SprintState) Valid() bool {
	return s == SprintPlanned || s == SprintActive || s == SprintClosed
}

// sprintIDRE splits a sprint id from the right, because a team key may contain
// hyphens: everything before the last `-S-` is the team key (docs/04 8.1).
var sprintIDRE = regexp.MustCompile(`^([A-Z][A-Z0-9-]{1,15})-S-(\d{4,})$`)

// ParseSprintID decodes `<TEAMKEY>-S-<NNNN>`.
func ParseSprintID(s string) (TeamKey, int, error) {
	m := sprintIDRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", 0, fmt.Errorf("sprint id %q: want <TEAMKEY>-S-<NNNN>", s)
	}
	number, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, fmt.Errorf("sprint id %q: %w", s, err)
	}
	return TeamKey(m[1]), number, nil
}

// FormatSprintID renders a sprint id, zero-padded to four digits.
func FormatSprintID(key TeamKey, number int) string {
	return fmt.Sprintf("%s-%s-%04d", key, SprintTypeCode, number)
}

// Sprint is the parsed form of `.pmngr/sprints/<SPRINT-ID>.md`: a goal, a date
// range and the references that make up the scope (docs/04 section 8).
//
// Like a board it holds no item state. `items` is a list of
// `<projectKey>/<itemId>` references, one per line, and membership is team-repo
// state: it stays editable for an item whose project nobody cloned (R-SPR-2).
type Sprint struct {
	ID    string      `yaml:"id" json:"id"`
	Type  string      `yaml:"type" json:"type"`
	Title string      `yaml:"title,omitempty" json:"title,omitempty"`
	Board string      `yaml:"board" json:"board"`
	State SprintState `yaml:"state" json:"state"`
	Start Date        `yaml:"start" json:"start"`
	End   Date        `yaml:"end" json:"end"`
	Goal  string      `yaml:"goal,omitempty" json:"goal,omitempty"`
	// Committed is the scope as it stood when the sprint started; `items` may
	// grow afterwards, which is what tells commitment from mid-sprint additions
	// (R-SPR-1).
	Committed      []string  `yaml:"committed,omitempty" json:"committed,omitempty"`
	Items          []string  `yaml:"items" json:"items"`
	CapacityHours  *float64  `yaml:"capacity_hours,omitempty" json:"capacityHours,omitempty"`
	VelocityTarget *float64  `yaml:"velocity_target,omitempty" json:"velocityTarget,omitempty"`
	Participants   []string  `yaml:"participants,omitempty" json:"participants,omitempty"`
	Retro          string    `yaml:"retro,omitempty" json:"retro,omitempty"`
	Created        Timestamp `yaml:"created,omitempty" json:"created,omitempty"`
	Updated        Timestamp `yaml:"updated,omitempty" json:"updated,omitempty"`
	Author         string    `yaml:"author,omitempty" json:"author,omitempty"`

	// Extra preserves the front-matter keys this version does not model, so
	// that an older binary never damages a newer file.
	Extra map[string]any `yaml:"-" json:"extra,omitempty"`

	// Derived fields, never stored in the file.
	Body string `yaml:"-" json:"body"`
	Path string `yaml:"-" json:"path"`
	Rev  Rev    `yaml:"-" json:"rev"`
}

// sprintKnownKeys is the set of front-matter keys Sprint models.
var sprintKnownKeys = map[string]bool{
	"id": true, "type": true, "title": true, "board": true, "state": true,
	"start": true, "end": true, "goal": true, "committed": true, "items": true,
	"capacity_hours": true, "velocity_target": true, "participants": true,
	"retro": true, "created": true, "updated": true, "author": true,
}

// DisplayTitle returns the sprint title, defaulting to `Sprint <n>` as
// docs/04 section 8.2 prescribes.
func (s *Sprint) DisplayTitle() string {
	if strings.TrimSpace(s.Title) != "" {
		return s.Title
	}
	if _, number, err := ParseSprintID(s.ID); err == nil {
		return fmt.Sprintf("Sprint %d", number)
	}
	return s.ID
}

// Has reports whether a reference is in the sprint scope.
func (s *Sprint) Has(ref string) bool {
	for _, existing := range s.Items {
		if existing == ref {
			return true
		}
	}
	return false
}

// Members returns the scope as a set, which is what scopes a scrum board.
func (s *Sprint) Members() map[string]bool {
	out := make(map[string]bool, len(s.Items))
	for _, ref := range s.Items {
		out[ref] = true
	}
	return out
}

// AddItem appends a reference to the scope and reports whether it was new.
// `committed` is left untouched: an item added mid-sprint is not part of the
// commitment (R-SPR-1).
func (s *Sprint) AddItem(ref string) bool {
	if s.Has(ref) {
		return false
	}
	s.Items = append(s.Items, ref)
	return true
}

// RemoveItem drops a reference from the scope and reports whether it was there.
func (s *Sprint) RemoveItem(ref string) bool {
	kept := make([]string, 0, len(s.Items))
	found := false
	for _, existing := range s.Items {
		if existing == ref {
			found = true
			continue
		}
		kept = append(kept, existing)
	}
	s.Items = kept
	return found
}

// TotalDays is the length of the sprint in days, both ends inclusive.
func (s *Sprint) TotalDays() int {
	if s.Start.IsZero() || s.End.IsZero() || s.End.Before(s.Start.Time) {
		return 0
	}
	return int(s.End.Sub(s.Start.Time).Hours()/24) + 1
}

// RemainingDays is how many days of the sprint are left at now, both ends
// inclusive. It is 0 for a sprint that is over and the full length for one that
// has not started.
func (s *Sprint) RemainingDays(now time.Time) int {
	if s.End.IsZero() || now.IsZero() {
		return 0
	}
	today := NewDate(now)
	if today.After(s.End.Time) {
		return 0
	}
	if !s.Start.IsZero() && today.Before(s.Start.Time) {
		return s.TotalDays()
	}
	return int(s.End.Sub(today.Time).Hours()/24) + 1
}

// Overlaps reports whether two sprints of the same board cover a common day.
func (s *Sprint) Overlaps(other *Sprint) bool {
	if s == nil || other == nil || s.ID == other.ID || s.Board != other.Board {
		return false
	}
	if s.Start.IsZero() || s.End.IsZero() || other.Start.IsZero() || other.End.IsZero() {
		return false
	}
	return !s.Start.After(other.End.Time) && !other.Start.After(s.End.Time)
}

// ParseSprint decodes a sprint file. Like ParseBoard it reports one
// *ParseError carrying the diagnostic code, and fills the derived Path and Rev.
func ParseSprint(filePath string, data []byte) (*Sprint, error) {
	block, body, err := SplitFrontMatter(data)
	if err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMMissing, "front matter is missing or unterminated", err)
	}
	var s Sprint
	if err := yaml.Unmarshal(block, &s); err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMYAML, "front matter is not valid YAML", err)
	}
	fm := map[string]any{}
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMYAML, "front matter is not a mapping", err)
	}
	for key, value := range fm {
		if sprintKnownKeys[key] {
			continue
		}
		if s.Extra == nil {
			s.Extra = map[string]any{}
		}
		s.Extra[key] = value
	}
	if s.Type == "" {
		s.Type = "sprint"
	}
	if s.State == "" {
		s.State = SprintPlanned
	}
	s.Body = body
	s.Path = filePath
	s.Rev = ComputeRev(data)
	return &s, nil
}

// SerializeSprint renders a sprint back to file bytes in the key order of
// docs/04 section 8.2, with `committed` and `items` one reference per line so
// that two people editing different sprints — or the same sprint's goal and its
// scope — produce diffs that merge (R-SPR-1).
func SerializeSprint(s *Sprint) ([]byte, error) {
	if s == nil {
		return nil, errors.New("serialize sprint: nil sprint")
	}
	w := &fmWriter{}
	w.scalar("id", s.ID)
	w.scalar("type", s.Type)
	w.scalar("title", s.Title)
	w.scalar("board", s.Board)
	w.scalar("state", string(s.State))
	w.date("start", s.Start)
	w.date("end", s.End)
	w.scalar("goal", s.Goal)
	w.number("capacity_hours", s.CapacityHours)
	w.number("velocity_target", s.VelocityTarget)
	w.stringList("participants", s.Participants)
	writeRefBlock(w, "committed", s.Committed)
	writeRefBlock(w, "items", s.Items)
	w.scalar("retro", s.Retro)
	w.timestamp("created", s.Created)
	w.timestamp("updated", s.Updated)
	w.scalar("author", s.Author)
	if err := w.extra(s.Extra); err != nil {
		return nil, fmt.Errorf("serialize sprint %s: %w", s.Path, err)
	}
	return assemble(w.String(), s.Body), nil
}

// writeRefBlock emits a reference list one entry per line, and nothing at all
// when the list is empty — except for `items`, which is required and therefore
// always emitted, as `[]` when the scope is deliberately empty.
func writeRefBlock(w *fmWriter, key string, refs []string) {
	if len(refs) == 0 {
		if key == "items" {
			w.b.WriteString("items: []\n")
		}
		return
	}
	w.b.WriteString(key + ":\n")
	for _, ref := range refs {
		w.b.WriteString("  - " + yamlFlowString(ref) + "\n")
	}
}

// SprintValidateInput is the context the file-level validation needs. Every
// field is optional: a rule whose input is missing is skipped rather than
// guessed, exactly as a board's validation does.
type SprintValidateInput struct {
	// TeamKey is the key of team.yaml; a sprint id must carry it.
	TeamKey TeamKey
	// Boards is every board id of the team repository.
	Boards []string
	// Declared is every project key of team.yaml.
	Declared []ProjectKey
	// Known reports the references that resolve in a cloned project. A nil map
	// skips the dead-reference rule, which is what a workspace with no clone of
	// the project does.
	Known map[string]bool
	// Cloned is the set of projects an open repository serves; a reference into
	// a project nobody cloned is never dead, it is remote.
	Cloned map[ProjectKey]bool
}

// Validate applies the rules of docs/04 section 8.4 to one sprint file.
func (s *Sprint) Validate(in SprintValidateInput) []Diagnostic {
	var out []Diagnostic
	add := func(code Code, sev Severity, field, msg string) {
		out = append(out, Diagnostic{Code: code, Severity: sev, Path: s.Path, Field: field, Message: msg})
	}

	stem := boardStem(s.Path)
	key, _, err := ParseSprintID(s.ID)
	switch {
	case s.ID == "":
		add(CodeSprintID, SeverityError, "id", "missing")
	case err != nil:
		add(CodeSprintID, SeverityError, "id", err.Error())
	case stem != "" && stem != s.ID:
		add(CodeSprintID, SeverityError, "id",
			fmt.Sprintf("id %q does not match the file name %q", s.ID, stem))
	case in.TeamKey != "" && key != in.TeamKey:
		add(CodeSprintID, SeverityError, "id",
			fmt.Sprintf("id %q carries the team key %s, but this repository is %s", s.ID, key, in.TeamKey))
	}

	if !s.State.Valid() {
		add(CodeSprintState, SeverityError, "state",
			fmt.Sprintf("%q is not planned, active or closed", s.State))
	}

	switch {
	case s.Start.IsZero():
		add(CodeSprintDates, SeverityError, "start", "missing")
	case s.End.IsZero():
		add(CodeSprintDates, SeverityError, "end", "missing")
	case s.End.Before(s.Start.Time):
		add(CodeSprintDates, SeverityError, "end",
			fmt.Sprintf("%s is before the start %s", s.End, s.Start))
	}

	switch {
	case strings.TrimSpace(s.Board) == "":
		add(CodeSprintBoard, SeverityError, "board", "missing")
	case len(in.Boards) > 0 && !containsString(in.Boards, s.Board):
		add(CodeSprintBoard, SeverityError, "board",
			fmt.Sprintf("board %q does not exist in this team repository", s.Board))
	}

	for _, field := range []struct {
		name string
		refs []string
	}{{"items", s.Items}, {"committed", s.Committed}} {
		for _, raw := range field.refs {
			ref, err := ParseRef(raw)
			if err != nil {
				add(CodeBoardRefFormat, SeverityWarning, field.name, err.Error())
				continue
			}
			if len(in.Declared) > 0 && !containsKey(in.Declared, ref.Project) {
				add(CodeSprintRefUnknownProject, SeverityWarning, field.name,
					fmt.Sprintf("ref %s names the undeclared project %s", raw, ref.Project))
				continue
			}
			if in.Known != nil && in.Cloned[ref.Project] && !in.Known[ref.String()] {
				add(CodeSprintRefDead, SeverityWarning, field.name,
					fmt.Sprintf("ref %s does not resolve in the clone of %s", raw, ref.Project))
			}
		}
	}

	sortDiagnostics(out)
	return out
}

// ValidateSprintSet applies the rules that need every sprint of a board at
// once: two active sprints and overlapping date ranges (docs/04 section 8.4).
func ValidateSprintSet(sprints []*Sprint) []Diagnostic {
	var out []Diagnostic
	byBoard := map[string][]*Sprint{}
	for _, s := range sprints {
		if s == nil || s.Board == "" {
			continue
		}
		byBoard[s.Board] = append(byBoard[s.Board], s)
	}
	boards := make([]string, 0, len(byBoard))
	for board := range byBoard {
		boards = append(boards, board)
	}
	sort.Strings(boards)
	for _, board := range boards {
		list := byBoard[board]
		sort.SliceStable(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		var active []string
		for _, s := range list {
			if s.State == SprintActive {
				active = append(active, s.ID)
			}
		}
		if len(active) > 1 {
			out = append(out, Diagnostic{
				Code: CodeSprintTwoActive, Severity: SeverityWarning, Field: "state",
				Message: fmt.Sprintf("board %s has %d active sprints (%s); only one should be active",
					board, len(active), strings.Join(active, ", ")),
			})
		}
		for i := 0; i < len(list); i++ {
			for j := i + 1; j < len(list); j++ {
				if !list[i].Overlaps(list[j]) {
					continue
				}
				out = append(out, Diagnostic{
					Code: CodeSprintOverlap, Severity: SeverityWarning, Path: list[j].Path, Field: "start",
					Message: fmt.Sprintf("sprint %s (%s to %s) overlaps %s (%s to %s) on board %s",
						list[j].ID, list[j].Start, list[j].End,
						list[i].ID, list[i].Start, list[i].End, board),
				})
			}
		}
	}
	sortDiagnostics(out)
	return out
}

// OverlappingSprint returns the first sprint of the same board whose dates
// overlap the candidate, which is what a create or a date change is refused
// with (docs/04 section 8.4, GIT-US-0018).
func OverlappingSprint(candidate *Sprint, others []*Sprint) *Sprint {
	for _, other := range others {
		if candidate.Overlaps(other) {
			return other
		}
	}
	return nil
}

// containsString reports whether list holds value.
func containsString(list []string, value string) bool {
	for _, entry := range list {
		if entry == value {
			return true
		}
	}
	return false
}

// SprintStore reads and writes the sprints of a team repository. It is the only
// place that touches `.pmngr/sprints/`, and it goes through core.FS so that the
// browser and the companion share one implementation.
type SprintStore struct {
	fs  FS
	dir string
	// Clock supplies the `updated` stamp of a write. Nil means "leave it".
	Clock Clock
}

// NewSprintStore returns a store over the `sprints/` folder of a team
// `.pmngr/`. teamDir is TeamRef.TeamDirPath.
func NewSprintStore(fsys FS, teamDir string) *SprintStore {
	return &SprintStore{fs: fsys, dir: joinPath(teamDir, SprintsDirName)}
}

// Dir returns the vault-relative folder the store reads.
func (s *SprintStore) Dir() string { return s.dir }

// PathOf returns the file a sprint id is stored in.
func (s *SprintStore) PathOf(id string) string { return joinPath(s.dir, id+".md") }

// List returns every sprint of the team repository, sorted by id. A file that
// does not parse is skipped and reported through the returned diagnostics
// rather than failing the whole listing.
func (s *SprintStore) List(ctx context.Context) ([]*Sprint, []Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, wrapContext("sprint list", err)
	}
	entries, err := s.fs.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", s.dir, err)
	}
	var sprints []*Sprint
	var diags []Diagnostic
	for _, e := range entries {
		if e.IsDir || !isMarkdown(e.Name) {
			continue
		}
		full := joinPath(s.dir, e.Name)
		data, err := s.fs.ReadFile(full)
		if err != nil {
			diags = append(diags, Diagnostic{
				Code: CodeSprintID, Severity: SeverityError, Path: full,
				Message: fmt.Sprintf("cannot read the sprint: %v", err),
			})
			continue
		}
		sprint, err := ParseSprint(full, data)
		if err != nil {
			diags = append(diags, diagnosticOf(err, full))
			continue
		}
		sprints = append(sprints, sprint)
	}
	sort.SliceStable(sprints, func(i, j int) bool { return sprints[i].ID < sprints[j].ID })
	return sprints, diags, nil
}

// Get reads one sprint by its id.
func (s *SprintStore) Get(ctx context.Context, id string) (*Sprint, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("sprint get", err)
	}
	full := s.PathOf(id)
	data, err := s.fs.ReadFile(full)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, fmt.Errorf("sprint %s: %w", id, ErrItemNotFound)
		}
		return nil, fmt.Errorf("read %s: %w", full, err)
	}
	return ParseSprint(full, data)
}

// NextID allocates the next sprint id of a team: the maximum number in
// `sprints/` plus one, exactly as a project allocates an item id (docs/04 8.1).
func (s *SprintStore) NextID(ctx context.Context, team TeamKey) (string, error) {
	sprints, _, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	highest := 0
	for _, sprint := range sprints {
		if _, number, err := ParseSprintID(sprint.ID); err == nil && number > highest {
			highest = number
		}
	}
	return FormatSprintID(team, highest+1), nil
}

// Write persists a sprint, enforcing the optimistic lock when expected is not
// empty. It returns the sprint with its new rev.
func (s *SprintStore) Write(ctx context.Context, sprint *Sprint, expected Rev) (*Sprint, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("sprint write", err)
	}
	if sprint == nil {
		return nil, errors.New("sprint write: nil sprint")
	}
	full := sprint.Path
	if full == "" {
		full = s.PathOf(sprint.ID)
		sprint.Path = full
	}
	if expected != "" {
		current, err := s.fs.ReadFile(full)
		if err != nil && !errors.Is(err, ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", full, err)
		}
		if err == nil {
			if got := ComputeRev(current); got != expected {
				return nil, &StaleRevisionError{Path: full, Expected: expected, Current: got}
			}
		}
	}
	if s.Clock != nil {
		sprint.Updated = NewTimestamp(s.Clock.Now())
	}
	data, err := SerializeSprint(sprint)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(s.fs, full, data); err != nil {
		return nil, fmt.Errorf("write %s: %w", full, err)
	}
	sprint.Rev = ComputeRev(data)
	return sprint, nil
}
