package core

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
)

// Store is CRUD over the files of a vault. It never mutates the index directly;
// the index observes the change notifications the caller emits.
//
// This is the interface of docs/07 section 6.5. The knowledge-base page type is
// named KBPage rather than Page because Page[T] is the paginated result type of
// the read side.
type Store interface {
	Get(ctx context.Context, id ItemID) (*Item, error)
	GetRaw(ctx context.Context, id ItemID) (frontMatter, body []byte, rev Rev, err error)
	Create(ctx context.Context, draft ItemDraft) (*Item, error)
	Update(ctx context.Context, id ItemID, patch ItemPatch, expected Rev) (*Item, error)
	Delete(ctx context.Context, id ItemID, expected Rev) error
	Move(ctx context.Context, id ItemID, status Status, expected Rev) (*Item, error)
	AddComment(ctx context.Context, id ItemID, c CommentDraft) (*Comment, error)
	ListComments(ctx context.Context, id ItemID) ([]Comment, error)
	ReadPage(ctx context.Context, project ProjectKey, p string) (*KBPage, error)
	WritePage(ctx context.Context, project ProjectKey, p string, content []byte, expected Rev) (*KBPage, error)
}

// ErrStaleRevision reports a conditional write whose expected rev no longer
// matches the bytes on disk. It is the sentinel behind the RFC 7807 code
// "stale_revision" (docs/07 section 5.4) and is an alias of ErrRevMismatch, so a
// caller may test for either.
var ErrStaleRevision = ErrRevMismatch

// ErrTransitionDenied reports a status change the workflow does not allow.
var ErrTransitionDenied = errors.New("workflow transition denied")

// StaleRevisionCode is the RFC 7807 machine code of a rev mismatch.
const StaleRevisionCode = "stale_revision"

// TransitionDeniedCode is the RFC 7807 machine code of a refused status change.
const TransitionDeniedCode = "workflow_transition_denied"

// StaleRevisionError carries the current rev of a file so that a client or an
// agent can retry the merge without a second round trip (R-REV-3).
type StaleRevisionError struct {
	ID       ItemID
	Path     string
	Expected Rev
	Current  Rev
}

// Error implements the error interface.
func (e *StaleRevisionError) Error() string {
	return fmt.Sprintf("%s was modified on disk since revision %s (current %s)", e.subject(), e.Expected, e.Current)
}

func (e *StaleRevisionError) subject() string {
	if e.ID != "" {
		return string(e.ID)
	}
	return e.Path
}

// Unwrap reports ErrStaleRevision so that callers can classify with errors.Is.
func (e *StaleRevisionError) Unwrap() error { return ErrRevMismatch }

// Code returns the RFC 7807 machine code of this problem.
func (e *StaleRevisionError) Code() string { return StaleRevisionCode }

// TransitionError reports a status change the project workflow forbids.
type TransitionError struct {
	ID      ItemID
	From    Status
	To      Status
	Allowed []Status
}

// Error implements the error interface.
func (e *TransitionError) Error() string {
	if len(e.Allowed) == 0 {
		return fmt.Sprintf("%s: transition %s -> %s is not allowed", e.ID, e.From, e.To)
	}
	allowed := make([]string, 0, len(e.Allowed))
	for _, s := range e.Allowed {
		allowed = append(allowed, string(s))
	}
	return fmt.Sprintf("%s: transition %s -> %s is not allowed (allowed: %s)",
		e.ID, e.From, e.To, strings.Join(allowed, ", "))
}

// Unwrap reports ErrTransitionDenied so that callers can classify with errors.Is.
func (e *TransitionError) Unwrap() error { return ErrTransitionDenied }

// Code returns the RFC 7807 machine code of this problem.
func (e *TransitionError) Code() string { return TransitionDeniedCode }

