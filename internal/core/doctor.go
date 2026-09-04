package core

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Duplicate is one id claimed by more than one file: the post-merge collision
// the data model has to survive (E-ID-DUPLICATE, docs/03 section 4.3).
type Duplicate struct {
	ID    ItemID          `json:"id"`
	Files []DuplicateFile `json:"files"`
}

// DuplicateFile is one of the files claiming a duplicated id. The slice inside a
// Duplicate is ordered by the tie-break rule R-RENUM-1: earlier created first,
// then the lexicographically smaller path. The first entry keeps the id.
type DuplicateFile struct {
	Path    string    `json:"path"`
	Type    ItemType  `json:"type,omitempty"`
	Title   string    `json:"title,omitempty"`
	Created Timestamp `json:"created,omitempty"`
}

// Diagnostic renders the duplicate as the E-ID-DUPLICATE finding doctor prints.
func (d Duplicate) Diagnostic() Diagnostic {
	paths := make([]string, 0, len(d.Files))
	for _, f := range d.Files {
		paths = append(paths, f.Path)
	}
	return Diagnostic{
		Code:     CodeIDDuplicate,
		Severity: SeverityError,
		Path:     d.Files[0].Path,
		Field:    "id",
		Message:  fmt.Sprintf("%s claimed by %d files: %s", d.ID, len(d.Files), strings.Join(paths, ", ")),
	}
}

// Renumber is one step of a renumbering plan: the file that loses the contested
// id, and the id it receives.
type Renumber struct {
	OldID   ItemID   `json:"oldId"`
	NewID   ItemID   `json:"newId"`
	Type    ItemType `json:"type"`
	Path    string   `json:"path"`
	NewPath string   `json:"newPath"`
	// Keeper is the file that keeps the old id, empty when the item is
	// renumbered for another reason than a collision.
	Keeper string `json:"keeper,omitempty"`
}

// ReferenceUpdate records one inbound reference rewritten by ApplyRenumber.
type ReferenceUpdate struct {
	Path  string `json:"path"`
	Field string `json:"field"`
	OldID ItemID `json:"oldId"`
	NewID ItemID `json:"newId"`
}

// RenumberResult is what ApplyRenumber did, in the shape doctor prints and the
// commit body records.
type RenumberResult struct {
	Renamed    []Renumber        `json:"renamed"`
	References []ReferenceUpdate `json:"references,omitempty"`
	Redirects  map[ItemID]ItemID `json:"redirects,omitempty"`
	Warnings   []Diagnostic      `json:"warnings,omitempty"`
}

// FindDuplicateIDs scans a backlog and reports every id claimed by more than one
// file. projectDir may be the .pmngr folder or the documentation folder that
// contains it.
func FindDuplicateIDs(fs FS, projectDir string) ([]Duplicate, error) {
	items, err := scanItems(fs, BacklogDir(projectDir))
	if err != nil {
		return nil, err
	}
	return duplicatesOf(items), nil
}

