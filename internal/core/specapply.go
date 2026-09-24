package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// This file applies a story's or a task's "## Spec Delta" when the item moves
// to a done-category status (ADR-037 section 9, docs/03 R-DELTA-12 to
// R-DELTA-16, GIT-US-0110). The transition, the rewritten item and every spec
// the delta changes are prepared in memory first, validated together, and
// written as one staged file transaction: a refused application changes no
// file at all. Like the rest of the store it is pure text processing over the
// injected FS, so it compiles to WebAssembly unchanged (ADR-003).

// SpecDeltaConflictCode is the machine code of a refused Spec Delta
// application. It reuses the catalogue's generic "conflict" (docs/07 section
// 4.3): the delta disagrees with the specs as they stand.
const SpecDeltaConflictCode = "conflict"

// ErrSpecDeltaConflict classifies a Spec Delta that cannot be applied.
var ErrSpecDeltaConflict = errors.New("spec delta conflict")

// SpecDeltaError refuses a done transition whose Spec Delta cannot be applied:
// a missing spec or block, a requirement the delta changes twice, a MODIFIED
// naming a requirement that was already removed, a workflow without a
// cancelled-category status, or an error-severity finding of the delta itself.
// Nothing was written.
type SpecDeltaError struct {
	ID   ItemID
	Path string
	// Target is the spec or requirement the refused operation names, if any.
	Target  string
	Message string
}

// Error implements the error interface.
func (e *SpecDeltaError) Error() string {
	if e.Target != "" {
		return fmt.Sprintf("apply the Spec Delta of %s: %s: %s", e.ID, e.Target, e.Message)
	}
	return fmt.Sprintf("apply the Spec Delta of %s: %s", e.ID, e.Message)
}

// Unwrap reports ErrSpecDeltaConflict so callers can classify with errors.Is.
func (e *SpecDeltaError) Unwrap() error { return ErrSpecDeltaConflict }

// Code returns the machine code of this problem.
func (e *SpecDeltaError) Code() string { return SpecDeltaConflictCode }

// AppliedAddition is one ADDED operation after it was applied: the ref it was
// given and, for a move, the requirement it supersedes.
type AppliedAddition struct {
	Ref        RequirementRef  `json:"ref"`
	Supersedes *RequirementRef `json:"supersedes,omitempty"`
}

// DeltaApplication reports what applying a Spec Delta changed. Every list is in
// the order of the operations in the body.
type DeltaApplication struct {
	Item ItemID `json:"item"`
	// Added are the requirements the delta created, with their new refs.
	Added []AppliedAddition `json:"added,omitempty"`
	// Modified are the requirements whose block text was replaced; their block
	// rev changed, so a verified stamp of the old text is now suspect.
	Modified []RequirementRef `json:"modified,omitempty"`
	// Removed are the requirements moved to a cancelled-category status by a
	// REMOVED operation or a Supersedes: line.
	Removed []RequirementRef `json:"removed,omitempty"`
	// Unchanged are the requirements an operation names that already held
	// what the operation asks for: the delta had been applied before.
	Unchanged []RequirementRef `json:"unchanged,omitempty"`
	// Specs are the spec files written.
	Specs []ItemID `json:"specs,omitempty"`
	// Stamped are the requirements the DoneHook stamped `verified` in the
	// same write (ADR-037 section 7, R-REQ-11a (a), GIT-US-0141).
	Stamped []StampedRequirement `json:"stamped,omitempty"`
	// Unstamped are the requirements the item implements or modifies that
	// were left without a new stamp, each with its reason. Leaving one
	// unstamped never refuses the transition.
	Unstamped []UnstampedRequirement `json:"unstamped,omitempty"`
}

// StampedRequirement is a requirement whose `verified` stamp was written.
type StampedRequirement struct {
	Ref      RequirementRef `json:"ref"`
	Verified Verification   `json:"verified"`
}

// UnstampedRequirement is a requirement left without a new stamp, and why
// (docs/03 R-REQ-12a: failed, partial, no-results, no-tests, no-commit,
// mixed-commits, text, unchanged, stamp-newer, unavailable, stale, and on
// done also missing and removed).
type UnstampedRequirement struct {
	Ref    RequirementRef `json:"ref"`
	Reason string         `json:"reason"`
}

// empty reports whether the application changed nothing at all.
func (a *DeltaApplication) empty() bool {
	return a == nil || (len(a.Added) == 0 && len(a.Modified) == 0 && len(a.Removed) == 0 && len(a.Specs) == 0)
}