// ItemDraft is the input of Store.Create. The id, the timestamps and the file
// path are supplied by the store; everything else comes from the caller, and the
// project defaults fill the gaps (R-DEFAULT, docs/03 section 13.3).
type ItemDraft struct {
	Type      ItemType `json:"type"`
	Title     string   `json:"title"`
	Status    Status   `json:"status,omitempty"`
	Priority  Priority `json:"priority,omitempty"`
	Parent    ItemID   `json:"parent,omitempty"`
	Milestone ItemID   `json:"milestone,omitempty"`
	Sprint    string   `json:"sprint,omitempty"`

	Assignees []string `json:"assignees,omitempty"`
	Author    string   `json:"author,omitempty"`
	Owner     string   `json:"owner,omitempty"`
	Labels    []string `json:"labels,omitempty"`

	Estimate *float64 `json:"estimate,omitempty"`
	Effort   *float64 `json:"effort,omitempty"`

	Start Date `json:"start,omitempty"`
	Due   Date `json:"due,omitempty"`

	Links       []Link         `json:"links,omitempty"`
	Attachments []string       `json:"attachments,omitempty"`
	Custom      map[string]any `json:"custom,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`

	Body string `json:"body,omitempty"`

	// ID pins the identifier instead of allocating one. It exists for importers
	// and for tests; normal callers leave it empty.
	ID ItemID `json:"id,omitempty"`
}

// ItemPatch is the input of Store.Update: a sparse description of a change.
// A nil pointer means "leave this field alone"; Unset names the fields to clear;
// the Add/Remove slices are set operations so that two concurrent clients do not
// clobber each other's lists (docs/07 section 5.5).
type ItemPatch struct {
	Title     *string   `json:"title,omitempty"`
	Status    *Status   `json:"status,omitempty"`
	Priority  *Priority `json:"priority,omitempty"`
	Parent    *ItemID   `json:"parent,omitempty"`
	Milestone *ItemID   `json:"milestone,omitempty"`
	Sprint    *string   `json:"sprint,omitempty"`
	Author    *string   `json:"author,omitempty"`
	Owner     *string   `json:"owner,omitempty"`

	Assignees       *[]string `json:"assignees,omitempty"`
	AddAssignees    []string  `json:"addAssignees,omitempty"`
	RemoveAssignees []string  `json:"removeAssignees,omitempty"`
	Labels          *[]string `json:"labels,omitempty"`
	AddLabels       []string  `json:"addLabels,omitempty"`
	RemoveLabels    []string  `json:"removeLabels,omitempty"`

	Estimate *float64 `json:"estimate,omitempty"`
	Effort   *float64 `json:"effort,omitempty"`
	Spent    *float64 `json:"spent,omitempty"`

	Start *Date `json:"start,omitempty"`
	Due   *Date `json:"due,omitempty"`

	Links          *[]Link  `json:"links,omitempty"`
	AddLinks       []Link   `json:"addLinks,omitempty"`
	RemoveLinks    []Link   `json:"removeLinks,omitempty"`
	AddAttachments []string `json:"addAttachments,omitempty"`

	// Custom merges into the custom mapping; a nil value removes a key.
	Custom map[string]any `json:"custom,omitempty"`

	// Body replaces the whole body; BodyAppend appends to it.
	Body       *string `json:"body,omitempty"`
	BodyAppend string  `json:"bodyAppend,omitempty"`

	// Deleted flips the soft-delete flag.
	Deleted *bool `json:"deleted,omitempty"`

	// Unset names the front-matter fields to clear, e.g. "milestone", "due".
	Unset []string `json:"unset,omitempty"`
}

// DeleteOptions selects how an item is removed. The default is a soft delete:
// the file keeps its id and history and gains `deleted: true` (docs/03 section
// 7.1), so that a merge never resurrects a stale copy.
type DeleteOptions struct {
	Hard bool `json:"hard,omitempty"`
}

// MoveOptions tunes a status change.
type MoveOptions struct {
	// Force bypasses the declared workflow transitions (gintrack item move
	// --force). The workflow status itself must still exist.
	Force bool `json:"force,omitempty"`
}

