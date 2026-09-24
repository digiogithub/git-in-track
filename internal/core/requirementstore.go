package core

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// This file holds the single-requirement reads and writes of ADR-037 (docs/03
// section 21.5, GIT-US-0107): a requirement is a block in its spec's body plus
// its requirements: entry, and it is read, created and patched as a unit of its
// own under its own write token, the requirement rev. Like the rest of the spec
// layer it is pure text processing over an injected FS, so it compiles to
// WebAssembly unchanged (ADR-003).

// RequirementView is one requirement as every surface reads it: where it lives,
// what it says and its metadata, with both of its hashes.
//
// Rev is the requirement rev (R-REQ-REV-2): the write token every requirement
// write quotes. BlockRev is the block rev (R-REQ-REV-1): the fingerprint of
// what the requirement says, the value a verified.rev stamp records. They are
// different on purpose and a caller never swaps them.
type RequirementView struct {
	Ref     RequirementRef `json:"ref"`
	Spec    ItemID         `json:"spec"`
	Project ProjectKey     `json:"project,omitempty"`
	// Path is the file of the spec; Anchor the block's anchor in it (R-REQ-7).
	Path   string `json:"path"`
	Anchor string `json:"anchor"`
	// Line is the 1-based line of the heading inside the spec body.
	Line  int    `json:"line"`
	Title string `json:"title"`
	// Status is the requirement's status. When the entry has none it reads as
	// the workflow's initial status and StatusImplicit is set (W-REQ-NO-ENTRY).
	Status         Status `json:"status"`
	StatusImplicit bool   `json:"statusImplicit,omitempty"`
	// Text is the block below its heading — statement and scenarios — with the
	// blank lines around it removed. It is what a patch's text replaces.
	Text      string            `json:"text,omitempty"`
	Statement string            `json:"statement,omitempty"`
	Scenarios []Scenario        `json:"scenarios,omitempty"`
	Trace     *RequirementTrace `json:"trace,omitempty"`
	Verified  *Verification     `json:"verified,omitempty"`
	Links     []Link            `json:"links,omitempty"`
	// Extra carries the unknown keys of the entry, read-only (R-FMT-6).
	Extra map[string]any `json:"extra,omitempty"`
	// Rev is the requirement rev, the write token (R-REQ-REV-2).
	Rev Rev `json:"rev"`
	// BlockRev is the block rev, what verification stamps (R-REQ-REV-1).
	BlockRev Rev `json:"blockRev"`
}

// Row returns the view without the block text, statement and scenarios: the
// shape of a list row.
func (v RequirementView) Row() RequirementView {
	v.Text, v.Statement, v.Scenarios = "", "", nil
	return v
}

// RequirementPatch is a sparse change to one requirement. A nil field is left
// alone. Text replaces the block below the heading, Title the heading's title;
// the remaining fields change the requirement's requirements: entry only.
// Trace and Verified replace the whole value, keeping the unknown keys of the
// value they replace when they bring none (R-FMT-6). Unset clears "trace",
// "verified" or "links".
type RequirementPatch struct {
	Title    *string           `json:"title,omitempty"`
	Text     *string           `json:"text,omitempty"`
	Status   *Status           `json:"status,omitempty"`
	Trace    *RequirementTrace `json:"trace,omitempty"`
	Verified *Verification     `json:"verified,omitempty"`
	Links    *[]Link           `json:"links,omitempty"`
	Unset    []string          `json:"unset,omitempty"`
}

// RequirementDraft is a new requirement. The number is allocated by the store.
type RequirementDraft struct {
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
	// Status defaults to the workflow's initial status; a writer always
	// materializes it (docs/03 section 21.4).
	Status Status            `json:"status,omitempty"`
	Trace  *RequirementTrace `json:"trace,omitempty"`
	Links  []Link            `json:"links,omitempty"`
}

// RequirementHeading renders the canonical heading of a block, with the em
// dash every writer emits (R-REQ-1).
func RequirementHeading(ref RequirementRef, title string) string {
	return "### " + ref.String() + " " + ReqSeparator + " " + title
}

