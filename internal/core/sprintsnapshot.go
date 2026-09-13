package core

import (
	"sort"
	"strconv"
	"time"
)

// This file freezes the progress of a sprint at the moment it is closed.
//
// ADR-017 says no time series is ever stored, because a burndown is a function
// of the item files and a stored copy would be a second truth to keep correct.
// Closing a sprint is the one moment where that reasoning inverts: afterwards
// the items leave the scope, so the numbers stop being recomputable from the
// current state at all. ADR-034 records the amendment. Everything here is pure,
// takes `now` from the caller, and is written exactly once.

// SprintSnapshotVersion is the version a snapshot this binary writes carries. A
// file with a higher version parses without loss and is emitted back unchanged.
const SprintSnapshotVersion = 1

// MetricsSourceSnapshot marks numbers that were read from a sprint's frozen
// snapshot instead of reconstructed. It is not a fourth way of reading history:
// it is the record of a reading that already happened, and the snapshot carries
// the provenance of the history it froze alongside it.
const MetricsSourceSnapshot MetricsSource = "snapshot"

// SprintSnapshot is the progress of a sprint as it stood at its close: the
// totals, the three distributions, the burndown series and the provenance of
// the history all of it was computed from.
type SprintSnapshot struct {
	Version  int       `yaml:"version" json:"version"`
	ClosedAt Timestamp `yaml:"closed_at" json:"closedAt"`

	Totals     SnapshotTotals            `yaml:"totals" json:"totals"`
	ByStatus   map[Status]int            `yaml:"by_status,omitempty" json:"byStatus,omitempty"`
	ByAssignee map[string]SnapshotBucket `yaml:"by_assignee,omitempty" json:"byAssignee,omitempty"`
	ByLabel    map[string]SnapshotBucket `yaml:"by_label,omitempty" json:"byLabel,omitempty"`
	// Burndown is the observed part of the series, one point per sprint day at
	// most, so that the file cannot grow without a bound.
	Burndown []SnapshotPoint `yaml:"burndown,omitempty" json:"burndown,omitempty"`
	// Provenance is the honesty half: a snapshot taken where no git history
	// could be read is marked approximate rather than passed off as a
	// reconstruction (ADR-017).
	Provenance MetricsProvenance `yaml:"provenance" json:"provenance"`

	// Extra preserves the keys inside the block this version does not model, so
	// that an older binary never damages a newer file.
	Extra map[string]any `yaml:"-" json:"extra,omitempty"`
}

// SnapshotTotals is the scope as it stood at the close.
type SnapshotTotals struct {
	// Items is the whole scope; Resolved how much of it a clone or a snapshot
	// could grade, and Unresolved the rest. An unresolved reference is
	// reported, never counted as done and never counted as points.
	Items      int `yaml:"items" json:"items"`
	Resolved   int `yaml:"resolved" json:"resolved"`
	Done       int `yaml:"done" json:"done"`
	Unresolved int `yaml:"unresolved" json:"unresolved"`

	Points          float64 `yaml:"points" json:"points"`
	CommittedPoints float64 `yaml:"committed_points" json:"committedPoints"`
	DonePoints      float64 `yaml:"done_points" json:"donePoints"`
}

// SnapshotBucket is one row of the per-assignee and per-label distributions.
type SnapshotBucket struct {
	Total  int     `yaml:"total" json:"total"`
	Done   int     `yaml:"done" json:"done"`
	Points float64 `yaml:"points" json:"points"`
}

// SnapshotPoint is one frozen day of the burndown. Only days the history could
// speak for are frozen, because a future day carries no measurement.
type SnapshotPoint struct {
	Date Date `yaml:"date" json:"date"`
	// Remaining counts the references that were not finished that day;
	// RemainingPoints is the same in estimate.
	Remaining       int     `yaml:"remaining" json:"remaining"`
	RemainingPoints float64 `yaml:"remaining_points" json:"remainingPoints"`
	// Ideal is the straight line from the commitment to zero.
	Ideal     float64 `yaml:"ideal" json:"ideal"`
	Completed int     `yaml:"completed" json:"completed"`
	// Unknown counts the references whose state that day the history could not
	// state. A point with Unknown > 0 was an approximation when it was frozen.
	Unknown int `yaml:"unknown" json:"unknown"`
}

