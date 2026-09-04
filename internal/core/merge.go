package core

// Item-aware three-way merge (docs/06-git-sync.md section 5, story GIT-US-0022).
//
// A conflicted `.md` file is never handed to the user as raw conflict markers.
// The front matter is merged field by field on parsed values — a textual merge
// of YAML is exactly how an assignee or a label gets silently dropped (risk R5)
// — and the body is merged as text, hunk by hunk, with the heading each hunk
// falls under. Everything the merge decides on its own is reported so the UI
// can show it and the user can flip it; nothing is resolved silently.
//
// The result is re-serialized through the emitter the app writes with, so a
// resolved file is byte-identical in shape to one saved from the editor.

import (
	"fmt"
	"sort"
	"strings"
)

// The kinds of decision the front-matter merge makes, from the table in
// docs/06 section 5.2.
const (
	// FieldImmutable is id, type, created or author: base wins and a
	// difference is an anomaly, normally an ID collision (section 5.5).
	FieldImmutable = "immutable"
	// FieldSet is a set-like list (labels, assignees, participants): additions
	// from both sides are kept, deletions from either side are honored.
	FieldSet = "set"
	// FieldOrdered is an ordered list (links, actions, items, order): merged
	// by the ordered-list rule of section 5.4, which never loses an entry.
	FieldOrdered = "ordered"
	// FieldOrderMap is a board `order:` mapping of column to refs; each column
	// is merged with the ordered-list rule.
	FieldOrderMap = "order-map"
	// FieldScalar is a plain value decided by "only one side changed" or, when
	// both did, by the newest `updated`.
	FieldScalar = "scalar"
	// FieldTimestamp is `updated` itself: the newest of the two.
	FieldTimestamp = "timestamp"
	// FieldUnknown is a key this version does not model. It is preserved and
	// decided like a scalar, and flagged.
	FieldUnknown = "unknown"
)

// The sides a decision can take. They are also the values a Resolution sends
// back, which is what makes keep-mine and keep-theirs available everywhere.
const (
	SideBase   = "base"
	SideOurs   = "ours"
	SideTheirs = "theirs"
	SideMerged = "merged"
	SideBoth   = "both"
	SideEdited = "edited"
)

// immutableFields is the set base wins for.
var immutableFields = map[string]bool{"id": true, "type": true, "created": true, "author": true}

// setFields is the set-like lists, merged as a union that honors deletions.
var setFields = map[string]bool{
	"labels": true, "assignees": true, "participants": true, "attachments": true,
}

// orderedFields is the lists whose order carries meaning.
var orderedFields = map[string]bool{
	"links": true, "actions": true, "items": true, "order": true, "columns": true,
}

// identityKeys are the keys an ordered-list entry is identified by, in the
// order they are tried. A board card is a `ref`, a link is its target.
var identityKeys = []string{"ref", "target", "id", "key", "name"}

// MergeInput is the three versions of one conflicted path. A side that does
// not exist — an add/add conflict has no base — is the empty string.
type MergeInput struct {
	Base   string `json:"base"`
	Ours   string `json:"ours"`
	Theirs string `json:"theirs"`
}

// FieldDecision is one front-matter field the merge had to decide, reported so
// the resolver can show it and the user can override it (docs/06 section 5.2:
// "the merge is automatic but never silent").
type FieldDecision struct {
	Field  string `json:"field"`
	Kind   string `json:"kind"`
	Base   any    `json:"base,omitempty"`
	Ours   any    `json:"ours,omitempty"`
	Theirs any    `json:"theirs,omitempty"`
	Merged any    `json:"merged,omitempty"`
	// Choice is the side the merged value came from: base, ours, theirs or
	// merged when both contributed.
	Choice string `json:"choice"`
	// Review is true when the decision was a judgement call — both sides
	// changed the field — and the user should look at it.
	Review bool `json:"review"`
	// Note explains the rule that was applied.
	Note string `json:"note,omitempty"`
}