// DoneTransition is what a DoneHook sees: the story or task entering a
// done-category status, after its Spec Delta was applied in memory and before
// anything is validated or written.
type DoneTransition struct {
	// Context is the context of the write.
	Context context.Context //nolint:containedctx // lives for one DoneHook call
	// Item is the item as it will be written.
	Item *Item
	// Config is the configuration of the item's project.
	Config *ProjectConfig
	// Refs are the requirements the item implements or modifies after the
	// delta was applied, from its front-matter links.
	Refs []RequirementRef
	// Delta reports what the delta changed; nil when the item has none.
	Delta *DeltaApplication
	// Spec returns a spec of this project staged for the same write, loading
	// it on first use. A hook changes the returned item in place (its
	// requirements: entries); the change is validated and written together
	// with the transition, or not at all.
	Spec func(id ItemID) (*Item, error)
}

// DoneHook runs inside the write that moves a story or a task to a
// done-category status, after its Spec Delta was applied and before the files
// are validated and written. An error refuses the whole transition.
//
// It is the seam of the durable verification stamp (ADR-037 section 7,
// R-REQ-11a (a), GIT-US-0141): the vault installs a hook that copies the most
// recent passing verification-cache entry into requirements.R<n>.verified of
// each requirement in Refs (StageVerified), and records in Delta.Stamped and
// Delta.Unstamped what it did.
type DoneHook func(t *DoneTransition) error

// StageVerified sets requirements.R<n>.verified of a staged spec to v, the
// way a requirement write does: it is written together with the transition.
func (t *DoneTransition) StageVerified(spec *Item, n int, v Verification) error {
	return applyRequirementPatch(spec, n, RequirementPatch{Verified: &v}, t.Config)
}

// deltaPlan is a done transition prepared in memory: the specs it changes,
// staged, and the revs they were read at.
type deltaPlan struct {
	store *FileStore
	item  *Item
	// specs holds every spec read for the transition; order is the order they
	// were first read in, which is the order they are written in.
	specs map[ItemID]*Item
	order []ItemID
	read  map[string]Rev
	// orig is the canonical serialization of each spec as read; changed marks
	// the specs whose serialization differs from it once the delta and the
	// hook ran.
	orig    map[ItemID][]byte
	changed map[ItemID]bool
	result  *DeltaApplication
}

// spec returns a spec of the store's project staged for the transition,
// reading it on first use.
func (p *deltaPlan) spec(id ItemID) (*Item, error) {
	if it, ok := p.specs[id]; ok {
		return it, nil
	}
	if key, _, _, err := ParseItemID(string(id)); err == nil && p.store.cfg != nil && p.store.cfg.Key != "" && key != p.store.cfg.Key {
		return nil, p.conflict(string(id), "the spec belongs to project %s; a Spec Delta applies to the specs of its own project (%s) only", key, p.store.cfg.Key)
	}
	it, err := p.store.readSpec(id)
	if err != nil {
		if errors.Is(err, ErrItemNotFound) {
			return nil, p.conflict(string(id), "the spec does not exist")
		}
		return nil, err
	}
	it.Requirements = it.Requirements.Clone()
	orig, err := SerializeItem(it)
	if err != nil {
		return nil, err
	}
	p.orig[id] = orig
	p.specs[id] = it
	p.order = append(p.order, id)
	p.read[it.Path] = it.Rev
	return it, nil
}

// conflict builds the refusal of the transition.
func (p *deltaPlan) conflict(target, format string, args ...any) error {
	return &SpecDeltaError{ID: p.item.ID, Path: p.item.Path, Target: target, Message: fmt.Sprintf(format, args...)}
}

// entersDone reports whether a status change moves a story or a task into the
// done category from outside it: the moment its Spec Delta is applied.
func (s *FileStore) entersDone(it *Item, from, to Status) bool {
	if s.cfg == nil || !carriesSpecDelta(it.Type) {
		return false
	}
	return s.cfg.CategoryOf(to) == CategoryDone && s.cfg.CategoryOf(from) != CategoryDone
}

// cancelledStatus is the first cancelled-category status of the workflow, the
// status a REMOVED requirement takes (R-DELTA-3).
func (s *FileStore) cancelledStatus() (Status, bool) {
	if s.cfg == nil {
		return "", false
	}
	for _, st := range s.cfg.Workflow.Statuses {
		if st.Category == CategoryCancelled {
			return st.ID, true
		}
	}
	return "", false
}

