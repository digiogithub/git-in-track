package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file is the retrospective half of the team repository: the parsed form
// of `.pmngr/retros/<RETRO-ID>-<slug>.md`, its byte-stable emitter and the
// store that reads and writes the folder (docs/04 section 9).
//
// A retro hangs off a closed sprint the way a sprint hangs off a board: the
// file holds no item state, only team-repo state plus references. The one piece
// of item state a retro can create is an improvement action promoted into a
// task, and even then the task lives in its own project repository and the
// retro keeps nothing but the reference (R-RETRO-2).

// RetrosDirName is the folder of the team `.pmngr/` that holds retrospectives
// (docs/04 section 9).
const RetrosDirName = "retros"

// RetroTypeCode is the type code of a retro id, between the team key and the
// number: `ACME-TEAM-R-0007`.
const RetroTypeCode = "R"

// RetroState is the facilitation stage of a retro (docs/04 section 9.2). It
// drives the UI mode and nothing else: every state accepts every write, because
// a retro that refuses a late note loses the note.
type RetroState string

// The four retro states, in the order a session walks them.
const (
	RetroCollecting RetroState = "collecting"
	RetroVoting     RetroState = "voting"
	RetroDiscussing RetroState = "discussing"
	RetroClosed     RetroState = "closed"
)

// Valid reports whether s is one of the known retro states.
func (s RetroState) Valid() bool {
	return s == RetroCollecting || s == RetroVoting || s == RetroDiscussing || s == RetroClosed
}

// RetroCategory is the column a note and its theme belong to.
type RetroCategory string

// The three collection categories of docs/04 section 9.1.
const (
	CategoryWentWell  RetroCategory = "went_well"
	CategoryToImprove RetroCategory = "to_improve"
	CategoryPuzzle    RetroCategory = "puzzle"
)

// Valid reports whether c is one of the three categories.
func (c RetroCategory) Valid() bool {
	return c == CategoryWentWell || c == CategoryToImprove || c == CategoryPuzzle
}

// retroCategories is the canonical order the body sections are written in.
var retroCategories = []RetroCategory{CategoryWentWell, CategoryToImprove, CategoryPuzzle}

// retroSectionTitles maps a category to the body heading that carries it.
var retroSectionTitles = map[RetroCategory]string{
	CategoryWentWell:  "Went well",
	CategoryToImprove: "To improve",
	CategoryPuzzle:    "Puzzles",
}

// RetroActionStatus is the retro-local bookkeeping of an improvement action.
// Once `task` is set the task's own status in the project repository is the
// truth; `done` here is the fallback for an action that was never promoted
// (R-RETRO-1).
type RetroActionStatus string

// The four action states of docs/04 section 9.2.
const (
	ActionProposed RetroActionStatus = "proposed"
	ActionPromoted RetroActionStatus = "promoted"
	ActionDone     RetroActionStatus = "done"
	ActionDropped  RetroActionStatus = "dropped"
)

// Valid reports whether s is one of the known action states.
func (s RetroActionStatus) Valid() bool {
	return s == ActionProposed || s == ActionPromoted || s == ActionDone || s == ActionDropped
}

// DefaultVotesPerPerson is the vote budget of a retro that declares none
// (docs/04 section 9.2).
const DefaultVotesPerPerson = 3

// retroIDRE splits a retro id from the right, because a team key may contain
// hyphens: everything before the last `-R-` is the team key (docs/04 9.1).
var retroIDRE = regexp.MustCompile(`^([A-Z][A-Z0-9-]{1,15})-R-(\d{4,})$`)

// ParseRetroID decodes `<TEAMKEY>-R-<NNNN>`.
func ParseRetroID(s string) (TeamKey, int, error) {
	m := retroIDRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", 0, fmt.Errorf("retro id %q: want <TEAMKEY>-R-<NNNN>", s)
	}
	number, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, fmt.Errorf("retro id %q: %w", s, err)
	}
	return TeamKey(m[1]), number, nil
}

// FormatRetroID renders a retro id, zero-padded to four digits.
func FormatRetroID(key TeamKey, number int) string {
	return fmt.Sprintf("%s-%s-%04d", key, RetroTypeCode, number)
}