// MergeHunk is one region of the body the two sides did not both leave alone.
type MergeHunk struct {
	Index int `json:"index"`
	// Section is the Markdown heading the hunk falls under, or empty.
	Section string `json:"section,omitempty"`
	Base    string `json:"base"`
	Ours    string `json:"ours"`
	Theirs  string `json:"theirs"`
	// Merged is the text that lands in the result with the current choice.
	Merged string `json:"merged"`
	// Choice is ours, theirs, both, base or edited.
	Choice string `json:"choice"`
	// Conflicted is true when both sides changed the same region and no rule
	// could pick for them: this is the hunk the user has to decide.
	Conflicted bool `json:"conflicted"`
	// Suggestion is what the resolver preselects for a conflicted hunk.
	Suggestion string `json:"suggestion,omitempty"`
	Note       string `json:"note,omitempty"`
}

// Resolution is what the user decided. Every field is optional, and the ones
// that are set win over the automatic merge. `Take` and `Content` are the
// escape hatches that are available for every conflict, whatever its shape.
type Resolution struct {
	// Take is "ours" or "theirs": keep one whole side, verbatim.
	Take string `json:"take,omitempty"`
	// Content replaces the whole file with text the user edited.
	Content string `json:"content,omitempty"`
	// Body replaces the merged body, keeping the merged front matter.
	Body string `json:"body,omitempty"`
	// Fields overrides a field decision: field name to base, ours or theirs.
	Fields map[string]string `json:"fields,omitempty"`
	// Hunks overrides a body hunk: the hunk index, as a decimal string, to
	// ours, theirs, both, base or edited.
	Hunks map[string]string `json:"hunks,omitempty"`
	// HunkText carries the text of an "edited" hunk, keyed the same way.
	HunkText map[string]string `json:"hunkText,omitempty"`
}

// Empty reports whether the resolution asks for nothing, which is what an
// analysis call sends.
func (r *Resolution) Empty() bool {
	return r == nil || (r.Take == "" && r.Content == "" && r.Body == "" &&
		len(r.Fields) == 0 && len(r.Hunks) == 0 && len(r.HunkText) == 0)
}

// MergeResult is the merged file plus everything the UI needs to explain it.
type MergeResult struct {
	Path string `json:"path"`
	// Structured is true when all three sides carry YAML front matter, which is
	// what makes the field-level merge possible. A plain Markdown page or an
	// unparsable side falls back to the text merge alone.
	Structured bool `json:"structured"`
	// Fields lists every front-matter field a decision was made for.
	Fields []FieldDecision `json:"fields,omitempty"`
	// Hunks lists every body region that was not identical on both sides.
	Hunks []MergeHunk `json:"hunks,omitempty"`
	// Content is the merged file, canonically serialized.
	Content string `json:"content"`
	// Conflicted counts the hunks that still need a decision.
	Conflicted int `json:"conflicted"`
	// Review counts the field decisions the user should look at.
	Review int `json:"review"`
	// Clean is true when nothing is left to decide, so the result can be
	// written as it stands.
	Clean bool `json:"clean"`
	// Warnings are the anomalies that did not stop the merge.
	Warnings []string `json:"warnings,omitempty"`
}