// FileStore implements Store over an FS. Writes are serialized by a mutex and
// land atomically: the bytes go to "<name>.tmp" first and are then renamed over
// the target (docs/06 section 3.1, step 4).
type FileStore struct {
	mu      sync.Mutex
	fs      FS
	backlog string // the .pmngr folder
	docs    string // the documentation folder that contains it
	cfg     *ProjectConfig
	alloc   *Allocator

	// Clock supplies created/updated. It defaults to SystemClock.
	Clock Clock

	// RenameOnTitleChange renames the file when the title changes, keeping the
	// id (R-SLUG-2). It defaults to true.
	RenameOnTitleChange bool

	// Validate runs the validator before every write, which is step 1 of the
	// write path (docs/06 section 3.1): an invalid item never reaches the disk.
	// It defaults to true and is only turned off by an importer that knowingly
	// writes items it will repair afterwards.
	Validate bool
}

// Ensure FileStore satisfies the interface at compile time.
var _ Store = (*FileStore)(nil)

// NewStore returns a store for the project rooted at projectDir, which may be
// either the .pmngr folder or the documentation folder that contains it.
func NewStore(fs FS, projectDir string, cfg *ProjectConfig) *FileStore {
	backlog := BacklogDir(projectDir)
	return &FileStore{
		fs:                  fs,
		backlog:             backlog,
		docs:                path.Dir(backlog),
		cfg:                 cfg,
		alloc:               NewAllocator(fs, backlog, cfg),
		Clock:               SystemClock,
		RenameOnTitleChange: true,
		Validate:            true,
	}
}

// Allocator returns the id allocator the store creates items with, so that a
// caller can share one allocator across a batch or set the acting user.
func (s *FileStore) Allocator() *Allocator { return s.alloc }

// SetAllocator replaces the id allocator.
func (s *FileStore) SetAllocator(a *Allocator) { s.alloc = a }

// BacklogPath returns the .pmngr folder the store writes to.
func (s *FileStore) BacklogPath() string { return s.backlog }

// DocsPath returns the documentation folder the knowledge base lives in.
func (s *FileStore) DocsPath() string { return s.docs }

// now returns the current time from the injected clock, truncated to the second.
func (s *FileStore) now() Timestamp {
	c := s.Clock
	if c == nil {
		c = SystemClock
	}
	return NewTimestamp(c.Now())
}

// Get returns one item by id, following the redirects left by a renumber
// (R-RENUM-2).
func (s *FileStore) Get(ctx context.Context, id ItemID) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("get", err)
	}
	found, err := s.locate(id)
	if err != nil {
		return nil, err
	}
	return s.read(found.Path)
}

// read parses one item file.
func (s *FileStore) read(p string) (*Item, error) {
	data, err := s.fs.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return ParseItem(p, data)
}

// GetRaw returns the unparsed front matter, the body and the rev of an item.
func (s *FileStore) GetRaw(ctx context.Context, id ItemID) (frontMatter, body []byte, rev Rev, err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, "", wrapContext("get raw", err)
	}
	found, err := s.locate(id)
	if err != nil {
		return nil, nil, "", err
	}
	data, err := s.fs.ReadFile(found.Path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("read %s: %w", found.Path, err)
	}
	block, markdown, err := SplitFrontMatter(data)
	if err != nil {
		return nil, nil, "", err
	}
	return block, []byte(markdown), ComputeRev(data), nil
}