// retroLocalIDRE is the shape of a note, theme or action id: unique within one
// retro file and short enough to read inside a body bullet.
var retroLocalIDRE = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

// RetroTheme groups the notes the room merged into one topic (step 2 of
// docs/04 section 9.1).
type RetroTheme struct {
	ID       string        `yaml:"id" json:"id"`
	Title    string        `yaml:"title" json:"title"`
	Category RetroCategory `yaml:"category,omitempty" json:"category,omitempty"`
	// Notes are the ids of the body bullets this theme absorbed.
	Notes []string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// RetroAction is one improvement action: a real, trackable item with an
// accountable owner, not a line of prose (R-RETRO-4).
type RetroAction struct {
	ID    string `yaml:"id" json:"id"`
	Title string `yaml:"title" json:"title"`
	// Owner is the single accountable handle. An action without one is the
	// most common way retro actions die, so it is a warning (R-RETRO-4).
	Owner string `yaml:"owner,omitempty" json:"owner,omitempty"`
	Due   Date   `yaml:"due,omitempty" json:"due,omitempty"`
	// Theme is the theme the action came out of, when it came out of one.
	Theme string `yaml:"theme,omitempty" json:"theme,omitempty"`
	// Task is the `<projectKey>/<itemId>` reference promotion produced. Empty
	// until the action is promoted (R-RETRO-2).
	Task   string            `yaml:"task,omitempty" json:"task,omitempty"`
	Status RetroActionStatus `yaml:"status,omitempty" json:"status,omitempty"`
	Note   string            `yaml:"note,omitempty" json:"note,omitempty"`
}

// State returns the action's status, defaulting to `proposed`.
func (a RetroAction) State() RetroActionStatus {
	if a.Status == "" {
		return ActionProposed
	}
	return a.Status
}

// Settled reports an action that needs no further follow-up from the retro's
// own point of view. A promoted action is never settled here: its task decides
// (R-RETRO-1).
func (a RetroAction) Settled() bool {
	return a.State() == ActionDone || a.State() == ActionDropped
}

// RetroNote is one sticky note. Notes live in the body, one bullet per line, so
// that two people adding notes at the same time produce diffs that merge
// (docs/04 section 9.1, step 1).
type RetroNote struct {
	// ID is the `(n1)` prefix of the bullet. It is empty for a bullet somebody
	// typed by hand without one, which stays exactly as it was written.
	ID       string        `json:"id,omitempty"`
	Category RetroCategory `json:"category"`
	Text     string        `json:"text"`
	// Author is the trailing `— handle`, absent on an anonymous retro.
	Author string `json:"author,omitempty"`
}

// Retro is the parsed form of `.pmngr/retros/<RETRO-ID>-<slug>.md`
// (docs/04 section 9).
type Retro struct {
	ID    string `yaml:"id" json:"id"`
	Type  string `yaml:"type" json:"type"`
	Title string `yaml:"title" json:"title"`
	// Sprint is the closed sprint this retro reviews. It is absent for a
	// cadence-independent retro, such as an incident retro.
	Sprint       string     `yaml:"sprint,omitempty" json:"sprint,omitempty"`
	Board        string     `yaml:"board,omitempty" json:"board,omitempty"`
	Date         Date       `yaml:"date" json:"date"`
	Facilitator  string     `yaml:"facilitator,omitempty" json:"facilitator,omitempty"`
	Participants []string   `yaml:"participants,omitempty" json:"participants,omitempty"`
	State        RetroState `yaml:"state" json:"state"`
	// Anonymous stops the UI recording note authorship.
	Anonymous      bool `yaml:"anonymous,omitempty" json:"anonymous,omitempty"`
	VotesPerPerson *int `yaml:"votes_per_person,omitempty" json:"votesPerPerson,omitempty"`
	// CarriedFrom is the previous retro whose actions are reviewed here; it is
	// what chains the team's memory together (R-RETRO-5).
	CarriedFrom string       `yaml:"carried_from,omitempty" json:"carriedFrom,omitempty"`
	Themes      []RetroTheme `yaml:"themes,omitempty" json:"themes,omitempty"`
	// Votes maps a theme id to the handles that voted for it.
	Votes   map[string][]string `yaml:"votes,omitempty" json:"votes,omitempty"`
	Actions []RetroAction       `yaml:"actions,omitempty" json:"actions,omitempty"`
	Created Timestamp           `yaml:"created,omitempty" json:"created,omitempty"`
	Updated Timestamp           `yaml:"updated,omitempty" json:"updated,omitempty"`
	Author  string              `yaml:"author,omitempty" json:"author,omitempty"`

	// Extra preserves the front-matter keys this version does not model, so
	// that an older binary never damages a newer file.
	Extra map[string]any `yaml:"-" json:"extra,omitempty"`

	// Derived fields, never stored as front matter.

	// Notes are the body bullets of the three collection sections, in the order
	// the file lists them.
	Notes []RetroNote `yaml:"-" json:"notes"`
	// body is the parsed body, kept so that serializing re-renders the notes
	// and the action checklist while preserving every other section verbatim.
	body retroBody
	Body string `yaml:"-" json:"body"`
	Path string `yaml:"-" json:"path"`
	Rev  Rev    `yaml:"-" json:"rev"`
}

// retroKnownKeys is the set of front-matter keys Retro models.
var retroKnownKeys = map[string]bool{
	"id": true, "type": true, "title": true, "sprint": true, "board": true,
	"date": true, "facilitator": true, "participants": true, "state": true,
	"anonymous": true, "votes_per_person": true, "carried_from": true,
	"themes": true, "votes": true, "actions": true,
	"created": true, "updated": true, "author": true,
}

// DisplayTitle returns the retro title, defaulting to `Retro <n>`.
func (r *Retro) DisplayTitle() string {
	if strings.TrimSpace(r.Title) != "" {
		return r.Title
	}
	if _, number, err := ParseRetroID(r.ID); err == nil {
		return fmt.Sprintf("Retro %d", number)
	}
	return r.ID
}

// VoteBudget is how many votes each participant may cast.
func (r *Retro) VoteBudget() int {
	if r.VotesPerPerson == nil || *r.VotesPerPerson <= 0 {
		return DefaultVotesPerPerson
	}
	return *r.VotesPerPerson
}

// Action returns the action with an id, and whether there is one.
func (r *Retro) Action(id string) (*RetroAction, bool) {
	for i := range r.Actions {
		if r.Actions[i].ID == id {
			return &r.Actions[i], true
		}
	}
	return nil, false
}

// Theme returns the theme with an id, and whether there is one.
func (r *Retro) Theme(id string) (*RetroTheme, bool) {
	for i := range r.Themes {
		if r.Themes[i].ID == id {
			return &r.Themes[i], true
		}
	}
	return nil, false
}

// Note returns the note with an id, and whether there is one.
func (r *Retro) Note(id string) (*RetroNote, bool) {
	for i := range r.Notes {
		if r.Notes[i].ID != "" && r.Notes[i].ID == id {
			return &r.Notes[i], true
		}
	}
	return nil, false
}

// NextNoteID allocates the next free `nN` id of this retro.
func (r *Retro) NextNoteID() string {
	used := make([]string, 0, len(r.Notes))
	for _, n := range r.Notes {
		used = append(used, n.ID)
	}
	return nextLocalID("n", used)
}

// NextThemeID allocates the next free `tN` id of this retro.
func (r *Retro) NextThemeID() string {
	used := make([]string, 0, len(r.Themes))
	for _, t := range r.Themes {
		used = append(used, t.ID)
	}
	return nextLocalID("t", used)
}

// NextActionID allocates the next free `aN` id of this retro.
func (r *Retro) NextActionID() string {
	used := make([]string, 0, len(r.Actions))
	for _, a := range r.Actions {
		used = append(used, a.ID)
	}
	return nextLocalID("a", used)
}

// nextLocalID returns `<prefix><max+1>` over the ids that already use the
// prefix. An id somebody typed by hand in another shape is simply skipped.
func nextLocalID(prefix string, used []string) string {
	highest := 0
	for _, id := range used {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimPrefix(id, prefix)); err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("%s%d", prefix, highest+1)
}

// VoteCount is how many votes a theme received.
func (r *Retro) VoteCount(theme string) int { return len(r.Votes[theme]) }

// VotesCast is how many votes a handle spent across every theme.
func (r *Retro) VotesCast(handle string) int {
	total := 0
	for _, voters := range r.Votes {
		for _, voter := range voters {
			if voter == handle {
				total++
			}
		}
	}
	return total
}

// NotesOf returns the notes of one category, in file order.
func (r *Retro) NotesOf(category RetroCategory) []RetroNote {
	out := make([]RetroNote, 0, len(r.Notes))
	for _, n := range r.Notes {
		if n.Category == category {
			out = append(out, n)
		}
	}
	return out
}

// ParseRetro decodes a retro file. Like ParseSprint it reports one *ParseError
// carrying the diagnostic code, and fills the derived Path, Rev, Notes and body.
func ParseRetro(filePath string, data []byte) (*Retro, error) {
	block, body, err := SplitFrontMatter(data)
	if err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMMissing, "front matter is missing or unterminated", err)
	}
	var r Retro
	if err := yaml.Unmarshal(block, &r); err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMYAML, "front matter is not valid YAML", err)
	}
	fm := map[string]any{}
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return nil, newParseError(filePath, 0, "", CodeFMYAML, "front matter is not a mapping", err)
	}
	for key, value := range fm {
		if retroKnownKeys[key] {
			continue
		}
		if r.Extra == nil {
			r.Extra = map[string]any{}
		}
		r.Extra[key] = value
	}
	if r.Type == "" {
		r.Type = "retro"
	}
	if r.State == "" {
		r.State = RetroCollecting
	}
	r.Body = body
	r.body = parseRetroBody(body)
	r.Notes = r.body.notes()
	r.Path = filePath
	r.Rev = ComputeRev(data)
	return &r, nil
}

