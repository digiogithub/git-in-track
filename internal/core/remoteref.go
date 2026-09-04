package core

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
)

// This file reads the committed index snapshots of a team repository —
// `.pmngr/index/<projectKey>.json` — and turns them into what a board needs to
// render a card of a project nobody cloned (docs/04 sections 6 and 7).
//
// It is pure: the file system is the FS the caller supplies, the clock is the
// instant the caller passes in, and nothing here writes anything. Generating a
// snapshot is (*Index).ProjectSnapshot, in snapshot.go.

// Card sources, as reported on a rendered board (docs/07 section 5.5).
const (
	// CardSourceLive marks a card read from a local clone: authoritative.
	CardSourceLive = "live"
	// CardSourceSnapshot marks a card read from a committed snapshot:
	// advisory, possibly stale, never editable.
	CardSourceSnapshot = "snapshot"
)

// SnapshotFreshness grades the age of a committed snapshot (R-SNAP-9).
type SnapshotFreshness string

// The freshness grades. They are what the UI renders: nothing at all for a
// fresh snapshot, a discreet "updated 3 days ago" while it is ageing, an amber
// badge once it is stale.
const (
	FreshnessUnknown SnapshotFreshness = "unknown"
	FreshnessFresh   SnapshotFreshness = "fresh"
	FreshnessAgeing  SnapshotFreshness = "ageing"
	FreshnessStale   SnapshotFreshness = "stale"
)

// freshWindow is how long a snapshot carries no age marker at all (R-SNAP-9).
const freshWindow = 24 * time.Hour

// defaultMaxAgeDays is the staleness threshold when team.yaml declares none.
const defaultMaxAgeDays = 7

// MaxAge returns the age after which a snapshot is stale, defaulting to seven
// days (docs/04 section 3.1).
func (p SnapshotPolicy) MaxAge() time.Duration {
	days := p.MaxAgeDays
	if days <= 0 {
		days = defaultMaxAgeDays
	}
	return time.Duration(days) * 24 * time.Hour
}

// Freshness grades an age against the policy.
func (p SnapshotPolicy) Freshness(age time.Duration) SnapshotFreshness {
	switch {
	case age < freshWindow:
		return FreshnessFresh
	case age < p.MaxAge():
		return FreshnessAgeing
	default:
		return FreshnessStale
	}
}