// Create allocates an id, materializes the project defaults, stamps the
// timestamps and writes the file (docs/03 sections 4.1 and 13.3).
func (s *FileStore) Create(ctx context.Context, draft ItemDraft) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("create item", err)
	}
	if !draft.Type.Valid() || draft.Type == TypeComment {
		return nil, fmt.Errorf("create item: %q is not a creatable item type", draft.Type)
	}
	if strings.TrimSpace(draft.Title) == "" {
		return nil, &DiagnosticError{Diagnostic: Diagnostic{
			Code: CodeTitle, Severity: SeverityError, Field: "title", Message: "missing or empty",
		}}
	}
	dir, ok := ItemDirName(draft.Type)
	if !ok {
		return nil, fmt.Errorf("create item: no folder for type %q", draft.Type)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id := draft.ID
	if id == "" {
		allocated, err := s.alloc.Next(ctx, draft.Type)
		if err != nil {
			return nil, err
		}
		id = allocated
	}

	// Lost-update guard: between the scan the allocator did and this write, a
	// concurrent process or a git pull may have created the same id
	// (docs/07 section 6.5).
	if existing, err := s.find(id); err == nil {
		return nil, fmt.Errorf("create %s: %w: %s", id, ErrDuplicateID, existing.Path)
	} else if !errors.Is(err, ErrItemNotFound) {
		return nil, err
	}

	now := s.now()
	it := &Item{
		ID:          id,
		Type:        draft.Type,
		Title:       strings.TrimSpace(draft.Title),
		Status:      draft.Status,
		Priority:    draft.Priority,
		Parent:      draft.Parent,
		Milestone:   draft.Milestone,
		Sprint:      draft.Sprint,
		Assignees:   draft.Assignees,
		Author:      draft.Author,
		Owner:       draft.Owner,
		Labels:      draft.Labels,
		Estimate:    draft.Estimate,
		Effort:      draft.Effort,
		Created:     now,
		Updated:     now,
		Start:       draft.Start,
		Due:         draft.Due,
		Links:       draft.Links,
		Attachments: draft.Attachments,
		Custom:      draft.Custom,
		Extra:       draft.Extra,
		Body:        draft.Body,
		Path:        path.Join(s.backlog, dir, FileName(id, draft.Title)),
	}
	s.applyDefaults(it)
	if cat := s.cfg.CategoryOf(it.Status); cat == CategoryInProgress {
		it.Started = now
	}
	if err := s.validate(it); err != nil {
		return nil, err
	}
	if err := s.writeItem(it, ""); err != nil {
		return nil, err
	}
	return it, nil
}

// applyDefaults materializes project.yaml:defaults.<type> into a new item. The
// values are written into the file: they are never applied at read time.
func (s *FileStore) applyDefaults(it *Item) {
	d := s.cfg.Defaults[it.Type]
	if it.Status == "" {
		it.Status = d.Status
	}
	if it.Status == "" {
		it.Status = s.cfg.InitialStatus()
	}
	if it.Priority == "" {
		it.Priority = d.Priority
	}
	if len(it.Assignees) == 0 && len(d.Assignees) > 0 {
		it.Assignees = append([]string(nil), d.Assignees...)
	}
	if len(it.Labels) == 0 && len(d.Labels) > 0 {
		it.Labels = append([]string(nil), d.Labels...)
	}
}

// Update applies a sparse patch to an item under an optimistic lock. An empty
// expected rev writes unconditionally, which is what a CLI edit without --rev
// does; every API and MCP caller supplies one (R-REV-3).
func (s *FileStore) Update(ctx context.Context, id ItemID, patch ItemPatch, expected Rev) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("update", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it, err := s.readChecked(id, expected)
	if err != nil {
		return nil, err
	}
	oldPath := it.Path
	if err := applyPatch(it, patch); err != nil {
		return nil, err
	}
	it.Updated = s.now()
	s.retarget(it, oldPath)
	if err := s.validate(it); err != nil {
		return nil, err
	}
	if err := s.writeItem(it, oldPath); err != nil {
		return nil, err
	}
	return it, nil
}

// Delete soft-deletes an item: the file stays, with `deleted: true`, so that the
// id is never reused and a merge cannot resurrect it (R-ID-3).
func (s *FileStore) Delete(ctx context.Context, id ItemID, expected Rev) error {
	return s.DeleteWith(ctx, id, expected, DeleteOptions{})
}