// SerializeRetro renders a retro back to file bytes in the key order of
// docs/04 section 9.2 and re-renders the body sections the structured state
// owns: the three note lists and the action checklist. Every other section —
// the carried-over actions, the discussion, anything a human added — is written
// back exactly as it was read, so the file stays a document rather than a
// generated report (AC "reads well in a plain editor").
func SerializeRetro(r *Retro) ([]byte, error) {
	if r == nil {
		return nil, errors.New("serialize retro: nil retro")
	}
	w := &fmWriter{}
	w.scalar("id", r.ID)
	w.scalar("type", r.Type)
	w.scalar("title", r.Title)
	w.scalar("sprint", r.Sprint)
	w.scalar("board", r.Board)
	w.date("date", r.Date)
	w.scalar("facilitator", r.Facilitator)
	w.stringList("participants", r.Participants)
	w.scalar("state", string(r.State))
	if r.Anonymous {
		w.raw("anonymous", "true")
	}
	if r.VotesPerPerson != nil {
		w.raw("votes_per_person", strconv.Itoa(*r.VotesPerPerson))
	}
	w.scalar("carried_from", r.CarriedFrom)
	writeRetroThemes(w, r.Themes)
	writeRetroVotes(w, r.Votes)
	writeRetroActions(w, r.Actions)
	w.timestamp("created", r.Created)
	w.timestamp("updated", r.Updated)
	w.scalar("author", r.Author)
	if err := w.extra(r.Extra); err != nil {
		return nil, fmt.Errorf("serialize retro %s: %w", r.Path, err)
	}
	return assemble(w.String(), renderRetroBody(r)), nil
}