// SnapshotInfo is what the UI shows about one committed snapshot, whether it
// could be read or not. It is the answer to "why does this card look like
// this?", and it is carried by the team surface and by every remote card.
type SnapshotInfo struct {
	Project ProjectKey `json:"project"`
	// Path is where the file lives (or would live) in the team repository.
	Path string `json:"path"`
	// Present reports a file that was read and decoded.
	Present bool `json:"present"`
	// Enabled mirrors the team policy: with snapshots off, a remote card shows
	// its id and nothing else (R-SNAP-10).
	Enabled     bool      `json:"enabled"`
	Generated   Timestamp `json:"generated,omitempty"`
	GeneratedBy string    `json:"generatedBy,omitempty"`
	Generator   string    `json:"generator,omitempty"`
	// Commit and Dirty record the git state the snapshot was built from
	// (R-SNAP-4).
	Commit string `json:"commit,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
	Items  int    `json:"items"`
	// AgeSeconds is how old the snapshot is at the instant it was read.
	AgeSeconds int64             `json:"ageSeconds,omitempty"`
	Freshness  SnapshotFreshness `json:"freshness"`
	Stale      bool              `json:"stale"`
	// Error explains a file that exists but could not be used, in one sentence.
	Error string `json:"error,omitempty"`
}

// Age returns the recorded age as a duration.
func (i SnapshotInfo) Age() time.Duration { return time.Duration(i.AgeSeconds) * time.Second }

// DecodeProjectSnapshot parses `.pmngr/index/<projectKey>.json`.
//
// It is strict about the two things a reader cannot recover from — a document
// that is not JSON, and a schema this build does not implement — and lenient
// about everything else, because a snapshot is a cache: an unknown extra field
// is tomorrow's version, not a reason to blank a board.
func DecodeProjectSnapshot(data []byte) (*ProjectSnapshot, error) {
	var s ProjectSnapshot
	if err := decodeJSON(data, &s); err != nil {
		return nil, fmt.Errorf("decode project snapshot: %w", err)
	}
	if s.Schema != SnapshotSchema {
		return nil, fmt.Errorf("decode project snapshot: schema %d is not the supported version %d",
			s.Schema, SnapshotSchema)
	}
	return &s, nil
}

// ReadProjectSnapshot reads one committed snapshot from the team repository.
// A missing file is reported as (nil, false, nil): it is the normal state of a
// team that has never run `gintrack snapshot`, not an error.
func ReadProjectSnapshot(fsys FS, teamDirPath string, key ProjectKey) (*ProjectSnapshot, bool, error) {
	if fsys == nil {
		return nil, false, errors.New("read project snapshot: nil file system")
	}
	p := ProjectSnapshotPath(teamDirPath, key)
	data, err := fsys.ReadFile(p)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", p, err)
	}
	snap, err := DecodeProjectSnapshot(data)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", p, err)
	}
	return snap, true, nil
}

// SnapshotSet is every committed snapshot of a team repository, read once and
// then queried per card. A board render reads the files once and answers
// hundreds of refs from memory.
type SnapshotSet struct {
	policy  SnapshotPolicy
	now     time.Time
	entries map[ProjectKey]*snapshotEntry
	order   []ProjectKey
	diags   []Diagnostic
}

// snapshotEntry is one project's snapshot, indexed by item id.
type snapshotEntry struct {
	info  SnapshotInfo
	snap  *ProjectSnapshot
	items map[ItemID]ProjectSnapshotItem
	cfg   *ProjectConfig
}

// ReadSnapshots reads the committed snapshot of every key, in order. It never
// fails as a whole: a file that cannot be read or decoded becomes a diagnostic
// and a SnapshotInfo carrying the reason, so that one broken snapshot degrades
// one project's cards instead of the board (AC of GIT-US-0019).
//
// now is the instant staleness is measured from; a zero value disables the
// staleness grading rather than pretending the epoch is today.
func ReadSnapshots(
	fsys FS, teamDirPath string, keys []ProjectKey, policy SnapshotPolicy, now time.Time,
) *SnapshotSet {
	set := &SnapshotSet{policy: policy, now: now, entries: map[ProjectKey]*snapshotEntry{}}
	for _, key := range keys {
		if _, seen := set.entries[key]; seen {
			continue
		}
		set.order = append(set.order, key)
		entry := &snapshotEntry{info: SnapshotInfo{
			Project:   key,
			Path:      ProjectSnapshotPath(teamDirPath, key),
			Enabled:   policy.Enabled,
			Freshness: FreshnessUnknown,
		}}
		set.entries[key] = entry
		if !policy.Enabled {
			// R-SNAP-10: the team turned snapshots off; nothing is read, and a
			// remote card shows its id alone.
			continue
		}
		snap, found, err := ReadProjectSnapshot(fsys, teamDirPath, key)
		switch {
		case err != nil:
			entry.info.Error = err.Error()
			set.diags = append(set.diags, Diagnostic{
				Code: CodeSnapMalformed, Severity: SeverityError,
				Path: entry.info.Path, Field: "schema", Message: err.Error(),
			})
			continue
		case !found:
			continue
		}
		entry.snap = snap
		entry.info.Present = true
		entry.info.Generated = snap.Generated
		entry.info.GeneratedBy = snap.GeneratedBy
		entry.info.Generator = snap.Generator
		entry.info.Items = len(snap.Items)
		if snap.Source != nil {
			entry.info.Commit = snap.Source.Commit
			entry.info.Dirty = snap.Source.Dirty
		}
		entry.items = make(map[ItemID]ProjectSnapshotItem, len(snap.Items))
		for _, it := range snap.Items {
			entry.items[it.ID] = it
		}
		entry.cfg = snapshotConfig(key, snap)
		set.gradeAge(entry)
		if snap.Project.Key != "" && snap.Project.Key != key {
			set.diags = append(set.diags, Diagnostic{
				Code: CodeSnapKeyMismatch, Severity: SeverityWarning,
				Path: entry.info.Path, Field: "project.key",
				Message: fmt.Sprintf("the snapshot declares project %q but the file is named after %q",
					snap.Project.Key, key),
			})
		}
		if entry.info.Dirty {
			set.diags = append(set.diags, Diagnostic{
				Code: CodeSnapDirty, Severity: SeverityWarning,
				Path: entry.info.Path, Field: "source",
				Message: fmt.Sprintf("the %s snapshot was generated from a dirty working tree", key),
			})
		}
		if entry.info.Stale {
			set.diags = append(set.diags, Diagnostic{
				Code: CodeSnapStale, Severity: SeverityWarning,
				Path: entry.info.Path, Field: "generated",
				Message: fmt.Sprintf("the %s snapshot is %s old; run `gintrack snapshot` in a clone of %s",
					key, humanAge(entry.info.Age()), key),
			})
		}
	}
	sortDiagnostics(set.diags)
	return set
}

// gradeAge fills the freshness fields of an entry.
func (s *SnapshotSet) gradeAge(entry *snapshotEntry) {
	if s.now.IsZero() || entry.info.Generated.IsZero() {
		entry.info.Freshness = FreshnessUnknown
		return
	}
	age := s.now.Sub(entry.info.Generated.Time)
	if age < 0 {
		age = 0
	}
	entry.info.AgeSeconds = int64(age / time.Second)
	entry.info.Freshness = s.policy.Freshness(age)
	entry.info.Stale = entry.info.Freshness == FreshnessStale
}

// Policy returns the snapshot policy the set was read with.
func (s *SnapshotSet) Policy() SnapshotPolicy {
	if s == nil {
		return SnapshotPolicy{}
	}
	return s.policy
}

// Info returns what is known about one project's snapshot, including for a
// project that has none.
func (s *SnapshotSet) Info(key ProjectKey) SnapshotInfo {
	if s == nil {
		return SnapshotInfo{Project: key, Freshness: FreshnessUnknown}
	}
	if entry, ok := s.entries[key]; ok {
		return entry.info
	}
	return SnapshotInfo{Project: key, Freshness: FreshnessUnknown, Enabled: s.policy.Enabled}
}

// Snapshot returns the decoded snapshot of a project.
func (s *SnapshotSet) Snapshot(key ProjectKey) (*ProjectSnapshot, bool) {
	if s == nil {
		return nil, false
	}
	entry, ok := s.entries[key]
	if !ok || entry.snap == nil {
		return nil, false
	}
	return entry.snap, true
}

// Item returns one item of a project's snapshot.
func (s *SnapshotSet) Item(key ProjectKey, id ItemID) (ProjectSnapshotItem, bool) {
	if s == nil {
		return ProjectSnapshotItem{}, false
	}
	entry, ok := s.entries[key]
	if !ok || entry.items == nil {
		return ProjectSnapshotItem{}, false
	}
	it, ok := entry.items[id]
	return it, ok
}

// Config returns the workflow a snapshot publishes, in the shape the board
// column mapping expects. It is nil when the project has no usable snapshot.
func (s *SnapshotSet) Config(key ProjectKey) *ProjectConfig {
	if s == nil {
		return nil
	}
	if entry, ok := s.entries[key]; ok {
		return entry.cfg
	}
	return nil
}

// Keys returns the projects the set was read for, in the order given.
func (s *SnapshotSet) Keys() []ProjectKey {
	if s == nil {
		return nil
	}
	return append([]ProjectKey(nil), s.order...)
}

// Diagnostics returns the findings of the read: a malformed file, a snapshot
// whose project key disagrees with its name, a stale or dirty one.
func (s *SnapshotSet) Diagnostics() []Diagnostic {
	if s == nil {
		return nil
	}
	return append([]Diagnostic(nil), s.diags...)
}

// snapshotConfig rebuilds a project workflow from what a snapshot publishes, so
// that the board columns of a project nobody cloned map exactly as they would
// with the project.yaml in hand.
//
// Statuses the workflow omits but items carry are added with the category the
// item recorded: a snapshot written by an older generator still maps.
func snapshotConfig(key ProjectKey, snap *ProjectSnapshot) *ProjectConfig {
	cfg := &ProjectConfig{Schema: SupportedSchema, Key: key, Name: snap.Project.Name}
	known := map[Status]bool{}
	for _, s := range snap.Workflow {
		if s.ID == "" || known[s.ID] {
			continue
		}
		known[s.ID] = true
		cfg.Workflow.Statuses = append(cfg.Workflow.Statuses, StatusDef{
			ID: s.ID, Name: s.Name, Category: s.Category, Terminal: s.Terminal,
		})
	}
	for _, it := range snap.Items {
		if it.Status == "" || known[it.Status] {
			continue
		}
		known[it.Status] = true
		cfg.Workflow.Statuses = append(cfg.Workflow.Statuses, StatusDef{
			ID: it.Status, Category: it.Category,
			Terminal: it.Category == CategoryDone || it.Category == CategoryCancelled,
		})
	}
	for _, l := range snap.Labels {
		cfg.Labels = append(cfg.Labels, Label{Name: l.Name, Color: l.Color})
	}
	return cfg
}

// Item projects a snapshot entry onto the item shape the filters and the column
// mapping work with. It is not a backlog item: it has no body, it is never
// written, and its revision is the one the snapshot recorded.
func (e ProjectSnapshotItem) Item() Item {
	return Item{
		ID: e.ID, Type: e.Type, Title: e.Title, Status: e.Status, Priority: e.Priority,
		Parent: e.Parent, Milestone: e.Milestone, Sprint: e.Sprint,
		Assignees: append([]string(nil), e.Assignees...),
		Labels:    append([]string(nil), e.Labels...),
		Estimate:  clonePtr(e.Estimate),
		Due:       e.Due, Updated: e.Updated, Path: e.Path, Rev: e.Rev,
	}
}

// humanAge renders a duration the way a card badge does.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "less than an hour"
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour")
	default:
		return plural(int(d/(24*time.Hour)), "day")
	}
}

// plural renders "1 day" and "3 days".
func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// ---------------------------------------------------------------- links ---

// Git hosting flavors, as declared by `host:` in team.yaml (docs/04 §7.3).
const (
	HostGitHub          = "github"
	HostGitLab          = "gitlab"
	HostGitea           = "gitea"
	HostBitbucket       = "bitbucket"
	HostBitbucketServer = "bitbucket-server"
	HostGeneric         = "generic"
)

// HostKind returns the hosting flavor of a project, inferring it from the URLs
// when team.yaml declares none (R-URL-2).
func (p TeamProject) HostKind() string {
	declared := strings.ToLower(strings.TrimSpace(p.Host))
	switch declared {
	case HostGitHub, HostGitLab, HostGitea, HostBitbucket, HostBitbucketServer, HostGeneric:
		return declared
	case "forgejo":
		return HostGitea
	}
	source := p.WebURL
	if source == "" {
		source = p.Repo
	}
	host := strings.ToLower(hostnameOf(source))
	switch {
	case strings.Contains(host, "github"):
		return HostGitHub
	case strings.Contains(host, "gitlab"):
		return HostGitLab
	case host == "bitbucket.org":
		return HostBitbucket
	default:
		return HostGeneric
	}
}

// BrowseURL returns the web_url of a project, derived from an https clone URL
// when team.yaml declares none. It is empty when no link can be built, which is
// what disables the "open on the host" action (R-URL-3).
func (p TeamProject) BrowseURL() string {
	if p.WebURL != "" {
		return strings.TrimRight(p.WebURL, "/")
	}
	if !strings.HasPrefix(p.Repo, "https://") && !strings.HasPrefix(p.Repo, "http://") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimRight(p.Repo, "/"), ".git")
}

// FileURL builds the browse URL of a repository-root-relative path on the
// project's git host, following the patterns of docs/04 section 7.3. It returns
// an empty string when no URL can be built, and never guesses a host.
func (p TeamProject) FileURL(filePath string) string {
	base := p.BrowseURL()
	if base == "" || filePath == "" {
		return ""
	}
	branch := p.Branch()
	escaped := escapePathSegments(path.Clean(filePath))
	switch p.HostKind() {
	case HostGitHub:
		return base + "/blob/" + url.PathEscape(branch) + "/" + escaped
	case HostGitLab:
		return base + "/-/blob/" + url.PathEscape(branch) + "/" + escaped
	case HostGitea:
		return base + "/src/branch/" + url.PathEscape(branch) + "/" + escaped
	case HostBitbucket:
		return base + "/src/" + url.PathEscape(branch) + "/" + escaped
	case HostBitbucketServer:
		return base + "/browse/" + escaped + "?at=" + url.QueryEscape("refs/heads/"+branch)
	default:
		// R-URL-3: a generic host has no known pattern; the UI shows the repo
		// URL and the path as text instead of linking to a guess.
		return ""
	}
}

// escapePathSegments URL-escapes every segment of a path, keeping the slashes
// (R-URL-1).
func escapePathSegments(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

// hostnameOf returns the host of a URL, tolerating the scp-like `git@host:path`
// form git remotes are often written in.
func hostnameOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Hostname()
	}
	if _, rest, ok := strings.Cut(raw, "@"); ok {
		host, _, _ := strings.Cut(rest, ":")
		return host
	}
	return ""
}

// ---------------------------------------------------- snapshot stability ---

// SameSnapshotContent reports whether two committed snapshots carry the same
// data, ignoring the fields that change on every run: when it was generated, by
// whom, with which build and from which commit.
//
// It is what keeps a snapshot out of the git history when nothing moved: a
// regenerated file that compares equal is not written back (ADR-014).
func SameSnapshotContent(a, b ProjectSnapshot) bool {
	strip := func(s ProjectSnapshot) ProjectSnapshot {
		s.Generated = Timestamp{}
		s.GeneratedBy = ""
		s.Generator = ""
		s.Source = nil
		return s
	}
	left, errLeft := EncodeProjectSnapshot(strip(a))
	right, errRight := EncodeProjectSnapshot(strip(b))
	if errLeft != nil || errRight != nil {
		return false
	}
	return bytes.Equal(left, right)
}

// SnapshotKeys returns the project keys a team declares, in declaration order.
// It is the list a board render and `gintrack snapshot` both iterate.
func SnapshotKeys(cfg *TeamConfig) []ProjectKey {
	if cfg == nil {
		return nil
	}
	out := make([]ProjectKey, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		out = append(out, p.Key)
	}
	return out
}