// DeleteWith removes an item, soft by default and hard on request.
func (s *FileStore) DeleteWith(ctx context.Context, id ItemID, expected Rev, opts DeleteOptions) error {
	if err := ctx.Err(); err != nil {
		return wrapContext("delete", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it, err := s.readChecked(id, expected)
	if err != nil {
		return err
	}
	if opts.Hard {
		if err := s.fs.Remove(it.Path); err != nil {
			return fmt.Errorf("delete %s: %w", id, err)
		}
		return nil
	}
	it.Deleted = true
	it.Updated = s.now()
	return s.writeItem(it, "")
}

// Move changes the status of an item, enforcing the declared transitions and
// stamping started/closed as the item enters or leaves a terminal category.
func (s *FileStore) Move(ctx context.Context, id ItemID, status Status, expected Rev) (*Item, error) {
	return s.MoveWith(ctx, id, status, expected, MoveOptions{})
}

// MoveWith is Move with the option to bypass the workflow transitions.
func (s *FileStore) MoveWith(ctx context.Context, id ItemID, status Status, expected Rev, opts MoveOptions) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("move", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it, err := s.readChecked(id, expected)
	if err != nil {
		return nil, err
	}
	if err := s.checkTransition(it, status, opts.Force); err != nil {
		return nil, err
	}
	now := s.now()
	from := it.Status
	it.Status = status
	it.Updated = now
	s.stampTransition(it, from, status, now)
	if err := s.validate(it); err != nil {
		return nil, err
	}
	if err := s.writeItem(it, ""); err != nil {
		return nil, err
	}
	return it, nil
}

// checkTransition applies the workflow rules to a status change. An unknown
// status is always an error; a transition the workflow does not declare is a
// warning in a file (W-WORKFLOW-TRANSITION) that this layer escalates to a
// refusal, unless the caller forces it (docs/03 section 6.1).
func (s *FileStore) checkTransition(it *Item, to Status, force bool) error {
	d := ValidateTransition(s.cfg, it.Status, to)
	switch {
	case d.Code == "":
		return nil
	case d.Severity == SeverityError:
		d.Path = it.Path
		return &DiagnosticError{Diagnostic: d}
	case force:
		return nil
	default:
		return &TransitionError{ID: it.ID, From: it.Status, To: to, Allowed: s.cfg.Workflow.Transitions[it.Status]}
	}
}

// stampTransition maintains started and closed across a status change: started
// is stamped the first time the item enters the in_progress category, closed
// when it enters a terminal one and cleared when it leaves it (docs/03 7.1).
func (s *FileStore) stampTransition(it *Item, from, to Status, now Timestamp) {
	fromCat := s.cfg.CategoryOf(from)
	toCat := s.cfg.CategoryOf(to)
	if toCat == CategoryInProgress && it.Started.IsZero() {
		it.Started = now
	}
	closing := toCat == CategoryDone || toCat == CategoryCancelled
	wasClosed := fromCat == CategoryDone || fromCat == CategoryCancelled
	switch {
	case closing && !wasClosed:
		it.Closed = now
	case !closing && wasClosed:
		it.Closed = Timestamp{}
	}
}

// readChecked locates an item, reads it and enforces the optimistic lock.
func (s *FileStore) readChecked(id ItemID, expected Rev) (*Item, error) {
	found, err := s.locate(id)
	if err != nil {
		return nil, err
	}
	data, err := s.fs.ReadFile(found.Path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", found.Path, err)
	}
	current := ComputeRev(data)
	if expected != "" && expected != current {
		return nil, &StaleRevisionError{ID: id, Path: found.Path, Expected: expected, Current: current}
	}
	return ParseItem(found.Path, data)
}

// retarget recomputes the file name after a title change, keeping the id
// (R-SLUG-2). It is a no-op when renaming is disabled or the slug is unchanged.
func (s *FileStore) retarget(it *Item, oldPath string) {
	if !s.RenameOnTitleChange {
		return
	}
	want := FileName(it.ID, it.Title)
	if path.Base(oldPath) == want {
		return
	}
	it.Path = path.Join(path.Dir(oldPath), want)
}

// validate rejects an invalid item before it reaches the disk. Warnings never
// block a write; only error-severity diagnostics do (JoinDiagnostics).
func (s *FileStore) validate(it *Item) error {
	if !s.Validate {
		return nil
	}
	return JoinDiagnostics(ValidateItem(it, s.cfg))
}

// writeItem serializes an item canonically and writes it atomically, removing
// the file it replaced when the name changed.
func (s *FileStore) writeItem(it *Item, oldPath string) error {
	data, err := SerializeItem(it)
	if err != nil {
		return err
	}
	if err := s.fs.MkdirAll(path.Dir(it.Path)); err != nil {
		return fmt.Errorf("write %s: %w", it.Path, err)
	}
	if err := writeFileAtomic(s.fs, it.Path, data); err != nil {
		return err
	}
	if oldPath != "" && oldPath != it.Path {
		if err := s.fs.Remove(oldPath); err != nil && !errors.Is(err, ErrNotExist) {
			return fmt.Errorf("remove %s: %w", oldPath, err)
		}
	}
	it.Rev = ComputeRev(data)
	return nil
}

// locate finds the file that claims an id. An id nobody claims is looked up
// again through the redirect table, so that a link written before a renumber
// still resolves while the item that kept the id stays reachable (R-RENUM-2).
func (s *FileStore) locate(id ItemID) (scannedItem, error) {
	found, err := s.find(id)
	if !errors.Is(err, ErrItemNotFound) {
		return found, err
	}
	if resolved := s.resolveRedirect(id); resolved != id {
		return s.find(resolved)
	}
	return found, err
}

// find looks an id up by the folder of its type first (one directory listing)
// and falls back to a full scan, so that a misplaced file is still found
// (R-SLUG-1).
func (s *FileStore) find(resolved ItemID) (scannedItem, error) {
	if dir, ok := s.dirForID(resolved); ok {
		entries, err := s.fs.ReadDir(dir)
		if err != nil && !errors.Is(err, ErrNotExist) {
			return scannedItem{}, fmt.Errorf("read dir %s: %w", dir, err)
		}
		var matches []scannedItem
		for _, e := range entries {
			if e.IsDir || !isItemFileName(e.Name) || IDFromFileName(e.Name) != resolved {
				continue
			}
			it, err := scanItemFile(s.fs, path.Join(dir, e.Name))
			if err != nil {
				return scannedItem{}, err
			}
			if it.ID == resolved {
				matches = append(matches, it)
			}
		}
		switch len(matches) {
		case 1:
			return matches[0], nil
		case 0:
			// Fall through to the full scan: the file may sit in another folder
			// or carry a stale name.
		default:
			return scannedItem{}, duplicateError(resolved, matches)
		}
	}

	items, err := scanItems(s.fs, s.backlog)
	if err != nil {
		return scannedItem{}, err
	}
	var matches []scannedItem
	for _, it := range items {
		if it.ID == resolved {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return scannedItem{}, fmt.Errorf("%s: %w", resolved, ErrItemNotFound)
	default:
		return scannedItem{}, duplicateError(resolved, matches)
	}
}

// dirForID returns the folder an id belongs in, derived from its type code.
func (s *FileStore) dirForID(id ItemID) (string, bool) {
	_, code, _, err := ParseItemID(string(id))
	if err != nil {
		return "", false
	}
	t, ok := ItemTypeFor(code)
	if !ok {
		return "", false
	}
	dir, ok := ItemDirName(t)
	if !ok {
		return "", false
	}
	return path.Join(s.backlog, dir), true
}

// resolveRedirect follows id_allocation.redirects, so that a stale link keeps
// working after a renumber (R-RENUM-2). The chain is bounded to keep a cycle in
// a hand-edited project.yaml from hanging the caller.
func (s *FileStore) resolveRedirect(id ItemID) ItemID {
	seen := map[ItemID]bool{id: true}
	for i := 0; i < 8; i++ {
		next, ok := s.cfg.IDAllocation.Redirects[id]
		if !ok || next == "" || seen[next] {
			return id
		}
		id = next
		seen[id] = true
	}
	return id
}

// duplicateError builds the E-ID-DUPLICATE error naming every file that claims
// the id (R-SLUG-4).
func duplicateError(id ItemID, matches []scannedItem) error {
	paths := make([]string, 0, len(matches))
	for _, m := range matches {
		paths = append(paths, m.Path)
	}
	return fmt.Errorf("%w %s claimed by %s", ErrDuplicateID, id, strings.Join(paths, ", "))
}

// applyPatch mutates an item in place. It never touches id, type, created or the
// derived fields.
func applyPatch(it *Item, p ItemPatch) error {
	if p.Title != nil {
		title := strings.TrimSpace(*p.Title)
		if title == "" {
			return &DiagnosticError{Diagnostic: Diagnostic{
				Code: CodeTitle, Severity: SeverityError, Path: it.Path, Field: "title", Message: "missing or empty",
			}}
		}
		it.Title = title
	}
	setIf(&it.Status, p.Status)
	setIf(&it.Priority, p.Priority)
	setIf(&it.Parent, p.Parent)
	setIf(&it.Milestone, p.Milestone)
	setIf(&it.Sprint, p.Sprint)
	setIf(&it.Author, p.Author)
	setIf(&it.Owner, p.Owner)
	setIf(&it.Start, p.Start)
	setIf(&it.Due, p.Due)
	if p.Estimate != nil {
		v := *p.Estimate
		it.Estimate = &v
	}
	if p.Effort != nil {
		v := *p.Effort
		it.Effort = &v
	}
	if p.Spent != nil {
		v := *p.Spent
		it.Spent = &v
	}
	if p.Assignees != nil {
		it.Assignees = append([]string(nil), (*p.Assignees)...)
	}
	it.Assignees = addStrings(it.Assignees, p.AddAssignees)
	it.Assignees = removeStrings(it.Assignees, p.RemoveAssignees)
	if p.Labels != nil {
		it.Labels = append([]string(nil), (*p.Labels)...)
	}
	it.Labels = addStrings(it.Labels, p.AddLabels)
	it.Labels = removeStrings(it.Labels, p.RemoveLabels)
	if p.Links != nil {
		it.Links = append([]Link(nil), (*p.Links)...)
	}
	it.Links = addLinks(it.Links, p.AddLinks)
	it.Links = removeLinks(it.Links, p.RemoveLinks)
	it.Attachments = addStrings(it.Attachments, p.AddAttachments)
	if len(p.Custom) > 0 {
		if it.Custom == nil {
			it.Custom = make(map[string]any, len(p.Custom))
		}
		for k, v := range p.Custom {
			if v == nil {
				delete(it.Custom, k)
				continue
			}
			it.Custom[k] = v
		}
		if len(it.Custom) == 0 {
			it.Custom = nil
		}
	}
	if p.Deleted != nil {
		it.Deleted = *p.Deleted
	}
	if p.Body != nil {
		it.Body = strings.Trim(*p.Body, "\n")
	}
	if p.BodyAppend != "" {
		body := strings.Trim(it.Body, "\n")
		if body != "" {
			body += "\n\n"
		}
		it.Body = body + strings.Trim(p.BodyAppend, "\n")
	}
	for _, field := range p.Unset {
		if err := unsetField(it, field); err != nil {
			return err
		}
	}
	return nil
}

// setIf assigns a pointed-at value when the pointer is not nil.
func setIf[T any](dst, src *T) {
	if src != nil {
		*dst = *src
	}
}

// unsetField clears one front-matter field by name.
func unsetField(it *Item, field string) error {
	switch field {
	case "status":
		it.Status = ""
	case "priority":
		it.Priority = ""
	case "parent":
		it.Parent = ""
	case "epic":
		it.Epic = ""
	case "milestone":
		it.Milestone = ""
	case "sprint":
		it.Sprint = ""
	case "assignees":
		it.Assignees = nil
	case "author":
		it.Author = ""
	case "owner":
		it.Owner = ""
	case "labels":
		it.Labels = nil
	case "estimate":
		it.Estimate = nil
	case "effort":
		it.Effort = nil
	case "spent":
		it.Spent = nil
	case "started":
		it.Started = Timestamp{}
	case "closed":
		it.Closed = Timestamp{}
	case "start":
		it.Start = Date{}
	case "due":
		it.Due = Date{}
	case "links":
		it.Links = nil
	case "attachments":
		it.Attachments = nil
	case "custom":
		it.Custom = nil
	case "deleted":
		it.Deleted = false
	case "body":
		it.Body = ""
	default:
		return fmt.Errorf("unset %q: unknown or immutable field", field)
	}
	return nil
}

// addStrings appends the values that are not already present, preserving order.
func addStrings(list, add []string) []string {
	for _, v := range add {
		v = strings.TrimSpace(v)
		if v == "" || containsFold(list, v) {
			continue
		}
		list = append(list, v)
	}
	return list
}

// removeStrings drops the given values, comparing case-insensitively.
func removeStrings(list, remove []string) []string {
	if len(remove) == 0 || len(list) == 0 {
		return list
	}
	out := list[:0]
	for _, v := range list {
		if containsFold(remove, v) {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// addLinks appends the relations that are not already present.
func addLinks(list, add []Link) []Link {
	for _, l := range add {
		if l.Kind == "" || l.Target == "" || hasLink(list, l) {
			continue
		}
		list = append(list, l)
	}
	return list
}

// removeLinks drops the relations that match kind and target.
func removeLinks(list, remove []Link) []Link {
	if len(remove) == 0 || len(list) == 0 {
		return list
	}
	out := list[:0]
	for _, l := range list {
		if hasLink(remove, l) {
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// hasLink reports whether a relation with the same kind and target is present.
func hasLink(list []Link, l Link) bool {
	for _, e := range list {
		if e.Kind == l.Kind && e.Target == l.Target {
			return true
		}
	}
	return false
}

// ReadPage returns a knowledge-base page. The path is relative to the
// documentation folder and MUST stay inside it.
func (s *FileStore) ReadPage(ctx context.Context, project ProjectKey, p string) (*KBPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("read page", err)
	}
	full, err := s.pagePath(project, p)
	if err != nil {
		return nil, err
	}
	data, err := s.fs.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("read page %s: %w", p, err)
	}
	return s.page(full, path.Clean(p), data), nil
}

// WritePage creates or replaces a knowledge-base page under an optimistic lock.
// An empty expected rev writes unconditionally; a non-empty one must match the
// bytes currently on disk.
func (s *FileStore) WritePage(ctx context.Context, project ProjectKey, p string, content []byte, expected Rev) (*KBPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapContext("write page", err)
	}
	full, err := s.pagePath(project, p)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	switch current, err := s.fs.ReadFile(full); {
	case err == nil:
		if rev := ComputeRev(current); expected != "" && expected != rev {
			return nil, &StaleRevisionError{Path: full, Expected: expected, Current: rev}
		}
	case errors.Is(err, ErrNotExist):
		// Creating a page: nothing to check.
	default:
		return nil, fmt.Errorf("read page %s: %w", p, err)
	}

	data := Canonicalize(content)
	if err := s.fs.MkdirAll(path.Dir(full)); err != nil {
		return nil, fmt.Errorf("write page %s: %w", p, err)
	}
	if err := writeFileAtomic(s.fs, full, data); err != nil {
		return nil, err
	}
	return s.page(full, path.Clean(p), data), nil
}

// pagePath validates a knowledge-base path and returns its vault-relative form.
func (s *FileStore) pagePath(project ProjectKey, p string) (string, error) {
	if project != "" && s.cfg.Key != "" && project != s.cfg.Key {
		return "", fmt.Errorf("page %s: unknown project %q", p, project)
	}
	clean := path.Clean(strings.TrimPrefix(p, "./"))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "/") ||
		clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("page %q: path must stay inside the documentation folder", p)
	}
	if first, _ := splitFirst(clean); first == BacklogDirName {
		return "", fmt.Errorf("page %q: the backlog is not a knowledge-base page", p)
	}
	return path.Join(s.docs, clean), nil
}

// page parses a knowledge-base file into the shared KBPage model and stamps the
// project it belongs to.
func (s *FileStore) page(full, rel string, data []byte) *KBPage {
	page := ParsePage(full, rel, data)
	page.Project = s.cfg.Key
	return page
}

// writeFileAtomic writes data to a sibling temporary file and renames it over
// the target, so that a reader never sees a half-written file and a crash never
// truncates the previous version (docs/06 section 3.1, step 4).
//
// The temporary name has no random part: writes to one vault are serialized by
// the caller's mutex, and a deterministic name is what lets the watcher ignore
// the intermediate file (docs/06 section 9.2).
func writeFileAtomic(fsys FS, p string, data []byte) error {
	tmp := p + ".tmp"
	if err := fsys.WriteFile(tmp, data); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := fsys.Rename(tmp, p); err != nil {
		_ = fsys.Remove(tmp)
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

// wrapContext turns a cancelled or expired context into an error that says
// which operation was abandoned, keeping context.Canceled reachable.
func wrapContext(op string, err error) error {
	return fmt.Errorf("%s: %w", op, err)
}