// writeRetroThemes emits the groups, one field per line so that renaming a
// theme is a one-line diff.
func writeRetroThemes(w *fmWriter, themes []RetroTheme) {
	if len(themes) == 0 {
		return
	}
	w.b.WriteString("themes:\n")
	for _, t := range themes {
		w.b.WriteString("  - id: " + yamlFlowString(t.ID) + "\n")
		w.b.WriteString("    title: " + yamlString(t.Title) + "\n")
		if t.Category != "" {
			w.b.WriteString("    category: " + yamlFlowString(string(t.Category)) + "\n")
		}
		if len(t.Notes) > 0 {
			w.b.WriteString("    notes: [" + joinFlow(t.Notes) + "]\n")
		}
	}
}

// writeRetroVotes emits the ballot as `theme: [handles]`, sorted by theme id so
// that the file is stable whatever order the votes arrived in.
func writeRetroVotes(w *fmWriter, votes map[string][]string) {
	themes := make([]string, 0, len(votes))
	for theme, voters := range votes {
		if len(voters) > 0 {
			themes = append(themes, theme)
		}
	}
	if len(themes) == 0 {
		return
	}
	sort.Strings(themes)
	w.b.WriteString("votes:\n")
	for _, theme := range themes {
		w.b.WriteString("  " + yamlString(theme) + ": [" + joinFlow(votes[theme]) + "]\n")
	}
}