// planDone prepares the done transition of it: its Spec Delta applied to the
// staged specs and to its own body and links, then the DoneHook. It returns nil
// when there is nothing to do beyond the ordinary write: no delta operation and
// no hook. it is changed in place.
func (s *FileStore) planDone(ctx context.Context, it *Item) (*deltaPlan, error) {
	body := normalizeNewlines(it.Body)
	delta := ParseSpecDelta(body)
	if len(delta.Operations) == 0 && len(delta.Findings) == 0 && s.DoneHook == nil {
		return nil, nil
	}
	p := &deltaPlan{
		store: s, item: it,
		specs: map[ItemID]*Item{}, read: map[string]Rev{}, orig: map[ItemID][]byte{}, changed: map[ItemID]bool{},
		result: &DeltaApplication{Item: it.ID},
	}
	for _, f := range delta.Findings {
		if f.Severity == SeverityError {
			return nil, p.conflict(f.Ref, "line %d of the body: %s (%s)", f.Line, f.Message, f.Code)
		}
	}
	if err := p.checkTargets(delta); err != nil {
		return nil, err
	}

	var rewrites []headingRewrite
	for _, op := range delta.Operations {
		var err error
		switch op.Op {
		case DeltaAdded:
			var rw *headingRewrite
			rw, err = p.applyAdded(op, body)
			if rw != nil {
				rewrites = append(rewrites, *rw)
			}
		case DeltaModified:
			err = p.applyModified(op)
		case DeltaRemoved:
			err = p.removeRequirement(*op.Ref, op.Ref.String())
		}
		if err != nil {
			return nil, err
		}
	}
	// Record the numbers in the body: ### ADDED <REQREF> — <title> (R-DELTA-9).
	// Rewrites run back to front so earlier offsets stay valid.
	sort.Slice(rewrites, func(i, j int) bool { return rewrites[i].start > rewrites[j].start })
	for _, rw := range rewrites {
		body = body[:rw.start] + rw.line + body[rw.end:]
	}
	if len(rewrites) > 0 {
		it.Body = body
	}

	if s.DoneHook != nil {
		if err := s.DoneHook(&DoneTransition{
			Context: ctx, Item: it, Config: s.cfg, Refs: workRefs(it.Links), Delta: p.result,
			Spec: p.spec,
		}); err != nil {
			return nil, err
		}
	}
	for _, id := range p.order {
		data, err := SerializeItem(p.specs[id])
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(data, p.orig[id]) {
			p.changed[id] = true
			p.result.Specs = append(p.result.Specs, id)
		}
	}
	return p, nil
}

// checkTargets refuses a delta that changes one requirement twice: MODIFIED
// more than once, or MODIFIED and removed in the same delta. A REMOVED of the
// requirement an ADDED supersedes is the explicit spelling of a move and is
// allowed.
func (p *deltaPlan) checkTargets(delta SpecDelta) error {
	modified := map[RequirementRef]bool{}
	removed := map[RequirementRef]bool{}
	for _, op := range delta.Operations {
		switch op.Op {
		case DeltaModified:
			if modified[*op.Ref] || removed[*op.Ref] {
				return p.conflict(op.Ref.String(), "the delta changes this requirement more than once")
			}
			modified[*op.Ref] = true
		case DeltaRemoved:
			if modified[*op.Ref] {
				return p.conflict(op.Ref.String(), "the delta both modifies and removes this requirement")
			}
			removed[*op.Ref] = true
		case DeltaAdded:
			if op.Supersedes != nil {
				if modified[*op.Supersedes] {
					return p.conflict(op.Supersedes.String(), "the delta both modifies and supersedes this requirement")
				}
				removed[*op.Supersedes] = true
			}
		}
	}
	return nil
}

// headingRewrite replaces one ADDED heading of the body with its applied form.
type headingRewrite struct {
	start, end int
	line       string
}

// operationText is the block text an ADDED or MODIFIED operation proposes: the
// operation below its heading, without the Supersedes: line.
func operationText(op DeltaOperation) string {
	_, rest, _ := strings.Cut(op.lintText, "\n")
	return rest
}