// MergeFiles merges the three versions of one conflicted path, applying the
// user's resolution when there is one.
//
// It never fails on content: a side that has no front matter, or front matter
// that does not parse, degrades to the text merge rather than refusing to help.
// The error is reserved for a result that could not be serialized at all.
func MergeFiles(path string, in MergeInput, res *Resolution) (MergeResult, error) {
	out := MergeResult{Path: path}
	if res != nil && res.Take != "" {
		return wholeSide(path, in, res.Take)
	}
	if res != nil && res.Content != "" {
		out.Content = normalizeContent(res.Content)
		out.Clean = true
		return out, nil
	}

	baseFM, baseBody, baseOK := parseSide(in.Base)
	ourFM, ourBody, ourOK := parseSide(in.Ours)
	theirFM, theirBody, theirOK := parseSide(in.Theirs)
	out.Structured = ourOK && theirOK && (baseOK || in.Base == "")

	if !out.Structured {
		baseBody, ourBody, theirBody = in.Base, in.Ours, in.Theirs
	}

	body, hunks := mergeBody(baseBody, ourBody, theirBody, res)
	out.Hunks = hunks
	for _, h := range hunks {
		if h.Conflicted {
			out.Conflicted++
		}
	}
	if res != nil && res.Body != "" {
		body = res.Body
		out.Conflicted = 0
	}

	if !out.Structured {
		out.Content = normalizeContent(body)
		out.Clean = out.Conflicted == 0
		return out, nil
	}

	merged, decisions := mergeFrontMatter(baseFM, ourFM, theirFM, res)
	out.Fields = decisions
	for _, d := range decisions {
		if d.Review {
			out.Review++
		}
		if d.Kind == FieldImmutable && d.Review {
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"%s differs on both sides but is immutable; the base value was kept "+
					"(this normally means two items were allocated the same id)", d.Field))
		}
	}
	content, err := emitDocument(path, merged, body)
	if err != nil {
		return out, err
	}
	out.Content = content
	out.Clean = out.Conflicted == 0
	return out, nil
}

// wholeSide is the keep-mine / keep-theirs escape hatch: one side, verbatim.
func wholeSide(path string, in MergeInput, take string) (MergeResult, error) {
	out := MergeResult{Path: path, Clean: true}
	switch take {
	case SideOurs:
		out.Content = normalizeContent(in.Ours)
	case SideTheirs:
		out.Content = normalizeContent(in.Theirs)
	case SideBase:
		out.Content = normalizeContent(in.Base)
	default:
		return out, fmt.Errorf("merge %s: unknown side %q: use ours, theirs or base", path, take)
	}
	return out, nil
}

// parseSide splits one side into front matter and body.
func parseSide(text string) (fm map[string]any, body string, ok bool) {
	if text == "" {
		return map[string]any{}, "", false
	}
	fm, body, err := ParseDocument([]byte(text))
	if err != nil {
		return map[string]any{}, text, false
	}
	if fm == nil {
		fm = map[string]any{}
	}
	return fm, body, true
}

// normalizeContent gives a file the line endings and trailing newline every
// writer in this codebase produces.
func normalizeContent(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return text
	}
	return strings.TrimRight(text, "\n") + "\n"
}

// ---------------------------------------------------------------- body merge

// mergeBody runs the three-way text merge and applies the per-hunk rules of
// docs/06 section 5.3: an agreed change is taken, a one-sided change is taken,
// a checkbox-only difference resolves to checked, and an append-only section
// suggests "take both".
func mergeBody(base, ours, theirs string, res *Resolution) (string, []MergeHunk) {
	baseLines, ourLines, theirLines := splitLines(base), splitLines(ours), splitLines(theirs)
	regions := Diff3(baseLines, ourLines, theirLines)

	merged := make([]string, 0, len(ourLines)+len(theirLines))
	hunks := make([]MergeHunk, 0, 4)
	index := 0
	for _, region := range regions {
		if region.Stable {
			merged = append(merged, region.Lines...)
			continue
		}
		hunk := decideHunk(index, sectionOf(merged, len(merged)), region)
		applyHunkResolution(&hunk, region, res)
		merged = append(merged, splitLines(hunk.Merged)...)
		hunks = append(hunks, hunk)
		index++
	}
	return joinLines(merged), hunks
}