// duplicatesOf groups scanned files by id and keeps the groups of more than one.
func duplicatesOf(items []scannedItem) []Duplicate {
	byID := make(map[ItemID][]scannedItem, len(items))
	for _, it := range items {
		if it.ID == "" {
			continue
		}
		byID[it.ID] = append(byID[it.ID], it)
	}
	var out []Duplicate
	for id, group := range byID {
		if len(group) < 2 {
			continue
		}
		sortByTieBreak(group)
		files := make([]DuplicateFile, 0, len(group))
		for _, it := range group {
			files = append(files, DuplicateFile{
				Path: it.Path, Type: it.Type, Title: it.Title, Created: it.Created,
			})
		}
		out = append(out, Duplicate{ID: id, Files: files})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// sortByTieBreak orders the files claiming one id by R-RENUM-1: the item with
// the earlier created wins; on a tie the lexicographically smaller path wins.
// A file without a created timestamp sorts last, because a file that does not
// say when it was born cannot claim seniority.
func sortByTieBreak(group []scannedItem) {
	sort.Slice(group, func(i, j int) bool {
		a, b := group[i], group[j]
		if a.Created.IsZero() != b.Created.IsZero() {
			return b.Created.IsZero()
		}
		if !a.Created.Equal(b.Created.Time) {
			return a.Created.Before(b.Created.Time)
		}
		return a.Path < b.Path
	})
}

// PlanRenumber turns duplicates into the plan gintrack doctor --renumber prints
// before touching anything: the first file of each group keeps the id, every
// other file receives the next free number of its type.
//
// The allocator is what makes the plan concrete — the next free number cannot be
// known from the duplicates alone — and it hands out successive numbers without
// writing anything, so planning stays side-effect free.
func PlanRenumber(ctx context.Context, dups []Duplicate, alloc *Allocator) ([]Renumber, error) {
	var plan []Renumber
	for _, d := range dups {
		if len(d.Files) < 2 {
			continue
		}
		_, code, _, err := ParseItemID(string(d.ID))
		if err != nil {
			return nil, fmt.Errorf("plan renumber %s: %w", d.ID, err)
		}
		t, ok := ItemTypeFor(code)
		if !ok {
			return nil, fmt.Errorf("plan renumber %s: unknown type code %q", d.ID, code)
		}
		keeper := d.Files[0]
		for _, f := range d.Files[1:] {
			newID, err := alloc.reserveNext(ctx, t)
			if err != nil {
				return nil, fmt.Errorf("plan renumber %s: %w", d.ID, err)
			}
			plan = append(plan, Renumber{
				OldID:   d.ID,
				NewID:   newID,
				Type:    t,
				Path:    f.Path,
				NewPath: renumberedPath(f.Path, d.ID, newID, f.Title),
				Keeper:  keeper.Path,
			})
		}
	}
	return plan, nil
}

// renumberedPath keeps the human-written slug of a file and swaps only the id
// prefix, falling back to the slug of the title when the name has no slug.
func renumberedPath(p string, oldID, newID ItemID, title string) string {
	base := strings.TrimSuffix(path.Base(p), ".md")
	slug := strings.TrimPrefix(base, string(oldID)+"-")
	if slug == base || slug == "" {
		slug = Slugify(title)
	}
	return path.Join(path.Dir(p), string(newID)+"-"+slug+".md")
}

// ApplyRenumber executes a renumbering plan: it renames every file, rewrites the
// id field, updates every inbound parent, milestone and links[].target reference
// and every wikilink in a body, and records the old-to-new mapping in
// project.yaml:id_allocation.redirects (R-RENUM-2).
//
// The whole plan is prepared in memory and only then written, and every file it
// touched is restored when a write fails, so a failure leaves the vault
// unchanged (story GIT-US-0004).
func ApplyRenumber(ctx context.Context, fs FS, projectDir string, plan []Renumber) (RenumberResult, error) {
	var res RenumberResult
	if err := ctx.Err(); err != nil {
		return res, wrapContext("apply renumber", err)
	}
	if len(plan) == 0 {
		return res, nil
	}
	backlog := BacklogDir(projectDir)

	mapping := make(map[ItemID]ItemID, len(plan))
	renumbered := make(map[string]Renumber, len(plan))
	for _, r := range plan {
		if r.OldID == r.NewID {
			return res, fmt.Errorf("apply renumber: %s maps to itself", r.OldID)
		}
		if _, clash := renumbered[r.Path]; clash {
			return res, fmt.Errorf("apply renumber: %s appears twice in the plan", r.Path)
		}
		mapping[r.OldID] = r.NewID
		renumbered[r.Path] = r
	}

	tx := &fileTx{fs: fs}

	// 1. The renumbered files themselves.
	for _, r := range plan {
		data, err := fs.ReadFile(r.Path)
		if err != nil {
			return res, fmt.Errorf("apply renumber: read %s: %w", r.Path, err)
		}
		it, err := ParseItem(r.Path, data)
		if err != nil {
			return res, fmt.Errorf("apply renumber: %s does not parse, fix it first: %w", r.Path, err)
		}
		it.ID = r.NewID
		it.Path = r.NewPath
		rewriteReferences(it, mapping)
		out, err := SerializeItem(it)
		if err != nil {
			return res, err
		}
		tx.write(r.NewPath, out)
		if r.NewPath != r.Path {
			tx.remove(r.Path)
		}
		res.Renamed = append(res.Renamed, r)
		if r.Keeper != "" {
			res.Warnings = append(res.Warnings, Diagnostic{
				Code: CodeIDDuplicate, Severity: SeverityWarning, Path: r.Keeper, Field: "id",
				Message: fmt.Sprintf("%s keeps the id; inbound references to %s now point at %s",
					r.Keeper, r.OldID, r.NewID),
			})
		}
	}

	// 2. Every other item file that references a renumbered id.
	items, err := scanItems(fs, backlog)
	if err != nil {
		return res, err
	}
	for _, scanned := range items {
		if _, isRenumbered := renumbered[scanned.Path]; isRenumbered {
			continue
		}
		data, err := fs.ReadFile(scanned.Path)
		if err != nil {
			return res, fmt.Errorf("apply renumber: read %s: %w", scanned.Path, err)
		}
		it, err := ParseItem(scanned.Path, data)
		if err != nil {
			// A file that does not parse still holds references. Rewrite them
			// textually rather than skipping them, and say so.
			out, updates := rewriteRaw(data, scanned.Path, mapping)
			if len(updates) == 0 {
				continue
			}
			tx.write(scanned.Path, out)
			res.References = append(res.References, updates...)
			res.Warnings = append(res.Warnings, Diagnostic{
				Code: CodeFMYAML, Severity: SeverityWarning, Path: scanned.Path,
				Message: "file does not parse; its references were rewritten as text",
			})
			continue
		}
		updates := rewriteReferences(it, mapping)
		if len(updates) == 0 {
			continue
		}
		out, err := SerializeItem(it)
		if err != nil {
			return res, err
		}
		tx.write(scanned.Path, out)
		res.References = append(res.References, updates...)
	}

	// 3. Comment folders. Which of two colliding items a comment belongs to is
	// not recorded anywhere, so the folder stays with the keeper and the
	// operator is told.
	for _, r := range plan {
		dir := path.Join(backlog, CommentsDirName, string(r.OldID))
		if _, err := fs.Stat(dir); err == nil {
			res.Warnings = append(res.Warnings, Diagnostic{
				Code: CodeWarnCommentsAmbiguous, Severity: SeverityWarning, Path: dir,
				Message: fmt.Sprintf("comments of %s were left in place; move the ones that belong to %s by hand",
					r.OldID, r.NewID),
			})
		} else if !errors.Is(err, ErrNotExist) {
			return res, fmt.Errorf("apply renumber: stat %s: %w", dir, err)
		}
	}

	// 4. The redirect table.
	res.Redirects = mapping
	if err := stageRedirects(fs, backlog, mapping, tx); err != nil {
		return res, err
	}

	if err := tx.commit(); err != nil {
		return RenumberResult{}, err
	}
	sort.Slice(res.References, func(i, j int) bool {
		if res.References[i].Path != res.References[j].Path {
			return res.References[i].Path < res.References[j].Path
		}
		return res.References[i].Field < res.References[j].Field
	})
	return res, nil
}

// CodeWarnCommentsAmbiguous reports comments whose owner cannot be decided after
// a collision repair.
const CodeWarnCommentsAmbiguous Code = "W-CMT-AMBIGUOUS"

// stageRedirects adds the old-to-new mapping to
// project.yaml:id_allocation.redirects, preserving the file's comments and key
// order.
func stageRedirects(fs FS, backlog string, mapping map[ItemID]ItemID, tx *fileTx) error {
	p := path.Join(backlog, ProjectFileName)
	data, err := fs.ReadFile(p)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil
		}
		return fmt.Errorf("apply renumber: read %s: %w", p, err)
	}
	olds := make([]ItemID, 0, len(mapping))
	for old := range mapping {
		olds = append(olds, old)
	}
	sort.Slice(olds, func(i, j int) bool { return olds[i] < olds[j] })
	current := data
	changed := false
	for _, old := range olds {
		out, err := setYAMLPath(current, []string{"id_allocation", "redirects", string(old)}, string(mapping[old]))
		if err != nil {
			return fmt.Errorf("apply renumber: %w", err)
		}
		if out == nil {
			continue
		}
		current = out
		changed = true
	}
	if changed {
		tx.write(p, current)
	}
	return nil
}