// snapshotKnownKeys is the set of keys inside the `snapshot` block this version
// models; everything else lands in Extra.
var snapshotKnownKeys = map[string]bool{
	"version": true, "closed_at": true, "totals": true, "by_status": true,
	"by_assignee": true, "by_label": true, "burndown": true, "provenance": true,
}

// adoptSnapshotExtra keeps the keys inside a parsed `snapshot` block that this
// version does not model, and defaults an absent version to 1.
func adoptSnapshotExtra(snap *SprintSnapshot, raw any) {
	if snap == nil {
		return
	}
	if snap.Version <= 0 {
		snap.Version = SprintSnapshotVersion
	}
	block, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for key, value := range block {
		if snapshotKnownKeys[key] {
			continue
		}
		if snap.Extra == nil {
			snap.Extra = map[string]any{}
		}
		snap.Extra[key] = value
	}
}

// writeSnapshotBlock emits the `snapshot` block in a fixed key order, with the
// keys this version does not model sorted after the ones it does — the same
// rule the top-level front matter follows.
func writeSnapshotBlock(w *fmWriter, snap *SprintSnapshot) error {
	if snap == nil {
		return nil
	}
	version := snap.Version
	if version <= 0 {
		version = SprintSnapshotVersion
	}
	w.b.WriteString("snapshot:\n")
	w.b.WriteString("  version: " + strconv.Itoa(version) + "\n")
	if !snap.ClosedAt.IsZero() {
		w.b.WriteString("  closed_at: " + snap.ClosedAt.String() + "\n")
	}
	w.b.WriteString("  totals:\n")
	for _, entry := range []struct {
		key   string
		value string
	}{
		{"items", strconv.Itoa(snap.Totals.Items)},
		{"resolved", strconv.Itoa(snap.Totals.Resolved)},
		{"done", strconv.Itoa(snap.Totals.Done)},
		{"unresolved", strconv.Itoa(snap.Totals.Unresolved)},
		{"points", trimFloat(snap.Totals.Points)},
		{"committed_points", trimFloat(snap.Totals.CommittedPoints)},
		{"done_points", trimFloat(snap.Totals.DonePoints)},
	} {
		w.b.WriteString("    " + entry.key + ": " + entry.value + "\n")
	}
	writeSnapshotCounts(w, snap.ByStatus)
	writeSnapshotBuckets(w, "by_assignee", snap.ByAssignee)
	writeSnapshotBuckets(w, "by_label", snap.ByLabel)
	if len(snap.Burndown) > 0 {
		w.b.WriteString("  burndown:\n")
		for _, p := range snap.Burndown {
			w.b.WriteString("    - { date: " + p.Date.String() +
				", remaining: " + strconv.Itoa(p.Remaining) +
				", remaining_points: " + trimFloat(p.RemainingPoints) +
				", ideal: " + trimFloat(p.Ideal) +
				", completed: " + strconv.Itoa(p.Completed) +
				", unknown: " + strconv.Itoa(p.Unknown) + " }\n")
		}
	}
	writeSnapshotProvenance(w, snap.Provenance)
	for _, key := range sortedKeys(snap.Extra) {
		if err := w.writeKeyValue(2, key, snap.Extra[key]); err != nil {
			return err
		}
	}
	return nil
}

// writeSnapshotCounts emits the per-status distribution, keys sorted.
func writeSnapshotCounts(w *fmWriter, counts map[Status]int) {
	if len(counts) == 0 {
		return
	}
	keys := make([]string, 0, len(counts))
	for status := range counts {
		keys = append(keys, string(status))
	}
	sort.Strings(keys)
	w.b.WriteString("  by_status:\n")
	for _, key := range keys {
		w.b.WriteString("    " + yamlString(key) + ": " + strconv.Itoa(counts[Status(key)]) + "\n")
	}
}