// writeRetroActions emits the improvement actions, one field per line: an owner
// changing or a task reference arriving is then a one-line diff, which is what
// lets two facilitators write at once without losing an entry.
func writeRetroActions(w *fmWriter, actions []RetroAction) {
	if len(actions) == 0 {
		return
	}
	w.b.WriteString("actions:\n")
	for _, a := range actions {
		w.b.WriteString("  - id: " + yamlFlowString(a.ID) + "\n")
		w.b.WriteString("    title: " + yamlString(a.Title) + "\n")
		if a.Owner != "" {
			w.b.WriteString("    owner: " + yamlFlowString(a.Owner) + "\n")
		}
		if !a.Due.IsZero() {
			w.b.WriteString("    due: " + a.Due.String() + "\n")
		}
		if a.Theme != "" {
			w.b.WriteString("    theme: " + yamlFlowString(a.Theme) + "\n")
		}
		if a.Task != "" {
			w.b.WriteString("    task: " + yamlFlowString(a.Task) + "\n")
		}
		if a.Status != "" {
			w.b.WriteString("    status: " + yamlFlowString(string(a.Status)) + "\n")
		}
		if a.Note != "" {
			w.b.WriteString("    note: " + yamlString(a.Note) + "\n")
		}
	}
}

// joinFlow renders a list of scalars for a `[a, b]` collection.
func joinFlow(items []string) string {
	rendered := make([]string, 0, len(items))
	for _, s := range items {
		rendered = append(rendered, yamlFlowString(s))
	}
	return strings.Join(rendered, ", ")
}

// RetroValidateInput is the context the file-level validation needs. Every
// field is optional: a rule whose input is missing is skipped rather than
// guessed, exactly as a sprint's validation does.
type RetroValidateInput struct {
	// TeamKey is the key of team.yaml; a retro id must carry it.
	TeamKey TeamKey
	// Sprints is every sprint id of the team repository.
	Sprints []string
	// Declared is every project key of team.yaml.
	Declared []ProjectKey
	// Known reports the references that resolve in a cloned project. A nil map
	// skips the dead-task rule.
	Known map[string]bool
	// Cloned is the set of projects an open repository serves; a task inside a
	// project nobody cloned is never dead, it is remote.
	Cloned map[ProjectKey]bool
}