// rewriteReferences points every reference of an item at the renumbered ids and
// reports what it changed.
func rewriteReferences(it *Item, mapping map[ItemID]ItemID) []ReferenceUpdate {
	var out []ReferenceUpdate
	remap := func(field string, target *ItemID) {
		if *target == "" {
			return
		}
		to, ok := mapping[*target]
		if !ok {
			return
		}
		out = append(out, ReferenceUpdate{Path: it.Path, Field: field, OldID: *target, NewID: to})
		*target = to
	}
	remap("parent", &it.Parent)
	remap("epic", &it.Epic)
	remap("milestone", &it.Milestone)
	for i := range it.Links {
		target := ItemID(it.Links[i].Target)
		if to, ok := mapping[target]; ok {
			out = append(out, ReferenceUpdate{Path: it.Path, Field: "links", OldID: target, NewID: to})
			it.Links[i].Target = string(to)
		}
	}
	body, changed := rewriteWikilinks(it.Body, mapping)
	it.Body = body
	for _, old := range changed {
		out = append(out, ReferenceUpdate{Path: it.Path, Field: "body", OldID: old, NewID: mapping[old]})
	}
	return out
}

// wikilinkTargetRE matches the target part of a wikilink, up to the alias pipe
// or the heading anchor (docs/03 section 14.1).
var wikilinkTargetRE = regexp.MustCompile(`\[\[[^\[\]\n|#]+`)