// decideHunk applies the automatic rules to one unstable region.
func decideHunk(index int, section string, region TextRegion) MergeHunk {
	hunk := MergeHunk{
		Index:   index,
		Section: section,
		Base:    joinLines(region.Base),
		Ours:    joinLines(region.Ours),
		Theirs:  joinLines(region.Theirs),
	}
	switch {
	case hunk.Ours == hunk.Theirs:
		hunk.Merged, hunk.Choice = hunk.Ours, SideMerged
		hunk.Note = "both sides made the same change"
	case hunk.Ours == hunk.Base:
		hunk.Merged, hunk.Choice = hunk.Theirs, SideTheirs
		hunk.Note = "only the remote side changed this"
	case hunk.Theirs == hunk.Base:
		hunk.Merged, hunk.Choice = hunk.Ours, SideOurs
		hunk.Note = "only your side changed this"
	default:
		if lines, ok := checkboxMerge(region.Ours, region.Theirs); ok {
			hunk.Merged, hunk.Choice = joinLines(lines), SideMerged
			hunk.Note = "the two sides differ only in checked criteria; a completed one stays completed"
			return hunk
		}
		hunk.Conflicted = true
		hunk.Suggestion = SideOurs
		hunk.Note = "both sides changed this region"
		if appendOnlySection(section) {
			hunk.Suggestion = SideBoth
			hunk.Note = "both sides changed this region; " + section + " is append-only in practice"
		}
		hunk.Choice = hunk.Suggestion
		hunk.Merged = hunkText(hunk, hunk.Suggestion)
	}
	return hunk
}

// appendOnlySection reports the sections where "take both" is the sensible
// default, because in practice people append to them (docs/06 section 5.3).
func appendOnlySection(section string) bool {
	lower := strings.ToLower(strings.TrimLeft(section, "# "))
	return lower == "notes" || lower == "acceptance criteria"
}

// hunkText renders one hunk for a given choice.
func hunkText(h MergeHunk, choice string) string {
	switch choice {
	case SideOurs:
		return h.Ours
	case SideTheirs:
		return h.Theirs
	case SideBase:
		return h.Base
	case SideBoth:
		return joinBoth(h.Ours, h.Theirs)
	default:
		return h.Merged
	}
}

// joinBoth concatenates ours then theirs, skipping an empty half.
func joinBoth(ours, theirs string) string {
	switch {
	case ours == "":
		return theirs
	case theirs == "":
		return ours
	}
	return ours + "\n" + theirs
}

// applyHunkResolution overrides one hunk with what the user chose.
func applyHunkResolution(hunk *MergeHunk, _ TextRegion, res *Resolution) {
	if res == nil {
		return
	}
	key := fmt.Sprintf("%d", hunk.Index)
	choice, ok := res.Hunks[key]
	if !ok {
		if text, edited := res.HunkText[key]; edited {
			hunk.Merged, hunk.Choice, hunk.Conflicted = text, SideEdited, false
		}
		return
	}
	if choice == SideEdited {
		hunk.Merged, hunk.Choice, hunk.Conflicted = res.HunkText[key], SideEdited, false
		return
	}
	hunk.Merged = hunkText(*hunk, choice)
	hunk.Choice = choice
	hunk.Conflicted = false
}

// -------------------------------------------------------- front-matter merge

// mergeFrontMatter merges the parsed front matter field by field and reports
// every decision it made.
func mergeFrontMatter(base, ours, theirs map[string]any, res *Resolution) (map[string]any, []FieldDecision) {
	merged := make(map[string]any, len(ours)+len(theirs))
	decisions := make([]FieldDecision, 0, 4)
	newest := newerSide(ours, theirs)

	for _, key := range mergeKeys(base, ours, theirs) {
		bv, ov, tv := base[key], ours[key], theirs[key]
		decision := decideField(key, bv, ov, tv, newest)
		if res != nil {
			applyFieldResolution(&decision, bv, ov, tv, res)
		}
		if decision.Merged != nil {
			merged[key] = decision.Merged
		}
		if !sameValue(ov, tv) || decision.Review {
			decisions = append(decisions, decision)
		}
	}
	if _, ok := merged["updated"]; !ok {
		if v := newestTimestamp(ours, theirs); v != "" {
			merged["updated"] = v
		}
	}
	return merged, decisions
}