// Validate applies the rules of docs/04 section 9.5 to one retro file.
func (r *Retro) Validate(in RetroValidateInput) []Diagnostic {
	var out []Diagnostic
	add := func(code Code, sev Severity, field, msg string) {
		out = append(out, Diagnostic{Code: code, Severity: sev, Path: r.Path, Field: field, Message: msg})
	}

	stem := boardStem(r.Path)
	key, _, err := ParseRetroID(r.ID)
	switch {
	case r.ID == "":
		add(CodeRetroID, SeverityError, "id", "missing")
	case err != nil:
		add(CodeRetroID, SeverityError, "id", err.Error())
	case stem != "" && stem != r.ID && !strings.HasPrefix(stem, r.ID+"-"):
		add(CodeRetroID, SeverityError, "id",
			fmt.Sprintf("id %q is not the prefix of the file name %q", r.ID, stem))
	case in.TeamKey != "" && key != in.TeamKey:
		add(CodeRetroID, SeverityError, "id",
			fmt.Sprintf("id %q carries the team key %s, but this repository is %s", r.ID, key, in.TeamKey))
	}

	if r.Date.IsZero() {
		add(CodeRetroDate, SeverityError, "date", "missing")
	}
	if !r.State.Valid() {
		add(CodeRetroState, SeverityError, "state",
			fmt.Sprintf("%q is not collecting, voting, discussing or closed", r.State))
	}

	themes := map[string]bool{}
	for _, t := range r.Themes {
		themes[t.ID] = true
	}
	for theme, voters := range r.Votes {
		if !themes[theme] {
			add(CodeRetroVoteTheme, SeverityError, "votes",
				fmt.Sprintf("votes reference the unknown theme %q", theme))
		}
		if len(r.Participants) == 0 {
			continue
		}
		for _, voter := range voters {
			if !containsString(r.Participants, voter) {
				add(CodeRetroVoteNonParticipant, SeverityWarning, "votes",
					fmt.Sprintf("%s voted on %s but is not a participant", voter, theme))
			}
		}
	}
	budget := r.VoteBudget()
	for _, handle := range sortedVoters(r.Votes) {
		if cast := r.VotesCast(handle); cast > budget {
			add(CodeRetroVoteBudget, SeverityWarning, "votes",
				fmt.Sprintf("%s cast %d votes; the budget is %d", handle, cast, budget))
		}
	}

	seen := map[string]bool{}
	for _, a := range r.Actions {
		if seen[a.ID] {
			add(CodeRetroActionIDDup, SeverityError, "actions",
				fmt.Sprintf("duplicate action id %q", a.ID))
		}
		seen[a.ID] = true
		if strings.TrimSpace(a.Owner) == "" {
			add(CodeRetroActionNoOwner, SeverityWarning, "actions",
				fmt.Sprintf("action %s has no owner; an action nobody owns is an action nobody does", a.ID))
		}
		if a.Task == "" {
			continue
		}
		ref, err := ParseRef(a.Task)
		if err != nil {
			add(CodeRetroActionTaskDead, SeverityWarning, "actions", err.Error())
			continue
		}
		if len(in.Declared) > 0 && !containsKey(in.Declared, ref.Project) {
			add(CodeRetroActionTaskDead, SeverityWarning, "actions",
				fmt.Sprintf("action %s names the undeclared project %s", a.ID, ref.Project))
			continue
		}
		if in.Known != nil && in.Cloned[ref.Project] && !in.Known[ref.String()] {
			add(CodeRetroActionTaskDead, SeverityWarning, "actions",
				fmt.Sprintf("the task %s of action %s does not resolve in the clone of %s",
					a.Task, a.ID, ref.Project))
		}
	}

	if r.Sprint != "" && len(in.Sprints) > 0 && !containsString(in.Sprints, r.Sprint) {
		add(CodeRetroSprintDead, SeverityWarning, "sprint",
			fmt.Sprintf("sprint %q does not exist in this team repository", r.Sprint))
	}

	sortDiagnostics(out)
	return out
}

// sortedVoters returns every handle that appears in the ballot, once, sorted.
func sortedVoters(votes map[string][]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, voters := range votes {
		for _, voter := range voters {
			if seen[voter] {
				continue
			}
			seen[voter] = true
			out = append(out, voter)
		}
	}
	sort.Strings(out)
	return out
}

// RetroStore reads and writes the retros of a team repository. It is the only
// place that touches `.pmngr/retros/`, and it goes through core.FS so that the
// browser and the companion share one implementation.
//
// Unlike a sprint, a retro file carries a slug after its id
// (`ACME-TEAM-R-0007-sprint-7.md`), so a lookup by id scans the folder for the
// file whose stem starts with the id.
type RetroStore struct {
	fs  FS
	dir string
	// Clock supplies the `updated` stamp of a write. Nil means "leave it".
	Clock Clock
}

// NewRetroStore returns a store over the `retros/` folder of a team `.pmngr/`.
// teamDir is TeamRef.TeamDirPath.
func NewRetroStore(fsys FS, teamDir string) *RetroStore {
	return &RetroStore{fs: fsys, dir: joinPath(teamDir, RetrosDirName)}
}

// Dir returns the vault-relative folder the store reads.
func (s *RetroStore) Dir() string { return s.dir }

// PathOf returns the file a new retro is stored in: the id, then the slug of
// its title when there is one (docs/04 section 9).
func (s *RetroStore) PathOf(id, title string) string {
	stem := id
	if strings.TrimSpace(title) != "" {
		stem = id + "-" + Slugify(title)
	}
	return joinPath(s.dir, stem+".md")
}