// writeSnapshotBuckets emits one of the two distributions, keys sorted and each
// row a flow mapping so that a distribution reads as a table.
func writeSnapshotBuckets(w *fmWriter, key string, buckets map[string]SnapshotBucket) {
	if len(buckets) == 0 {
		return
	}
	names := make([]string, 0, len(buckets))
	for name := range buckets {
		names = append(names, name)
	}
	sort.Strings(names)
	w.b.WriteString("  " + key + ":\n")
	for _, name := range names {
		bucket := buckets[name]
		w.b.WriteString("    " + yamlString(name) + ": { total: " + strconv.Itoa(bucket.Total) +
			", done: " + strconv.Itoa(bucket.Done) +
			", points: " + trimFloat(bucket.Points) + " }\n")
	}
}

// writeSnapshotProvenance emits the provenance of the frozen history. A
// snapshot whose provenance names no source at all emits nothing rather than an
// empty claim.
func writeSnapshotProvenance(w *fmWriter, p MetricsProvenance) {
	if p.Source == "" {
		return
	}
	w.b.WriteString("  provenance:\n")
	w.b.WriteString("    source: " + yamlString(string(p.Source)) + "\n")
	w.b.WriteString("    approximate: " + strconv.FormatBool(p.Approximate) + "\n")
	if !p.From.IsZero() {
		w.b.WriteString("    from: " + p.From.String() + "\n")
	}
	if p.Commits != 0 {
		w.b.WriteString("    commits: " + strconv.Itoa(p.Commits) + "\n")
	}
	if p.Truncated {
		w.b.WriteString("    truncated: true\n")
	}
	w.b.WriteString("    items: " + strconv.Itoa(p.Items) + "\n")
	w.b.WriteString("    covered: " + strconv.Itoa(p.Covered) + "\n")
	if p.Note != "" {
		w.b.WriteString("    note: " + yamlString(p.Note) + "\n")
	}
}

// BuildSprintSnapshot freezes the progress of a sprint. It is pure: the caller
// supplies the view, the metrics it already computed and the instant of the
// close, and this decides what the record says.
//
// The signature the story sketched took `SprintMetrics` and `Burndown`
// separately; it takes the `SprintMetricsView` instead because that is what a
// caller already holds and because it is the only value that carries the
// MetricsProvenance the snapshot has to freeze with the numbers.
func BuildSprintSnapshot(s *Sprint, view SprintView, metrics SprintMetricsView, now time.Time) SprintSnapshot {
	snap := SprintSnapshot{
		Version:    SprintSnapshotVersion,
		ClosedAt:   NewTimestamp(now),
		ByStatus:   map[Status]int{},
		ByAssignee: map[string]SnapshotBucket{},
		ByLabel:    map[string]SnapshotBucket{},
		Burndown:   []SnapshotPoint{},
	}

	totals := view.Sprint.Metrics
	snap.Totals = SnapshotTotals{
		Items: totals.Items, Resolved: totals.Resolved, Done: totals.Done,
		Unresolved: totals.Unresolved, Points: totals.Points,
		CommittedPoints: totals.CommittedPoints, DonePoints: totals.DonePoints,
	}

	seen := map[string]bool{}
	for _, card := range view.Cards {
		if seen[card.Ref] {
			continue
		}
		seen[card.Ref] = true
		if card.Status == "" {
			// A reference neither a clone nor a snapshot could grade. It is
			// counted in Unresolved and appears in no distribution: a snapshot
			// never turns a thing it could not read into work.
			continue
		}
		snap.ByStatus[card.Status]++
		for _, who := range dedupeSnapshotKeys(card.Assignees) {
			snap.ByAssignee[who] = addSnapshotCard(snap.ByAssignee[who], card)
		}
		for _, label := range dedupeSnapshotKeys(card.Labels) {
			snap.ByLabel[label] = addSnapshotCard(snap.ByLabel[label], card)
		}
	}

	snap.Burndown = freezeBurndown(s, metrics.Burndown)
	snap.Provenance = metrics.Provenance
	if snap.Provenance.Source == "" {
		snap.Provenance.Source = MetricsSourceNone
	}
	// Only a full reconstruction from git is exact; everything else is an
	// approximation and the snapshot says so for as long as it exists.
	if snap.Provenance.Source != MetricsSourceGit || snap.Provenance.Truncated {
		snap.Provenance.Approximate = true
	}
	return snap
}