// applyAdded appends the block of an ADDED operation to its spec under a new
// number, records the supersedes link of a move and removes the superseded
// requirement. An operation that already carries its number was applied
// before: only the item's link is made sure of.
func (p *deltaPlan) applyAdded(op DeltaOperation, body string) (*headingRewrite, error) {
	if op.Ref != nil {
		p.result.Unchanged = append(p.result.Unchanged, *op.Ref)
		p.link(LinkImplements, *op.Ref)
		if op.Supersedes != nil {
			p.link(LinkModifies, *op.Supersedes)
		}
		return nil, nil
	}
	spec, err := p.spec(op.Spec)
	if err != nil {
		return nil, err
	}
	var old *RequirementView
	if op.Supersedes != nil {
		oldSpec, err := p.spec(op.Supersedes.Spec)
		if err != nil {
			return nil, err
		}
		view, err := FindRequirement(oldSpec, op.Supersedes.Number, p.store.cfg)
		if err != nil {
			return nil, p.conflict(op.Supersedes.String(), "Supersedes: names a requirement with no block in %s", oldSpec.ID)
		}
		old = &view
	}
	var inbound []RequirementRef
	if p.store.RequirementRefs != nil {
		inbound = p.store.RequirementRefs(spec.ID)
	}
	ref := RequirementRef{Spec: spec.ID, Number: NextRequirementNumber(spec, inbound)}
	title, err := cleanRequirementTitle(spec, ref, op.Title)
	if err != nil {
		return nil, err
	}
	text, err := cleanRequirementText(spec, ref, operationText(op))
	if err != nil {
		return nil, err
	}
	spec.Body = insertRequirementBlock(spec.Body, spec.ID, renderRequirementBlock(RequirementHeading(ref, title), text))
	entry := &Requirement{}
	if p.store.cfg != nil {
		entry.Status = p.store.cfg.InitialStatus()
	}
	if old != nil {
		entry.Links = []Link{{Kind: LinkSupersedes, Target: old.Ref.String()}}
	}
	if spec.Requirements == nil {
		spec.Requirements = Requirements{}
	}
	spec.Requirements[ref.Key()] = entry
	p.result.Added = append(p.result.Added, AppliedAddition{Ref: ref, Supersedes: op.Supersedes})
	p.link(LinkImplements, ref)

	if old != nil {
		if err := p.removeRequirement(old.Ref, "Supersedes: "+old.Ref.String()); err != nil {
			return nil, err
		}
	}
	heading := "### " + string(DeltaAdded) + " " + ref.String() + " " + ReqSeparator + " " + title
	lineEnd := strings.IndexByte(body[op.Start:], '\n')
	end := len(body)
	if lineEnd >= 0 {
		end = op.Start + lineEnd
	}
	return &headingRewrite{start: op.Start, end: end, line: heading}, nil
}

// applyModified replaces the block of a MODIFIED operation's target with the
// proposed title and text. The verified stamp is left alone: the block rev
// changes, so the stamp becomes suspect until re-verified (R-DELTA-2).
func (p *deltaPlan) applyModified(op DeltaOperation) error {
	ref := *op.Ref
	spec, err := p.spec(ref.Spec)
	if err != nil {
		return err
	}
	view, err := FindRequirement(spec, ref.Number, p.store.cfg)
	if err != nil {
		return p.conflict(ref.String(), "MODIFIED names a requirement with no block in %s", spec.ID)
	}
	title, err := cleanRequirementTitle(spec, ref, op.Title)
	if err != nil {
		return err
	}
	text, err := cleanRequirementText(spec, ref, operationText(op))
	if err != nil {
		return err
	}
	p.link(LinkModifies, ref)
	if view.Title == title && view.Text == text {
		p.result.Unchanged = append(p.result.Unchanged, ref)
		return nil
	}
	if p.store.cfg != nil && p.store.cfg.CategoryOf(view.Status) == CategoryCancelled {
		return p.conflict(ref.String(), "MODIFIED names a requirement that was removed (status %s)", view.Status)
	}
	if err := applyRequirementPatch(spec, ref.Number, RequirementPatch{Title: &title, Text: &text}, p.store.cfg); err != nil {
		return err
	}
	p.result.Modified = append(p.result.Modified, ref)
	return nil
}

// removeRequirement moves a requirement to the first cancelled-category status,
// keeping its block and its number (R-DELTA-3, R-REQ-6). A requirement already
// in a cancelled-category status is left as it is.
func (p *deltaPlan) removeRequirement(ref RequirementRef, why string) error {
	spec, err := p.spec(ref.Spec)
	if err != nil {
		return err
	}
	view, err := FindRequirement(spec, ref.Number, p.store.cfg)
	if err != nil {
		return p.conflict(ref.String(), "%s names a requirement with no block in %s", why, spec.ID)
	}
	p.link(LinkModifies, ref)
	if p.store.cfg != nil && p.store.cfg.CategoryOf(view.Status) == CategoryCancelled && !view.StatusImplicit {
		p.result.Unchanged = append(p.result.Unchanged, ref)
		return nil
	}
	status, ok := p.store.cancelledStatus()
	if !ok {
		return p.conflict(ref.String(), "the workflow declares no cancelled-category status for a removed requirement")
	}
	if err := applyRequirementPatch(spec, ref.Number, RequirementPatch{Status: &status}, p.store.cfg); err != nil {
		return err
	}
	p.result.Removed = append(p.result.Removed, ref)
	return nil
}