// List returns every retro of the team repository, newest date first and then
// by id, which is the order a retro index reads in. A file that does not parse
// is skipped and reported through the returned diagnostics rather than failing
// the whole listing.
func (s *RetroStore) List(ctx context.Context) ([]*Retro, []Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, wrapContext("retro list", err)
	}
	entries, err := s.fs.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", s.dir, err)
	}
	var retros []*Retro
	var diags []Diagnostic
	for _, e := range entries {
		if e.IsDir || !isMarkdown(e.Name) {
			continue
		}
		full := joinPath(s.dir, e.Name)
		data, err := s.fs.ReadFile(full)
		if err != nil {
			diags = append(diags, Diagnostic{
				Code: CodeRetroID, Severity: SeverityError, Path: full,
				Message: fmt.Sprintf("cannot read the retro: %v", err),
			})
			continue
		}
		retro, err := ParseRetro(full, data)
		if err != nil {
			diags = append(diags, diagnosticOf(err, full))
			continue
		}
		retros = append(retros, retro)
	}
	SortRetros(retros)
	return retros, diags, nil
}

// SortRetros orders retros newest first: by date descending, then by id
// descending so that two retros held on one day still have a stable order.
func SortRetros(retros []*Retro) {
	sort.SliceStable(retros, func(i, j int) bool {
		a, b := retros[i], retros[j]
		if !a.Date.Equal(b.Date.Time) {
			return b.Date.Before(a.Date.Time)
		}
		return a.ID > b.ID
	})
}

// Get reads one retro by its id, scanning the folder for the file whose stem is
// the id or begins with `<id>-`.
func (s *RetroStore) Get(ctx context.Context, id string) (*Retro, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("retro get", err)
	}
	full, err := s.pathFor(id)
	if err != nil {
		return nil, err
	}
	data, err := s.fs.ReadFile(full)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, fmt.Errorf("retro %s: %w", id, ErrItemNotFound)
		}
		return nil, fmt.Errorf("read %s: %w", full, err)
	}
	return ParseRetro(full, data)
}

// pathFor finds the file of a retro id.
func (s *RetroStore) pathFor(id string) (string, error) {
	entries, err := s.fs.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return "", fmt.Errorf("retro %s: %w", id, ErrItemNotFound)
		}
		return "", fmt.Errorf("read %s: %w", s.dir, err)
	}
	for _, e := range entries {
		if e.IsDir || !isMarkdown(e.Name) {
			continue
		}
		stem := strings.TrimSuffix(e.Name, ".md")
		if stem == id || strings.HasPrefix(stem, id+"-") {
			return joinPath(s.dir, e.Name), nil
		}
	}
	return "", fmt.Errorf("retro %s: %w", id, ErrItemNotFound)
}

// NextID allocates the next retro id of a team: the maximum number in
// `retros/` plus one, exactly as a project allocates an item id (docs/04 9.1).
func (s *RetroStore) NextID(ctx context.Context, team TeamKey) (string, error) {
	retros, _, err := s.List(ctx)
	if err != nil {
		return "", err
	}
	highest := 0
	for _, retro := range retros {
		if _, number, err := ParseRetroID(retro.ID); err == nil && number > highest {
			highest = number
		}
	}
	return FormatRetroID(team, highest+1), nil
}

// Write persists a retro, enforcing the optimistic lock when expected is not
// empty. It returns the retro with its new rev.
func (s *RetroStore) Write(ctx context.Context, retro *Retro, expected Rev) (*Retro, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("retro write", err)
	}
	if retro == nil {
		return nil, errors.New("retro write: nil retro")
	}
	full := retro.Path
	if full == "" {
		full = s.PathOf(retro.ID, retro.Title)
		retro.Path = full
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
		retro.Updated = NewTimestamp(s.Clock.Now())
	}
	data, err := SerializeRetro(retro)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(s.fs, full, data); err != nil {
		return nil, fmt.Errorf("write %s: %w", full, err)
	}
	retro.Rev = ComputeRev(data)
	retro.Body = renderRetroBody(retro)
	retro.body = parseRetroBody(retro.Body)
	retro.Notes = retro.body.notes()
	return retro, nil
}