// addSnapshotCard folds one card into a distribution row.
func addSnapshotCard(bucket SnapshotBucket, card BoardCard) SnapshotBucket {
	bucket.Total++
	bucket.Points += card.Points()
	if card.Done() {
		bucket.Done++
	}
	return bucket
}

// dedupeSnapshotKeys drops the empty and repeated entries of an assignee or
// label list, so that a card listed twice counts once.
func dedupeSnapshotKeys(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// freezeBurndown turns the live series into the frozen one: the observed days
// only, one point per date, capped at the length of the sprint so that the file
// cannot grow without a bound.
func freezeBurndown(s *Sprint, burndown Burndown) []SnapshotPoint {
	out := make([]SnapshotPoint, 0, len(burndown.Points))
	seen := map[string]bool{}
	for _, p := range burndown.Points {
		if !p.Observed || p.Date.IsZero() {
			continue
		}
		key := p.Date.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		remaining := p.Items - p.Completed
		if remaining < 0 {
			remaining = 0
		}
		out = append(out, SnapshotPoint{
			Date: p.Date, Remaining: remaining, RemainingPoints: p.Remaining,
			Ideal: p.Ideal, Completed: p.Completed, Unknown: p.Unknown,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date.Time) })
	if days := s.TotalDays(); days > 0 && len(out) > days {
		out = out[:days]
	}
	return out
}

// SprintMetricsFromSnapshot rebuilds what a metrics reader shows for a closed
// sprint out of its frozen snapshot, without touching any history source. It
// reports false for a sprint that carries no snapshot, which is the signal to
// fall through to BuildSprintMetrics (GIT-US-0080).
//
// It repairs nothing: the numbers are read back exactly as they were frozen,
// because the snapshot is a record of a moment and not a cache.
func SprintMetricsFromSnapshot(s *Sprint, summary SprintSummary) (SprintMetricsView, bool) {
	if s == nil || s.Snapshot == nil {
		return SprintMetricsView{}, false
	}
	snap := s.Snapshot
	out := SprintMetricsView{
		Sprint: summary,
		Burndown: Burndown{
			Sprint: s.ID, Start: s.Start, End: s.End,
			CommittedPoints: snap.Totals.CommittedPoints,
			Points:          make([]BurndownPoint, 0, len(snap.Burndown)),
		},
		Flow:  CumulativeFlow{Bands: FlowBands(), Days: []FlowPoint{}},
		Items: []BoardCard{},
	}
	for i, p := range snap.Burndown {
		out.Burndown.Points = append(out.Burndown.Points, BurndownPoint{
			Date: p.Date, Day: i + 1, Ideal: p.Ideal, Observed: true,
			Remaining: p.RemainingPoints,
			Scope:     p.RemainingPoints + snapshotDonePoints(snap, p),
			Done:      snapshotDonePoints(snap, p),
			Items:     p.Remaining + p.Completed,
			Completed: p.Completed, Unknown: p.Unknown,
		})
	}
	out.Provenance = SnapshotProvenance(snap)
	return out, true
}

// snapshotDonePoints is the finished estimate of one frozen day, which the
// frozen point states only as the remainder of the ideal start.
func snapshotDonePoints(snap *SprintSnapshot, p SnapshotPoint) float64 {
	done := snap.Totals.CommittedPoints - p.RemainingPoints
	if done < 0 {
		return 0
	}
	return done
}

// SnapshotProvenance is what the UI says above a chart that came from a frozen
// snapshot: that the numbers were frozen at the close and are not a live
// reading, plus whatever the history they were frozen from could claim.
func SnapshotProvenance(snap *SprintSnapshot) MetricsProvenance {
	if snap == nil {
		return MetricsProvenance{}
	}
	out := snap.Provenance
	frozen := "these numbers were frozen when the sprint was closed"
	if !snap.ClosedAt.IsZero() {
		frozen += " on " + snap.ClosedAt.String()
	}
	frozen += ", not reconstructed now."
	note := out.Note
	out.Source = MetricsSourceSnapshot
	out.Note = "Read from the sprint's stored snapshot: " + frozen
	if note != "" {
		out.Note += " When it was taken: " + note
	}
	return out
}