// SpecRequirements returns the requirements of a spec in body order. A number
// declared twice (E-REQ-DUPLICATE) is listed once, for its first block. cfg
// supplies the initial status an entry without one reads as; nil leaves it
// empty.
func SpecRequirements(spec *Item, cfg *ProjectConfig) ([]RequirementView, error) {
	if spec == nil || spec.Type != TypeSpec {
		return nil, nil
	}
	body := ParseSpecBody(spec.ID, spec.Body)
	seen := map[int]bool{}
	out := make([]RequirementView, 0, len(body.Blocks))
	for _, blk := range body.Blocks {
		if seen[blk.Ref.Number] {
			continue
		}
		seen[blk.Ref.Number] = true
		view, err := requirementViewOf(spec, blk, cfg)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// FindRequirement returns requirement n of a spec. A spec without that block —
// including one whose block was deleted by hand while its entry survived — has
// no such requirement: ErrItemNotFound.
func FindRequirement(spec *Item, n int, cfg *ProjectConfig) (RequirementView, error) {
	if spec == nil {
		return RequirementView{}, fmt.Errorf("requirement R%d: %w", n, ErrItemNotFound)
	}
	ref := RequirementRef{Spec: spec.ID, Number: n}
	if spec.Type != TypeSpec {
		return RequirementView{}, fmt.Errorf("%s: %s is a %s, not a spec: %w", ref, spec.ID, spec.Type, ErrItemNotFound)
	}
	blk, ok := ParseSpecBody(spec.ID, spec.Body).Block(n)
	if !ok {
		return RequirementView{}, fmt.Errorf("%s has no block in %s: %w", ref, spec.Path, ErrItemNotFound)
	}
	return requirementViewOf(spec, blk, cfg)
}

// requirementViewOf builds the view of one parsed block.
func requirementViewOf(spec *Item, blk RequirementBlock, cfg *ProjectConfig) (RequirementView, error) {
	entry := spec.Requirements[blk.Ref.Key()]
	rev, err := RequirementRev(blk.Text, entry)
	if err != nil {
		return RequirementView{}, fmt.Errorf("%s: %w", blk.Ref, err)
	}
	_, rest, _ := strings.Cut(normalizeNewlines(blk.Text), "\n")
	view := RequirementView{
		Ref:       blk.Ref,
		Spec:      spec.ID,
		Path:      spec.Path,
		Anchor:    blk.Ref.Anchor(),
		Line:      blk.Line,
		Title:     blk.Title,
		Text:      trimBlankLines(rest),
		Statement: blk.Statement,
		Scenarios: blk.Scenarios,
		Rev:       rev,
		BlockRev:  blk.Rev,
	}
	if entry != nil {
		e := entry.Clone()
		view.Status, view.Trace, view.Verified, view.Links, view.Extra = e.Status, e.Trace, e.Verified, e.Links, e.Extra
	}
	if view.Status == "" && cfg != nil {
		view.Status = cfg.InitialStatus()
		view.StatusImplicit = true
	}
	return view, nil
}

// normalizeNewlines turns CRLF into LF, which is what every offset of the block
// parser is measured on.
func normalizeNewlines(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// trimBlankLines drops the whitespace-only lines at both ends of a text and its
// final newline.
func trimBlankLines(s string) string {
	lines := strings.Split(normalizeNewlines(s), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// trailingBlank returns the whitespace-only lines a block extent ends with: the
// separation from whatever follows, which a rewrite of the block keeps.
func trailingBlank(ext string) string {
	lines := strings.SplitAfter(ext, "\n")
	i := len(lines)
	for i > 0 && strings.TrimSpace(lines[i-1]) == "" {
		i--
	}
	return strings.Join(lines[i:], "")
}

// requirementFieldError is the validation failure of a requirement write.
func requirementFieldError(spec *Item, ref RequirementRef, field, format string, args ...any) error {
	return &DiagnosticError{Diagnostic: Diagnostic{
		Code: CodeReqField, Severity: SeverityError, Path: spec.Path,
		Field: "requirements." + ref.Key() + "." + field, Message: fmt.Sprintf(format, args...),
	}}
}

// cleanRequirementTitle validates a title for the heading grammar (R-REQ-1).
func cleanRequirementTitle(spec *Item, ref RequirementRef, title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", requirementFieldError(spec, ref, "title", "a requirement needs a title")
	case strings.ContainsAny(title, "\r\n"):
		return "", requirementFieldError(spec, ref, "title", "a requirement title is one line")
	case utf8.RuneCountInString(title) > maxRequirementTitle:
		return "", requirementFieldError(spec, ref, "title", "a requirement title is at most %d characters", maxRequirementTitle)
	}
	return title, nil
}

// cleanRequirementText validates the text of a block below its heading: it may
// not hold a level 1–3 ATX heading outside a fence, which would end the block,
// nor leave a fence open, which would swallow the blocks after it (R-REQ-3).
func cleanRequirementText(spec *Item, ref RequirementRef, text string) (string, error) {
	text = trimBlankLines(text)
	var fence fenceTracker
	for i, line := range strings.Split(text, "\n") {
		if fence.step(line) {
			continue
		}
		if lvl := atxLevel(line); lvl >= 1 && lvl <= 3 {
			return "", requirementFieldError(spec, ref, "text",
				"line %d is a level-%d heading, which would end the requirement block; use #### or deeper", i+1, lvl)
		}
	}
	if fence.inside() {
		return "", requirementFieldError(spec, ref, "text", "the text leaves a fenced code block open")
	}
	return text, nil
}

// renderRequirementBlock renders a block from its heading and text.
func renderRequirementBlock(heading, text string) string {
	if text == "" {
		return heading + "\n"
	}
	return heading + "\n\n" + text + "\n"
}

// applyRequirementPatch applies a patch to requirement n of a spec in memory:
// the block in the body and the entry in requirements:. The status is always
// materialized (docs/03 section 21.4). Nothing but that block and that entry
// changes, so no other requirement's rev moves.
func applyRequirementPatch(it *Item, n int, patch RequirementPatch, cfg *ProjectConfig) error {
	body := normalizeNewlines(it.Body)
	parsed := ParseSpecBody(it.ID, body)
	blk, ok := parsed.Block(n)
	if !ok {
		return fmt.Errorf("%s.R%d has no block in %s: %w", it.ID, n, it.Path, ErrItemNotFound)
	}
	ref := blk.Ref
	heading, _, _ := strings.Cut(blk.Text, "\n")
	title := blk.Title
	if patch.Title != nil {
		t, err := cleanRequirementTitle(it, ref, *patch.Title)
		if err != nil {
			return err
		}
		if t != blk.Title {
			title = t
			heading = RequirementHeading(ref, title)
		}
	}
	newBody := body
	switch {
	case patch.Text != nil:
		text, err := cleanRequirementText(it, ref, *patch.Text)
		if err != nil {
			return err
		}
		_, rest, _ := strings.Cut(blk.Text, "\n")
		if text != trimBlankLines(rest) || title != blk.Title {
			block := renderRequirementBlock(heading, text) + trailingBlank(blk.Text)
			newBody = body[:blk.Start] + block + body[blk.End:]
		}
	case title != blk.Title:
		old, _, _ := strings.Cut(blk.Text, "\n")
		newBody = body[:blk.Start] + heading + body[blk.Start+len(old):]
	}
	if newBody != body {
		after := ParseSpecBody(it.ID, newBody)
		if len(after.Blocks) != len(parsed.Blocks) {
			return requirementFieldError(it, ref, "text", "the change would split or merge requirement blocks")
		}
		it.Body = newBody
	}

	key := ref.Key()
	entry := it.Requirements[key].Clone()
	if entry == nil {
		entry = &Requirement{}
	}
	if entry.Status == "" && cfg != nil {
		entry.Status = cfg.InitialStatus()
	}
	if patch.Status != nil {
		entry.Status = *patch.Status
	}
	if patch.Trace != nil {
		t := &RequirementTrace{
			Code:  append([]string(nil), patch.Trace.Code...),
			Tests: append([]string(nil), patch.Trace.Tests...),
			Extra: cloneMap(patch.Trace.Extra),
		}
		if t.Extra == nil && entry.Trace != nil {
			t.Extra = entry.Trace.Extra
		}
		entry.Trace = t
	}
	if patch.Verified != nil {
		v := *patch.Verified
		v.Extra = cloneMap(patch.Verified.Extra)
		if v.Extra == nil && entry.Verified != nil {
			v.Extra = entry.Verified.Extra
		}
		entry.Verified = &v
	}
	if patch.Links != nil {
		entry.Links = append([]Link(nil), (*patch.Links)...)
		if len(entry.Links) == 0 {
			entry.Links = nil
		}
	}
	for _, field := range patch.Unset {
		switch field {
		case "trace":
			entry.Trace = nil
		case "verified":
			entry.Verified = nil
		case "links":
			entry.Links = nil
		default:
			return requirementFieldError(it, ref, field, "%q cannot be unset: want trace, verified or links", field)
		}
	}
	reqs := it.Requirements.Clone()
	if reqs == nil {
		reqs = Requirements{}
	}
	reqs[key] = entry
	it.Requirements = reqs
	return nil
}

// requirementConflicts reports the fields a refused requirement write would
// still change, judged against the spec as it is on disk now (R-REV-3a). The
// text is named, never quoted back, like an item body.
//
// The list is empty exactly when every field the patch proposes already holds
// its value, which is what tells a caller its change is already there
// (GIT-US-0151). A patch that cannot be applied to the current content — one
// the store would refuse — has no field diff, so it names every field it
// carries rather than reading as already applied.
// Implements: GIT-SP-0001.R3, GIT-SP-0001.R4
func requirementConflicts(current *Item, n int, patch RequirementPatch, cfg *ProjectConfig) []ConflictField {
	if current == nil {
		return patchConflicts(RequirementView{}, patch)
	}
	cur, err := FindRequirement(current, n, cfg)
	if err != nil {
		return patchConflicts(RequirementView{}, patch)
	}
	proposed := current.clone()
	proposed.Requirements = current.Requirements.Clone()
	if err := applyRequirementPatch(proposed, n, patch, cfg); err != nil {
		return patchConflicts(cur, patch)
	}
	next, err := FindRequirement(proposed, n, cfg)
	if err != nil {
		return patchConflicts(cur, patch)
	}
	var out []ConflictField
	add := func(field, c, p string) {
		if c != p {
			out = append(out, ConflictField{Field: field, Current: c, Proposed: p})
		}
	}
	if cur.Text != next.Text {
		out = append(out, ConflictField{Field: "text"})
	}
	add("title", cur.Title, next.Title)
	add("status", string(cur.Status), string(next.Status))
	add("trace", renderTrace(cur.Trace), renderTrace(next.Trace))
	add("verified", renderVerification(cur.Verified), renderVerification(next.Verified))
	add("links", renderLinks(cur.Links), renderLinks(next.Links))
	return out
}

// patchConflicts names every field a patch carries, in the order
// requirementConflicts reports them, with the current value beside the
// proposed one. It is the report for a patch that could not be judged field by
// field. The text is named, never quoted.
func patchConflicts(cur RequirementView, patch RequirementPatch) []ConflictField {
	var out []ConflictField
	if patch.Text != nil {
		out = append(out, ConflictField{Field: "text"})
	}
	if patch.Title != nil {
		out = append(out, ConflictField{Field: "title", Current: cur.Title, Proposed: *patch.Title})
	}
	if patch.Status != nil {
		out = append(out, ConflictField{Field: "status", Current: string(cur.Status), Proposed: string(*patch.Status)})
	}
	unset := func(field string) bool { return slices.Contains(patch.Unset, field) }
	if patch.Trace != nil || unset("trace") {
		proposed := ""
		if patch.Trace != nil {
			proposed = renderTrace(patch.Trace)
		}
		out = append(out, ConflictField{Field: "trace", Current: renderTrace(cur.Trace), Proposed: proposed})
	}
	if patch.Verified != nil || unset("verified") {
		proposed := ""
		if patch.Verified != nil {
			proposed = renderVerification(patch.Verified)
		}
		out = append(out, ConflictField{Field: "verified", Current: renderVerification(cur.Verified), Proposed: proposed})
	}
	if patch.Links != nil || unset("links") {
		proposed := ""
		if patch.Links != nil {
			proposed = renderLinks(*patch.Links)
		}
		out = append(out, ConflictField{Field: "links", Current: renderLinks(cur.Links), Proposed: proposed})
	}
	for _, field := range patch.Unset {
		switch field {
		case "trace", "verified", "links":
		default:
			out = append(out, ConflictField{Field: field})
		}
	}
	return out
}

// renderTrace renders a trace for a conflict report.
func renderTrace(t *RequirementTrace) string {
	if t == nil || (len(t.Code) == 0 && len(t.Tests) == 0) {
		return ""
	}
	return "code: " + joinList(t.Code) + "; tests: " + joinList(t.Tests)
}

// renderVerification renders a stamp for a conflict report.
func renderVerification(v *Verification) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s %s %s", v.Rev, v.Commit, v.At.String(), v.By))
}

// insertRequirementBlock appends a new block to a spec body: after the last
// requirement block, else at the end of the "## Requirements" section, else in
// a "## Requirements" section added at the end of the body.
func insertRequirementBlock(body string, spec ItemID, block string) string {
	body = normalizeNewlines(body)
	pos := -1
	if blocks := ParseSpecBody(spec, body).Blocks; len(blocks) > 0 {
		pos = blocks[len(blocks)-1].End
	} else {
		var fence fenceTracker
		inRequirements := false
		for _, ln := range splitSpecLines(body) {
			if fence.step(ln.text) {
				continue
			}
			lvl := atxLevel(ln.text)
			if lvl != 1 && lvl != 2 {
				continue
			}
			if inRequirements {
				pos = ln.start
				break
			}
			if lvl == 2 && strings.EqualFold(headingText(ln.text, 2), "Requirements") {
				inRequirements = true
				pos = len(body)
			}
		}
	}
	if pos < 0 {
		return withBlankLineEnd(body) + "## Requirements\n\n" + block
	}
	before, after := body[:pos], body[pos:]
	out := withBlankLineEnd(before) + block
	if after != "" {
		out += "\n" + after
	}
	return out
}

// withBlankLineEnd makes a non-empty text end in a blank line.
func withBlankLineEnd(s string) string {
	if s == "" {
		return s
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	if !strings.HasSuffix(s, "\n\n") {
		s += "\n"
	}
	return s
}

// GetRequirement reads one requirement from its spec file as it is on disk
// now rather than as the index last saw it. A write that follows another one
// in the same transaction — the verification stamp written after a Spec Delta
// was applied (ADR-037 section 7) — quotes the rev this returns.
func (s *FileStore) GetRequirement(ctx context.Context, ref RequirementRef) (RequirementView, error) {
	if err := ctx.Err(); err != nil {
		return RequirementView{}, wrapContext("get requirement", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.readSpec(ref.Spec)
	if err != nil {
		return RequirementView{}, err
	}
	return FindRequirement(it, ref.Number, s.cfg)
}

// readSpec locates and parses a spec for a requirement write. It is the part of
// readChecked a requirement write shares: the lock is checked afterwards,
// against the requirement rev rather than the file rev.
func (s *FileStore) readSpec(spec ItemID) (*Item, error) {
	found, err := s.locate(spec)
	if err != nil {
		return nil, err
	}
	data, err := s.fs.ReadFile(found.Path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", found.Path, err)
	}
	it, err := ParseItem(found.Path, data)
	if err != nil {
		return nil, err
	}
	if it.Type != TypeSpec {
		return nil, fmt.Errorf("%s is a %s, not a spec: %w", spec, it.Type, ErrItemNotFound)
	}
	return it, nil
}

// UpdateRequirement patches one requirement under its requirement rev
// (R-REQ-REV-2): only its block and its requirements: entry change, so a
// concurrent write to another requirement of the same spec neither conflicts
// with this one nor is lost by it. A stale rev fails with a StaleRevisionError
// carrying the current requirement rev and the fields the patch would still
// change. An empty expected rev writes unconditionally, as Update does.
//
// The write goes through the canonical serializer, the validator and the
// implicit schema upgrade exactly like an item update.
// Implements: GIT-SP-0001.R2, GIT-SP-0001.R9, GIT-SP-0001.R10
func (s *FileStore) UpdateRequirement(
	ctx context.Context, ref RequirementRef, patch RequirementPatch, expected Rev,
) (*Item, RequirementView, error) {
	if err := ctx.Err(); err != nil {
		return nil, RequirementView{}, wrapContext("update requirement", err)
	}
	if err := s.cfg.WriteGate(); err != nil {
		return nil, RequirementView{}, fmt.Errorf("update %s: %w", ref, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it, err := s.readSpec(ref.Spec)
	if err != nil {
		return nil, RequirementView{}, err
	}
	current, err := FindRequirement(it, ref.Number, s.cfg)
	if err != nil {
		return nil, RequirementView{}, err
	}
	if expected != "" && expected != current.Rev {
		return nil, RequirementView{}, &StaleRevisionError{
			ID: ItemID(ref.String()), Path: it.Path, Expected: expected, Current: current.Rev,
			Fields: requirementConflicts(it, ref.Number, patch, s.cfg),
		}
	}
	if patch.Status != nil && *patch.Status != current.Status {
		// A requirement's status follows the project workflow exactly as an
		// item's does (R-REQ-10).
		probe := &Item{ID: ItemID(ref.String()), Status: current.Status, Path: it.Path}
		if err := s.checkTransition(probe, *patch.Status, false); err != nil {
			return nil, RequirementView{}, err
		}
	}
	had := HasSpecConstruct(it)
	if err := applyRequirementPatch(it, ref.Number, patch, s.cfg); err != nil {
		return nil, RequirementView{}, err
	}
	it.Updated = s.now()
	if err := s.validateAndUpgrade(it, had); err != nil {
		return nil, RequirementView{}, err
	}
	if err := s.writeItem(it, ""); err != nil {
		return nil, RequirementView{}, err
	}
	view, err := FindRequirement(it, ref.Number, s.cfg)
	if err != nil {
		return nil, RequirementView{}, err
	}
	return it, view, nil
}

// CreateRequirement appends a new requirement to a spec. Its number is max + 1
// over the spec's blocks, its requirements: keys and the inbound refs the
// caller found in the project index (R-REQ-5), so a removed number is never
// handed out again. A create conflicts with nothing: it needs no rev.
// Implements: GIT-SP-0002.R7
func (s *FileStore) CreateRequirement(
	ctx context.Context, spec ItemID, draft RequirementDraft, inbound []RequirementRef,
) (*Item, RequirementView, error) {
	if err := ctx.Err(); err != nil {
		return nil, RequirementView{}, wrapContext("create requirement", err)
	}
	if err := s.cfg.WriteGate(); err != nil {
		return nil, RequirementView{}, fmt.Errorf("create requirement in %s: %w", spec, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it, err := s.readSpec(spec)
	if err != nil {
		return nil, RequirementView{}, err
	}
	ref := RequirementRef{Spec: it.ID, Number: NextRequirementNumber(it, inbound)}
	title, err := cleanRequirementTitle(it, ref, draft.Title)
	if err != nil {
		return nil, RequirementView{}, err
	}
	text, err := cleanRequirementText(it, ref, draft.Text)
	if err != nil {
		return nil, RequirementView{}, err
	}
	had := HasSpecConstruct(it)
	it.Body = insertRequirementBlock(it.Body, it.ID, renderRequirementBlock(RequirementHeading(ref, title), text))

	entry := &Requirement{Status: draft.Status, Links: append([]Link(nil), draft.Links...)}
	if len(entry.Links) == 0 {
		entry.Links = nil
	}
	if entry.Status == "" && s.cfg != nil {
		entry.Status = s.cfg.InitialStatus()
	}
	if draft.Trace != nil {
		entry.Trace = &RequirementTrace{
			Code:  append([]string(nil), draft.Trace.Code...),
			Tests: append([]string(nil), draft.Trace.Tests...),
			Extra: cloneMap(draft.Trace.Extra),
		}
	}
	reqs := it.Requirements.Clone()
	if reqs == nil {
		reqs = Requirements{}
	}
	reqs[ref.Key()] = entry
	it.Requirements = reqs
	it.Updated = s.now()

	if err := s.validateAndUpgrade(it, had); err != nil {
		return nil, RequirementView{}, err
	}
	if err := s.writeItem(it, ""); err != nil {
		return nil, RequirementView{}, err
	}
	view, err := FindRequirement(it, ref.Number, s.cfg)
	if err != nil {
		return nil, RequirementView{}, err
	}
	return it, view, nil
}