// rewriteWikilinks repoints [[OLD-ID]], [[OLD-ID|text]] and [[OLD-ID#comment]]
// at the new id, and reports which ids it changed, sorted (R-WIKI-3).
func rewriteWikilinks(body string, mapping map[ItemID]ItemID) (string, []ItemID) {
	if body == "" || len(mapping) == 0 {
		return body, nil
	}
	seen := make(map[ItemID]bool)
	out := wikilinkTargetRE.ReplaceAllStringFunc(body, func(m string) string {
		target := ItemID(strings.TrimSpace(strings.TrimPrefix(m, "[[")))
		to, ok := mapping[target]
		if !ok {
			return m
		}
		seen[target] = true
		return "[[" + string(to)
	})
	changed := make([]ItemID, 0, len(seen))
	for id := range seen {
		changed = append(changed, id)
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i] < changed[j] })
	return out, changed
}

// rewriteRaw rewrites the ids of a file that does not parse, as plain text.
func rewriteRaw(data []byte, p string, mapping map[ItemID]ItemID) ([]byte, []ReferenceUpdate) {
	text := string(data)
	var out []ReferenceUpdate
	olds := make([]ItemID, 0, len(mapping))
	for old := range mapping {
		olds = append(olds, old)
	}
	sort.Slice(olds, func(i, j int) bool { return olds[i] < olds[j] })
	for _, old := range olds {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(string(old)) + `\b`)
		if !re.MatchString(text) {
			continue
		}
		text = re.ReplaceAllString(text, string(mapping[old]))
		out = append(out, ReferenceUpdate{Path: p, Field: "text", OldID: old, NewID: mapping[old]})
	}
	return []byte(text), out
}

// fileTx stages writes and removals so that a plan is applied all at once and
// rolled back when one of the writes fails.
type fileTx struct {
	fs      FS
	writes  []stagedWrite
	removes []string

	// done records what has to be undone on rollback.
	created  []string
	restored []stagedWrite
}

// stagedWrite is one pending file write.
type stagedWrite struct {
	path string
	data []byte
}

// write stages the content of a file.
func (t *fileTx) write(p string, data []byte) {
	for i := range t.writes {
		if t.writes[i].path == p {
			t.writes[i].data = data
			return
		}
	}
	t.writes = append(t.writes, stagedWrite{path: p, data: data})
}

// remove stages the removal of a file.
func (t *fileTx) remove(p string) { t.removes = append(t.removes, p) }

// commit applies the staged operations, restoring everything it touched when one
// of them fails.
func (t *fileTx) commit() error {
	for _, w := range t.writes {
		previous, err := t.fs.ReadFile(w.path)
		switch {
		case err == nil:
			t.restored = append(t.restored, stagedWrite{path: w.path, data: previous})
		case errors.Is(err, ErrNotExist):
			t.created = append(t.created, w.path)
		default:
			return t.rollback(fmt.Errorf("apply: read %s: %w", w.path, err))
		}
		if err := t.fs.MkdirAll(path.Dir(w.path)); err != nil {
			return t.rollback(fmt.Errorf("apply: mkdir %s: %w", path.Dir(w.path), err))
		}
		if err := writeFileAtomic(t.fs, w.path, w.data); err != nil {
			return t.rollback(err)
		}
	}
	for _, p := range t.removes {
		previous, err := t.fs.ReadFile(p)
		if err != nil {
			if errors.Is(err, ErrNotExist) {
				continue
			}
			return t.rollback(fmt.Errorf("apply: read %s: %w", p, err))
		}
		if err := t.fs.Remove(p); err != nil {
			return t.rollback(fmt.Errorf("apply: remove %s: %w", p, err))
		}
		t.restored = append(t.restored, stagedWrite{path: p, data: previous})
	}
	return nil
}

// rollback restores every file the commit had already changed and returns the
// original error, joined with anything that went wrong while undoing.
func (t *fileTx) rollback(cause error) error {
	errs := []error{cause}
	for _, w := range t.restored {
		if err := t.fs.WriteFile(w.path, w.data); err != nil {
			errs = append(errs, fmt.Errorf("rollback %s: %w", w.path, err))
		}
	}
	for _, p := range t.created {
		if err := t.fs.Remove(p); err != nil && !errors.Is(err, ErrNotExist) {
			errs = append(errs, fmt.Errorf("rollback %s: %w", p, err))
		}
	}
	return errors.Join(errs...)
}