// link adds an implements or modifies link from the item to a requirement,
// unless the item already declares it (R-DELTA-4).
func (p *deltaPlan) link(kind LinkKind, ref RequirementRef) {
	p.item.Links = addLinks(p.item.Links, []Link{{Kind: kind, Target: ref.String()}})
}

// workRefs returns the requirements a list of links implements or modifies,
// each once, in link order.
func workRefs(links []Link) []RequirementRef {
	seen := map[RequirementRef]bool{}
	var out []RequirementRef
	for _, l := range links {
		if l.Kind != LinkImplements && l.Kind != LinkModifies {
			continue
		}
		ref, err := ParseRequirementRef(bareTarget(l.Target))
		if err != nil || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out
}

// writeDone validates and writes a prepared done transition as one staged
// file transaction: the item first, then every changed spec, then
// project.yaml when the transition raises the schema. had says whether the
// item held a spec construct before the write.
//
// Refusals — a validation error on any file, a file changed on disk since it
// was read — happen before the first byte is written. A failure while writing
// rolls back the files already written. What no staged write can cover is a
// crash of the process between two renames: the item is written first, so such
// a crash leaves it done and pointing at a requirement its spec does not hold
// yet, which the index reports as W-REF-DANGLING rather than silently
// allocating the numbers again on a retry.
func (s *FileStore) writeDone(it *Item, oldPath string, had bool, p *deltaPlan) error {
	var specs []*Item
	for _, id := range p.order {
		if p.changed[id] {
			specs = append(specs, p.specs[id])
		}
	}
	now := it.Updated
	gains := !had && HasSpecConstruct(it)
	upgrade := s.cfg != nil && s.cfg.Schema > 0 && s.cfg.Schema < SpecSchema && gains
	cfg := s.cfg
	if upgrade {
		upgraded := *s.cfg
		upgraded.Schema = SpecSchema
		cfg = &upgraded
	}
	if err := s.validateWith(it, cfg); err != nil {
		return err
	}
	tx := &fileTx{fs: s.fs}
	data, err := SerializeItem(it)
	if err != nil {
		return err
	}
	tx.write(it.Path, data)
	if oldPath != "" && oldPath != it.Path {
		tx.remove(oldPath)
	}
	specData := make([][]byte, len(specs))
	for i, spec := range specs {
		spec.Updated = now
		if err := s.validateWith(spec, cfg); err != nil {
			return err
		}
		if specData[i], err = SerializeItem(spec); err != nil {
			return err
		}
		tx.write(spec.Path, specData[i])
	}
	if upgrade {
		pyaml := path.Join(s.backlog, ProjectFileName)
		raw, err := s.fs.ReadFile(pyaml)
		switch {
		case errors.Is(err, ErrNotExist):
		case err != nil:
			return fmt.Errorf("upgrade schema: read %s: %w", pyaml, err)
		default:
			out, err := UpgradeProjectSchema(raw, SpecSchema)
			if err != nil {
				return err
			}
			if out != nil {
				tx.write(pyaml, out)
			}
		}
	}
	// The specs were read under the store's lock, but a hand edit or another
	// process may have changed one since: the file-level rev check of
	// docs/08 section 10, immediately before writing.
	for _, spec := range specs {
		current, err := s.fs.ReadFile(spec.Path)
		if err != nil {
			return fmt.Errorf("read %s: %w", spec.Path, err)
		}
		if rev := ComputeRev(current); rev != p.read[spec.Path] {
			return &StaleRevisionError{ID: spec.ID, Path: spec.Path, Expected: p.read[spec.Path], Current: rev}
		}
	}
	if err := s.fs.MkdirAll(path.Dir(it.Path)); err != nil {
		return fmt.Errorf("write %s: %w", it.Path, err)
	}
	if err := tx.commit(); err != nil {
		return err
	}
	it.Rev = ComputeRev(data)
	for i, spec := range specs {
		spec.Rev = ComputeRev(specData[i])
	}
	if upgrade {
		s.cfg.Schema = SpecSchema
	}
	return nil
}

// report returns what the transition applied, or nil when its delta changed
// and named nothing.
func (p *deltaPlan) report() *DeltaApplication {
	if p.result.empty() && len(p.result.Unchanged) == 0 && len(p.result.Unstamped) == 0 && len(p.result.Stamped) == 0 {
		return nil
	}
	return p.result
}