// mergeKeys is every key any side carries, in a stable order.
func mergeKeys(maps ...map[string]any) []string {
	seen := map[string]bool{}
	keys := make([]string, 0, 16)
	for _, m := range maps {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// decideField applies the rule table of docs/06 section 5.2 to one key.
func decideField(key string, bv, ov, tv any, newest string) FieldDecision {
	d := FieldDecision{Field: key, Base: bv, Ours: ov, Theirs: tv, Kind: kindOf(key)}
	switch {
	case immutableFields[key]:
		d.Merged, d.Choice = firstNonNil(bv, ov, tv), SideBase
		d.Note = "immutable: the value the two sides started from is kept"
		d.Review = !sameValue(ov, tv)
		return d
	case key == "updated":
		d.Merged, d.Choice = newerTimestamp(ov, tv), SideMerged
		d.Note = "the newer of the two timestamps"
		return d
	case d.Kind == FieldSet:
		d.Merged, d.Choice = mergeSet(bv, ov, tv), SideMerged
		d.Note = "additions from both sides kept, deletions from either side honored"
		return d
	case d.Kind == FieldOrderMap:
		d.Merged, d.Choice = mergeOrderMap(bv, ov, tv), SideMerged
		d.Note = "each column merged so that cards added on either side survive"
		return d
	case d.Kind == FieldOrdered:
		d.Merged, d.Choice = mergeOrdered(bv, ov, tv), SideMerged
		d.Note = "ordered merge: entries added on either side are kept, removals honored"
		return d
	}
	return decideScalar(d, bv, ov, tv, newest)
}

// decideScalar resolves a plain value: one side changed, both changed the same
// way, or both changed differently and the newest `updated` decides.
func decideScalar(d FieldDecision, bv, ov, tv any, newest string) FieldDecision {
	switch {
	case sameValue(ov, tv):
		d.Merged, d.Choice = ov, SideMerged
		return d
	case sameValue(bv, ov):
		d.Merged, d.Choice = tv, SideTheirs
		d.Note = "only the remote side changed it"
		return d
	case sameValue(bv, tv):
		d.Merged, d.Choice = ov, SideOurs
		d.Note = "only your side changed it"
		return d
	}
	d.Review = true
	if newest == SideOurs {
		d.Merged, d.Choice = ov, SideOurs
		d.Note = "both sides changed it; yours has the newer `updated` — check this"
		return d
	}
	d.Merged, d.Choice = tv, SideTheirs
	d.Note = "both sides changed it; the remote value was kept because its `updated` " +
		"is newer or the same — check this"
	return d
}

// kindOf classifies a key.
func kindOf(key string) string {
	switch {
	case immutableFields[key]:
		return FieldImmutable
	case key == "updated":
		return FieldTimestamp
	case setFields[key]:
		return FieldSet
	case key == "order":
		return FieldOrderMap
	case orderedFields[key]:
		return FieldOrdered
	case knownKeys[key]:
		return FieldScalar
	}
	return FieldUnknown
}

// applyFieldResolution overrides a decision with the side the user picked.
func applyFieldResolution(d *FieldDecision, bv, ov, tv any, res *Resolution) {
	choice, ok := res.Fields[d.Field]
	if !ok {
		return
	}
	switch choice {
	case SideOurs:
		d.Merged, d.Choice = ov, SideOurs
	case SideTheirs:
		d.Merged, d.Choice = tv, SideTheirs
	case SideBase:
		d.Merged, d.Choice = bv, SideBase
	default:
		return
	}
	d.Review = false
	d.Note = "chosen by you"
}

// mergeSet unions two set-like lists while honoring deletions: an entry the
// base had and one side removed is gone, an entry either side added is kept.
func mergeSet(bv, ov, tv any) any {
	base, ours, theirs := stringSlice(bv), stringSlice(ov), stringSlice(tv)
	if ours == nil && theirs == nil {
		return nil
	}
	inBase, inOurs, inTheirs := setOf(base), setOf(ours), setOf(theirs)
	keep := map[string]bool{}
	for _, v := range append(append([]string{}, ours...), theirs...) {
		if inBase[v] && (!inOurs[v] || !inTheirs[v]) {
			continue // removed on one side: the removal wins
		}
		keep[v] = true
	}
	out := make([]any, 0, len(keep))
	names := make([]string, 0, len(keep))
	for v := range keep {
		names = append(names, v)
	}
	sort.Strings(names)
	for _, v := range names {
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeOrdered merges two ordered lists (docs/06 section 5.4): start from the
// remote order, honor every removal, then splice back in what we added, next to
// the neighbor it had.
func mergeOrdered(bv, ov, tv any) any {
	base, ours, theirs := anySlice(bv), anySlice(ov), anySlice(tv)
	if ours == nil && theirs == nil {
		return nil
	}
	baseIDs, ourIDs, theirIDs := identitySet(base), identitySet(ours), identitySet(theirs)

	out := make([]any, 0, len(theirs)+len(ours))
	seen := map[string]bool{}
	for _, entry := range theirs {
		id := identityOf(entry)
		if baseIDs[id] && !ourIDs[id] {
			continue // we removed it
		}
		out = append(out, entry)
		seen[id] = true
	}
	for i, entry := range ours {
		id := identityOf(entry)
		if seen[id] || (baseIDs[id] && !theirIDs[id]) {
			continue // already there, or the remote removed it
		}
		out = insertNear(out, entry, previousID(ours, i))
		seen[id] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// insertNear puts an entry after the entry it followed on our side, and at the
// end when that neighbor is gone.
func insertNear(list []any, entry any, after string) []any {
	if after != "" {
		for i, existing := range list {
			if identityOf(existing) == after {
				out := make([]any, 0, len(list)+1)
				out = append(out, list[:i+1]...)
				out = append(out, entry)
				return append(out, list[i+1:]...)
			}
		}
	}
	return append(list, entry)
}

// previousID is the identity of the entry before index i.
func previousID(list []any, i int) string {
	if i == 0 {
		return ""
	}
	return identityOf(list[i-1])
}

// mergeOrderMap merges a board `order:` mapping column by column, so that
// cards added on either side survive on both (docs/06 section 5.4).
func mergeOrderMap(bv, ov, tv any) any {
	base, ours, theirs := anyMap(bv), anyMap(ov), anyMap(tv)
	if ours == nil && theirs == nil {
		return nil
	}
	out := map[string]any{}
	for _, column := range mergeKeys(base, ours, theirs) {
		if merged := mergeOrdered(base[column], ours[column], theirs[column]); merged != nil {
			out[column] = merged
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// identityOf is the key an ordered entry is matched by across sides.
func identityOf(entry any) string {
	switch t := entry.(type) {
	case map[string]any:
		for _, key := range identityKeys {
			if v, ok := t[key]; ok {
				return stringOf(v)
			}
		}
		return fmt.Sprintf("%v", t)
	default:
		return stringOf(entry)
	}
}

// identitySet indexes a list by identity.
func identitySet(list []any) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, entry := range list {
		out[identityOf(entry)] = true
	}
	return out
}

// ------------------------------------------------------------------ helpers

// newerSide reports which side carries the newer `updated`. A tie goes to
// theirs: the remote value already exists for everyone else (docs/06 §5.2).
func newerSide(ours, theirs map[string]any) string {
	if stringOf(ours["updated"]) > stringOf(theirs["updated"]) {
		return SideOurs
	}
	return SideTheirs
}

// newerTimestamp is max(ours, theirs) as the ISO 8601 strings they are.
func newerTimestamp(ov, tv any) any {
	if stringOf(ov) > stringOf(tv) {
		return ov
	}
	if tv == nil {
		return ov
	}
	return tv
}

// newestTimestamp is the newer `updated` of two sides, as a string.
func newestTimestamp(ours, theirs map[string]any) string {
	o, t := stringOf(ours["updated"]), stringOf(theirs["updated"])
	if o > t {
		return o
	}
	return t
}

// firstNonNil returns the first value that is set.
func firstNonNil(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// sameValue compares two decoded YAML values structurally.
func sameValue(a, b any) bool { return fmt.Sprintf("%#v", a) == fmt.Sprintf("%#v", b) }

// stringSlice reads a list of scalars as strings.
func stringSlice(v any) []string {
	list := anySlice(v)
	if list == nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, stringOf(e))
	}
	return out
}

// anySlice reads a value as a list, tolerating the shapes yaml.v3 produces.
func anySlice(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	case []string:
		out := make([]any, 0, len(t))
		for _, s := range t {
			out = append(out, s)
		}
		return out
	}
	return nil
}

// anyMap reads a value as a mapping.
func anyMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// setOf indexes a string list.
func setOf(list []string) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, v := range list {
		out[v] = true
	}
	return out
}

// emitDocument renders merged front matter and body as file bytes, then puts
// the result through the parser and the emitter the app writes with, so that a
// resolved file is byte-identical in shape to a file saved from the editor.
func emitDocument(path string, fm map[string]any, body string) (string, error) {
	w := &fmWriter{}
	written := map[string]bool{}
	for _, key := range canonicalKeyOrder {
		v, ok := fm[key]
		if !ok || v == nil {
			continue
		}
		written[key] = true
		if err := writeField(w, key, v); err != nil {
			return "", err
		}
	}
	for _, key := range sortedKeys(fm) {
		if written[key] || fm[key] == nil {
			continue
		}
		if err := writeField(w, key, fm[key]); err != nil {
			return "", err
		}
	}
	raw := assemble(w.String(), body)
	return string(canonicalize(path, raw)), nil
}

// writeField renders one front-matter entry, keeping the relation shape the
// emitter uses for `links:`.
func writeField(w *fmWriter, key string, v any) error {
	if key == "links" {
		if links, ok := decodeLinks(v); ok {
			w.links(key, links)
			return nil
		}
	}
	if err := w.writeKeyValue(0, key, v); err != nil {
		return fmt.Errorf("emit front matter field %q: %w", key, err)
	}
	return nil
}

// decodeLinks reads a merged `links:` list back into typed relations.
func decodeLinks(v any) ([]Link, bool) {
	list := anySlice(v)
	if list == nil {
		return nil, false
	}
	out := make([]Link, 0, len(list))
	for _, entry := range list {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, Link{
			Kind:   LinkKind(stringOf(m["kind"])),
			Target: stringOf(m["target"]),
			Note:   stringOf(m["note"]),
		})
	}
	return out, true
}

// canonicalize puts merged bytes through the parser and the emitter of the type
// the file declares. A file that does not parse is returned unchanged: a merge
// result the user still has to fix is better than no result at all.
func canonicalize(path string, raw []byte) []byte {
	fm, _, err := ParseDocument(raw)
	if err != nil {
		return raw
	}
	switch stringOf(fm["type"]) {
	case "board":
		if b, err := ParseBoard(path, raw); err == nil {
			if out, err := SerializeBoard(b); err == nil {
				return out
			}
		}
	case "sprint":
		if s, err := ParseSprint(path, raw); err == nil {
			if out, err := SerializeSprint(s); err == nil {
				return out
			}
		}
	case string(TypeComment):
		if c, err := ParseComment(path, raw); err == nil {
			if out, err := SerializeComment(c); err == nil {
				return out
			}
		}
	case string(TypeEpic), string(TypeStory), string(TypeTask), string(TypeMilestone):
		if it, err := ParseItem(path, raw); err == nil {
			if out, err := SerializeItem(it); err == nil {
				return out
			}
		}
	}
	return raw
}
