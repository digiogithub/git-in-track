package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// version is the build the "version" method answers with when the host injects
// none. Each host sets its own with (*Vault).SetVersion: the WebAssembly module
// passes the value -ldflags wrote into wasm/main_js.go, the CLI passes the one
// it prints in `gintrack version`.
var version = "dev"

// ProtocolVersion is the version of the request envelope the hosts speak. It
// must match CORE_PROTOCOL_VERSION in web/src/core-bridge/protocol.ts.
const ProtocolVersion = 1

// Vault is the whole backend behind the CoreApi contract: one file system, the
// projects discovered in it, the index built over it and one file store per
// project.
//
// It is deliberately free of syscall/js, os and path/filepath so that the same
// implementation serves the browser (over an in-memory file system filled by
// "vault.load") and the companion process (over internal/core/osfs). Every
// method of the contract is therefore exercised by native Go tests, and
// wasm/main_js.go only marshals strings in and out of JavaScript.
//
// A Vault is safe for concurrent use.
type Vault struct {
	mu sync.Mutex
	// base is the file system the vault is mounted on, untracked: writes that
	// come from outside (a host event, a "vault.load") go through it so that
	// they never show up in the next WriteSet.
	base core.FS
	// mem is base when it is an in-memory file system, nil otherwise. It is what
	// tells the two modes apart: only an in-memory vault can be replaced
	// wholesale by "vault.load" or fed file contents by "vault.apply".
	mem       *core.MemFS
	fs        *trackingFS
	projects  []core.ProjectRef
	team      *core.TeamRef
	index     *core.Index
	stores    map[core.ProjectKey]*core.FileStore
	rootLabel string
	version   string
	// docsFolders are the documentation folders the host declares. Discovery
	// probes the vault root and its first-level directories on its own; a
	// folder deeper than that is found only because it is listed here
	// (ADR-018).
	docsFolders []string

	// now supplies build timestamps and the created/updated stamps of writes.
	// It defaults to time.Now and exists so that tests can pin them.
	now func() time.Time
}

// Options configures a Vault.
type Options struct {
	// FS is the file system the vault is mounted on. A nil FS mounts a fresh
	// in-memory one, which is the browser's cold start: "vault.load" fills it.
	FS core.FS
	// Root is the human-readable label of the vault root: a directory handle
	// name in the browser, the vault directory in the companion process.
	Root string
	// DocsFolders are the documentation folders the host declares, vault
	// relative. They are probed at any depth, which is what keeps a monorepo
	// working under the bounded discovery rule (ADR-018).
	DocsFolders []string
	// Version is the build the "version" method reports. Empty means the
	// package default.
	Version string
	// Now supplies build timestamps and the created/updated stamps of writes.
	// Nil means time.Now.
	Now func() time.Time
	// Scan builds the index over FS as part of the call. It is what a
	// disk-backed host wants; the browser leaves it off because its vault is
	// empty until "vault.load" arrives.
	Scan bool
}

// New returns a vault configured by opts. It only fails when opts.Scan is set
// and the initial scan of the file system fails.
func New(opts Options) (*Vault, error) {
	v := newVault(opts)
	if !opts.Scan {
		return v, nil
	}
	if _, err := v.reload(context.Background()); err != nil {
		return nil, err
	}
	return v, nil
}

// newVault mounts a vault without scanning it, which is the only part of
// construction that cannot fail.
func newVault(opts Options) *Vault {
	v := &Vault{now: opts.Now, version: opts.Version, rootLabel: opts.Root, docsFolders: opts.DocsFolders}
	if v.now == nil {
		v.now = time.Now
	}
	if v.version == "" {
		v.version = version
	}
	fsys := opts.FS
	if fsys == nil {
		fsys = core.NewMemFS()
	}
	v.mount(fsys)
	return v
}

// NewInMemory returns a vault over an empty in-memory file system, which is how
// the browser starts: call "vault.load" to fill it.
func NewInMemory() *Vault {
	return newVault(Options{})
}

// Open mounts an existing vault on fsys, discovers its projects and builds the
// index from the files themselves. It is the native entry point: unlike the
// browser, a companion process reads the vault directly instead of having the
// host push file contents in.
func Open(fsys core.FS, rootLabel string) (*Vault, error) {
	return OpenWithDocs(fsys, rootLabel, nil)
}

// OpenWithDocs is Open with the documentation folders the host declares. A
// folder deeper than the bounded discovery rule — the monorepo `apps/api/docs`
// of docs/03 section 3.5 — is found only because it is listed here (ADR-018).
func OpenWithDocs(fsys core.FS, rootLabel string, docsFolders []string) (*Vault, error) {
	if fsys == nil {
		return nil, failf("invalid_request", "open vault: no file system")
	}
	return New(Options{FS: fsys, Root: rootLabel, DocsFolders: docsFolders, Scan: true})
}

// SetClock replaces the clock used for build timestamps and for the created and
// updated fields of new items. It exists for tests and for reproducible fixtures.
func (v *Vault) SetClock(now func() time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if now == nil {
		now = time.Now
	}
	v.now = now
	v.index.Now = now
	for _, s := range v.stores {
		s.Clock = core.ClockFunc(now)
	}
}

// SetVersion sets the build string the "version" method reports. Each host
// injects its own: the browser module the value -ldflags wrote into it, the
// companion process the version of the binary.
func (v *Vault) SetVersion(build string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if build == "" {
		build = version
	}
	v.version = build
}

// Root returns the label of the vault root the host mounted.
func (v *Vault) Root() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.rootLabel
}

// Projects returns the projects discovered in the vault, in discovery order.
// The team knowledge-base scope is not one of them: it is reported by Team.
func (v *Vault) Projects() []core.ProjectRef {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]core.ProjectRef, 0, len(v.projects))
	for _, p := range v.projects {
		if !p.Team {
			out = append(out, p)
		}
	}
	return out
}

// BaseFS returns the file system the vault is mounted on, untracked. It is what
// a caller reads with when the read must not show up in the next WriteSet.
func (v *Vault) BaseFS() core.FS {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.base
}

// Team returns the team repository this vault is, or nil when its root holds no
// team.yaml.
func (v *Vault) Team() *core.TeamRef {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.team
}

// Stats reports the current state of the index.
func (v *Vault) Stats() IndexStats {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.stats()
}

// Reload discovers the projects again and rebuilds the whole index from the
// file system. It is what a watcher calls after a change it cannot describe as
// a list of events: a branch switch, a pull, a directory rename.
func (v *Vault) Reload(ctx context.Context) (IndexStats, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.reload(ctx)
}

// ApplyEvents folds a batch of file events into the index and reports exactly
// what changed, so that a caller can invalidate only the affected queries.
//
// The files are read back through the vault's file system, which is why a
// native watcher only has to say which paths moved, not what they now contain.
// A change to a project.yaml that adds or removes a project forces a full
// rebuild, and the returned delta is then empty: call Stats for the new totals.
func (v *Vault) ApplyEvents(ctx context.Context, events []core.FileEvent) (core.IndexDelta, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delta, _, err := v.applyEvents(ctx, events)
	return delta, err
}

// Error carries the stable error code the hosts switch on, the message and the
// file the failure is about when there is one. The browser reports it as the
// `error` half of the JSON envelope; the companion server maps Code onto an
// RFC 7807 problem type.
type Error struct {
	Code    string
	Message string
	Path    string
	// Current is the revision the file holds now, filled in for a stale
	// revision so that every host can hand a caller the token its retry needs
	// without a second round trip (docs/03 R-REV-3).
	Current string
	// Conflicts are the fields the refused write would still have changed,
	// judged against the current content. Empty for every other failure.
	Conflicts []core.ConflictField
}

// Error implements the error interface.
func (e *Error) Error() string { return e.Message }

// AsError classifies err into the stable code catalog. It reports false only
// for a nil error: anything it does not recognize comes back as "internal", so
// a host can map every failure without repeating the catalog.
func AsError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	var already *Error
	if errors.As(err, &already) {
		return already, true
	}
	out := &Error{Code: "internal", Message: err.Error()}
	classify(err, out)
	return out, true
}

// failf builds an Error with a formatted message.
func failf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Call runs one CoreApi method and returns the JSON envelope, never an error:
// the boundary with JavaScript has one shape, `{"ok":true,"result":…}` or
// `{"ok":false,"error":{"code","message","path"}}`.
func (v *Vault) Call(method, params string) string {
	result, err := v.Dispatch(context.Background(), method, []byte(params))
	if err != nil {
		return failureEnvelope(err)
	}
	return successEnvelope(result)
}

// Dispatch routes one method of the CoreApi contract to its handler and returns
// the typed result, which is what the companion server serves and what Call
// wraps in the JSON envelope the browser expects. Every failure it returns can
// be classified with AsError.
//
// The vault mutex is held for the whole call so that a query never observes a
// half-applied write.
func (v *Vault) Dispatch(ctx context.Context, method string, raw []byte) (any, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	switch method {
	case "ping":
		return map[string]any{"pong": true, "wasm": true}, nil
	case "version":
		return map[string]any{"protocol": ProtocolVersion, "core": v.version}, nil

	case "vault.load":
		return v.vaultLoad(ctx, raw)
	case "vault.apply":
		return v.vaultApply(ctx, raw)
	case "vault.stats":
		return v.stats(), nil
	case "snapshot.export":
		return v.snapshotExport()
	case "snapshot.load":
		return v.snapshotLoad(raw)

	case "project.list":
		return v.projectList(), nil
	case "project.create":
		return v.projectCreate(ctx, raw)

	case "team.get":
		return v.teamGet()
	case "ref.resolve":
		return v.refResolve(raw)

	case "board.list":
		return v.boardList(ctx)
	case "board.get":
		return v.boardGet(ctx, raw)
	case "board.move", "board.update",
		"sprint.list", "sprint.get", "sprint.create", "sprint.update",
		"sprint.start", "sprint.close":
		return nil, failf("invalid_request",
			"%s needs the workspace: the sprint and its items live in different repositories", method)

	case "item.list":
		return v.itemList(ctx, raw)
	case "item.get":
		return v.itemGet(raw)
	case "item.children":
		return v.itemChildren(raw)
	case "item.create":
		return v.itemCreate(ctx, raw)
	case "item.update":
		return v.itemUpdate(ctx, raw)
	case "item.move":
		return v.itemMove(ctx, raw)
	case "item.delete":
		return v.itemDelete(ctx, raw)
	case "item.validate":
		return v.itemValidate(raw)
	case "item.parse":
		return v.itemParse(raw)
	case "item.serialize":
		return v.itemSerialize(raw)

	case "comment.list":
		return v.commentList(raw)
	case "comment.add":
		return v.commentAdd(ctx, raw)

	case "kb.tree":
		return v.kbTree(raw)
	case "kb.page":
		return v.kbPage(raw)
	case "kb.write":
		return v.kbWrite(ctx, raw)

	case "conflict.merge":
		return v.conflictMerge(raw)

	case "search":
		return v.search(raw)
	default:
		return nil, failf("unknown_method", "unknown method %q", method)
	}
}

// ---------------------------------------------------------------- vault ----

// mount points the vault at a file system and drops everything derived from the
// previous one. The caller holds the lock, except in New.
func (v *Vault) mount(fsys core.FS) {
	v.base = fsys
	v.mem, _ = fsys.(*core.MemFS)
	v.fs = newTrackingFS(fsys)
	v.projects = nil
	v.index = core.NewIndex(v.fs, nil)
	if v.now != nil {
		v.index.Now = v.now
	}
	v.stores = map[core.ProjectKey]*core.FileStore{}
}

// rediscover walks the vault again and rebuilds the per-project stores. It
// reports whether the set of projects changed, which is what forces a full
// rebuild instead of an incremental pass.
//
// A team.yaml at the vault root turns the vault into a team repository: its
// `knowledge/` folder joins the scan as a pseudo-project so that the team
// knowledge base is indexed, searched and linked exactly like a project's
// (docs/04 section 4).
func (v *Vault) rediscover() (bool, error) {
	found, err := core.DiscoverProjectsWith(v.fs, core.DiscoveryOptions{DocsFolders: v.docsFolders})
	if err != nil {
		return false, fmt.Errorf("discover projects: %w", err)
	}
	team, hasTeam, err := core.DiscoverTeam(v.fs, ".")
	if err != nil {
		return false, fmt.Errorf("discover team: %w", err)
	}
	scopes := found
	v.team = nil
	if hasTeam {
		v.team = team
		scopes = append(append([]core.ProjectRef{}, found...), team.KBScope())
	}
	changed := !sameProjects(v.projects, scopes)
	v.projects = scopes
	v.stores = make(map[core.ProjectKey]*core.FileStore, len(found))
	for _, p := range found {
		if p.Config == nil || p.Key == "" {
			// project.yaml could not be decoded: the vault opens read-only for
			// that project rather than pretending the folder is not there.
			continue
		}
		store := core.NewStore(v.fs, p.BacklogPath, p.Config)
		if v.now != nil {
			store.Clock = core.ClockFunc(v.now)
		}
		v.stores[p.Key] = store
	}
	return changed, nil
}

// sameProjects reports whether two discovery results describe the same projects
// in the same places.
func sameProjects(a, c []core.ProjectRef) bool {
	if len(a) != len(c) {
		return false
	}
	for i := range a {
		if a[i].Key != c[i].Key || a[i].DocsPath != c[i].DocsPath {
			return false
		}
	}
	return true
}

// rebuild throws the index away and builds it again over the mounted file
// system, keeping the projects discovered last. The caller holds the lock.
func (v *Vault) rebuild(ctx context.Context) (IndexStats, error) {
	v.index = core.NewIndex(v.fs, v.projects)
	v.index.Now = v.now
	if _, err := v.index.Build(ctx, true); err != nil {
		return IndexStats{}, fmt.Errorf("build index: %w", err)
	}
	return v.stats(), nil
}

// reload rediscovers the projects and rebuilds the whole index from the files.
// The caller holds the lock.
func (v *Vault) reload(ctx context.Context) (IndexStats, error) {
	if _, err := v.rediscover(); err != nil {
		return IndexStats{}, err
	}
	return v.rebuild(ctx)
}

// vaultLoad replaces the vault with the files the host pushed and builds the
// index from scratch. It is meaningless for a vault that reads its own files:
// there, a full rescan is Reload.
func (v *Vault) vaultLoad(ctx context.Context, raw []byte) (any, error) {
	if v.mem == nil {
		return nil, failf("invalid_request",
			"this vault reads its files itself: reload it instead of pushing files in")
	}
	p, err := decodeParams[vaultLoadParams](raw)
	if err != nil {
		return nil, err
	}
	mem := core.NewMemFS()
	for _, f := range p.Files {
		if err := mem.WriteFile(f.Path, []byte(f.Text)); err != nil {
			return nil, &Error{Code: "invalid_request", Message: err.Error(), Path: f.Path}
		}
	}
	v.mount(mem)
	v.rootLabel = p.RootLabel
	if len(p.DocsFolders) > 0 {
		v.docsFolders = p.DocsFolders
	}
	stats, err := v.reload(ctx)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// vaultApply mirrors a batch of host file events into the vault and re-indexes
// only what they touched.
func (v *Vault) vaultApply(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[vaultApplyParams](raw)
	if err != nil {
		return nil, err
	}
	events := make([]core.FileEvent, 0, len(p.Events))
	for _, ev := range p.Events {
		kind, err := fileEventKind(ev.Op)
		if err != nil {
			return nil, err
		}
		if err := v.applyToVault(kind, ev); err != nil {
			return nil, err
		}
		events = append(events, core.FileEvent{Kind: kind, Path: ev.Path, OldPath: ev.From})
	}
	delta, rebuilt, err := v.applyEvents(ctx, events)
	if err != nil {
		return nil, err
	}
	stats := v.stats()
	if !rebuilt {
		stats.Delta = &delta
	}
	return stats, nil
}

// applyEvents re-indexes what a batch of file events touched. It reports the
// delta and whether the batch forced a full rebuild, which leaves the delta
// empty. The caller holds the lock.
func (v *Vault) applyEvents(ctx context.Context, events []core.FileEvent) (core.IndexDelta, bool, error) {
	configTouched := false
	for _, ev := range events {
		if isConfigFile(ev.Path) || isConfigFile(ev.OldPath) {
			configTouched = true
			break
		}
	}
	if configTouched {
		changed, err := v.rediscover()
		if err != nil {
			return core.IndexDelta{}, false, err
		}
		if changed {
			// A project appeared or vanished: the layout classification of every
			// file may have changed, so an incremental pass cannot be trusted.
			if _, err := v.rebuild(ctx); err != nil {
				return core.IndexDelta{}, true, err
			}
			return core.IndexDelta{}, true, nil
		}
	}
	delta, err := v.index.ApplyFileEvents(ctx, events)
	if err != nil {
		return core.IndexDelta{}, false, fmt.Errorf("apply file events: %w", err)
	}
	return delta, false, nil
}

// applyToVault mirrors one host event into the in-memory file system. It goes
// straight to the MemFS, bypassing the write log: these bytes come from the
// host, which has already persisted them, so they must not show up in the next
// WriteSet.
//
// A vault that reads its own files has nothing to mirror. There the event only
// names the path that changed, and the bytes are read back from the file system
// itself, which is why a native watcher never has to carry file contents.
func (v *Vault) applyToVault(kind core.FileEventKind, ev fileEventParams) error {
	if v.mem == nil {
		return nil
	}
	switch kind {
	case core.FileCreated, core.FileModified:
		if err := v.mem.WriteFile(ev.Path, []byte(ev.Text)); err != nil {
			return &Error{Code: "invalid_request", Message: err.Error(), Path: ev.Path}
		}
	case core.FileRemoved:
		if err := v.mem.Remove(ev.Path); err != nil && !errors.Is(err, core.ErrNotExist) {
			return &Error{Code: "internal", Message: err.Error(), Path: ev.Path}
		}
	case core.FileRenamed:
		if ev.From != "" {
			if err := v.mem.Rename(ev.From, ev.Path); err != nil && !errors.Is(err, core.ErrNotExist) {
				return &Error{Code: "internal", Message: err.Error(), Path: ev.Path}
			}
		}
		if ev.Text != "" {
			if err := v.mem.WriteFile(ev.Path, []byte(ev.Text)); err != nil {
				return &Error{Code: "invalid_request", Message: err.Error(), Path: ev.Path}
			}
		}
	}
	return nil
}

// fileEventKind maps the contract's `op` onto the core event kind.
func fileEventKind(op string) (core.FileEventKind, error) {
	switch op {
	case "create":
		return core.FileCreated, nil
	case "write":
		return core.FileModified, nil
	case "remove":
		return core.FileRemoved, nil
	case "rename":
		return core.FileRenamed, nil
	default:
		return "", failf("invalid_request", "unknown file event op %q", op)
	}
}

// isConfigFile reports whether a path is a project.yaml inside a backlog or the
// team.yaml at the vault root: either one changes what the vault holds, so it
// forces a rediscovery instead of an incremental pass.
func isConfigFile(p string) bool {
	if p == "" {
		return false
	}
	clean := path.Clean(p)
	if clean == core.TeamFileName {
		return true
	}
	return path.Base(clean) == core.ProjectFileName &&
		path.Base(path.Dir(clean)) == core.BacklogDirName
}

// stats projects the current index state onto the contract's IndexStats.
func (v *Vault) stats() IndexStats {
	return newIndexStats(v.index.Stats(), v.index.Fingerprint(), v.index.Warnings())
}

// snapshotExport serializes the index for the IndexedDB cache.
func (v *Vault) snapshotExport() (any, error) {
	snap := v.index.Snapshot()
	data, err := core.EncodeSnapshot(snap)
	if err != nil {
		return nil, fmt.Errorf("encode snapshot: %w", err)
	}
	return snapshotBlob{Fingerprint: snap.Fingerprint, JSON: string(data)}, nil
}

// snapshotLoad hydrates the index from a cached snapshot, without any files. The
// result answers structural queries immediately; bodies and search come back
// once vault.load pushes the real files.
func (v *Vault) snapshotLoad(raw []byte) (any, error) {
	p, err := decodeParams[snapshotBlob](raw)
	if err != nil {
		return nil, err
	}
	snap, err := core.DecodeSnapshot([]byte(p.JSON))
	if err != nil {
		return nil, failf("invalid_request", "decode snapshot: %v", err)
	}
	if err := v.index.Load(snap); err != nil {
		return nil, failf("invalid_request", "load snapshot: %v", err)
	}
	v.projects = v.index.Projects()
	return v.stats(), nil
}

// -------------------------------------------------------------- projects ----

// projectList summarizes every discovered project.
func (v *Vault) projectList() []projectSummary {
	counts := v.index.ProjectCounts()
	out := make([]projectSummary, 0, len(v.projects))
	for _, p := range v.projects {
		if p.Team {
			// The team knowledge base is a scope of the index, not a project:
			// it has no backlog, no workflow and no id allocation.
			continue
		}
		_, writable := v.stores[p.Key]
		summary := projectSummary{
			Key:         string(p.Key),
			Name:        p.Name,
			DocsPath:    p.DocsPath,
			Statuses:    []statusSummary{},
			Labels:      []labelSummary{},
			Priorities:  defaultPriorities(),
			ItemCounts:  itemCounts(counts[p.Key]),
			BacklogPath: p.BacklogPath,
			Writable:    writable,
			Diagnostics: p.Diagnostics,
		}
		if p.Config != nil {
			for _, s := range p.Config.Workflow.Statuses {
				name := s.Name
				if name == "" {
					name = string(s.ID)
				}
				summary.Statuses = append(summary.Statuses, statusSummary{
					ID: string(s.ID), Name: name, Category: string(s.Category),
					Terminal: s.Terminal, WIP: s.WIP,
				})
			}
			for _, l := range p.Config.Labels {
				summary.Labels = append(summary.Labels, labelSummary{
					Name: l.Name, Color: l.Color, Description: l.Description,
				})
			}
			if len(p.Config.Priorities) > 0 {
				summary.Priorities = p.Config.Priorities
			}
			if summary.Name == "" {
				summary.Name = p.Config.Name
			}
			summary.Workflow = workflowOf(p.Config.Workflow)
			summary.Estimation = &estimationSummary{
				Scale:      p.Config.Estimation.Scale,
				Values:     p.Config.Estimation.Values,
				TrackHours: p.Config.Estimation.TrackHours,
			}
			summary.CustomFields = customFieldsOf(p.Config.CustomFields)
		}
		out = append(out, summary)
	}
	return out
}

// workflowOf exposes the initial status and the transition map as plain strings.
func workflowOf(w core.Workflow) *workflowSummary {
	out := &workflowSummary{Initial: string(w.Initial)}
	if len(w.Transitions) > 0 {
		out.Transitions = make(map[string][]string, len(w.Transitions))
		for from, tos := range w.Transitions {
			targets := make([]string, 0, len(tos))
			for _, to := range tos {
				targets = append(targets, string(to))
			}
			out.Transitions[string(from)] = targets
		}
	}
	return out
}

// customFieldsOf converts the declared custom fields to their wire form.
func customFieldsOf(fields []core.CustomField) []customFieldSummary {
	if len(fields) == 0 {
		return nil
	}
	out := make([]customFieldSummary, 0, len(fields))
	for _, f := range fields {
		applies := make([]string, 0, len(f.AppliesTo))
		for _, t := range f.AppliesTo {
			applies = append(applies, string(t))
		}
		out = append(out, customFieldSummary{
			Key: f.Key, Type: f.Type, Values: f.Values, Items: f.Items,
			AppliesTo: applies, Default: f.Default, Description: f.Description,
		})
	}
	return out
}

// defaultPriorities is the catalog a project.yaml that declares none falls back
// to; renaming these is not allowed (docs/03 section 6).
func defaultPriorities() []core.Priority {
	return []core.Priority{core.PriorityCritical, core.PriorityHigh, core.PriorityMedium, core.PriorityLow}
}

// itemCounts fills in a zero for every item type, so the UI never has to guard
// against a missing key.
func itemCounts(counts map[core.ItemType]int) map[core.ItemType]int {
	out := make(map[core.ItemType]int, len(core.ItemTypes()))
	for _, t := range core.ItemTypes() {
		out[t] = counts[t]
	}
	return out
}

// ----------------------------------------------------------------- items ----

// itemList answers a filtered, sorted, paginated query.
func (v *Vault) itemList(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[itemFilterParams](raw)
	if err != nil {
		return nil, err
	}
	filter, err := v.filterOf(p)
	if err != nil {
		return nil, err
	}
	page, err := v.index.Items(ctx, filter)
	if err != nil {
		return nil, failf("invalid_request", "%v", err)
	}
	items := page.Items
	if items == nil {
		items = []core.Item{}
	}
	if !wantsBody(p.Fields) {
		// Bodies are the bulk of a large vault; a list call drops them unless the
		// caller explicitly asked for them (see api.ts, Item.body).
		for i := range items {
			items[i].Body = ""
		}
	}
	return itemPage{Items: items, NextCursor: page.NextCursor, Total: page.Total}, nil
}

// wantsBody reports whether a projection asks for the Markdown body.
func wantsBody(fields []string) bool {
	for _, f := range fields {
		if strings.EqualFold(f, "body") {
			return true
		}
	}
	return false
}

// filterOf maps the contract's ItemFilter onto core.Filter. `category` has no
// core counterpart: it is expanded into the statuses of the matching category,
// which is what makes a cross-project board possible.
func (v *Vault) filterOf(p itemFilterParams) (core.Filter, error) {
	f := core.Filter{
		Assignees:      nonEmpty(p.Assignee),
		Labels:         p.Label,
		Parent:         core.ItemID(p.Parent),
		Milestone:      core.ItemID(p.Milestone),
		Text:           p.Text,
		IncludeDeleted: p.IncludeDeleted,
		Sort:           sortSpec(p.Sort, p.Order),
		Limit:          p.Limit,
		Cursor:         p.Cursor,
		Fields:         p.Fields,
	}
	if p.Project != "" {
		f.Projects = []core.ProjectKey{core.ProjectKey(p.Project)}
	}
	for _, t := range p.Type {
		f.Types = append(f.Types, core.ItemType(t))
	}
	for _, s := range p.Status {
		f.Statuses = append(f.Statuses, core.Status(s))
	}
	for _, pr := range p.Priority {
		f.Priorities = append(f.Priorities, core.Priority(pr))
	}
	f.Statuses = append(f.Statuses, v.statusesOfCategories(p.Category, p.Project)...)
	if p.UpdatedSince != "" {
		ts, err := core.ParseUpdatedSince(p.UpdatedSince, v.now())
		if err != nil {
			return core.Filter{}, failf("invalid_request", "updatedSince: %v", err)
		}
		f.UpdatedSince = ts
	}
	return f, nil
}

// statusesOfCategories expands coarse status categories into the concrete
// statuses the matching projects declare.
func (v *Vault) statusesOfCategories(categories []string, project string) []core.Status {
	if len(categories) == 0 {
		return nil
	}
	wanted := make(map[core.StatusCategory]bool, len(categories))
	for _, c := range categories {
		wanted[core.StatusCategory(c)] = true
	}
	seen := map[core.Status]bool{}
	var out []core.Status
	for _, p := range v.projects {
		if p.Config == nil || (project != "" && string(p.Key) != project) {
			continue
		}
		for _, s := range p.Config.Workflow.Statuses {
			if wanted[s.Category] && !seen[s.ID] {
				seen[s.ID] = true
				out = append(out, s.ID)
			}
		}
	}
	return out
}

// sortSpec turns the contract's (sort, order) pair into the core sort string.
func sortSpec(field, order string) string {
	if field == "" {
		return ""
	}
	if strings.EqualFold(order, "desc") {
		return "-" + field
	}
	return field
}

// nonEmpty wraps a value in a slice, or returns nil for the empty string.
func nonEmpty(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}

// itemGet returns one item with its body.
func (v *Vault) itemGet(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID string `json:"id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	it, err := v.index.Item(core.ItemID(p.ID))
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", p.ID, err)
	}
	return it, nil
}

// itemChildren returns the direct children of an item.
func (v *Vault) itemChildren(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID string `json:"id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	kids := v.index.Children(core.ItemID(p.ID))
	if kids == nil {
		kids = []core.Item{}
	}
	return kids, nil
}

// itemCreate allocates an id, writes the file and reports the WriteSet.
func (v *Vault) itemCreate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[itemDraftParams](raw)
	if err != nil {
		return nil, err
	}
	store, err := v.storeFor(core.ProjectKey(p.Project))
	if err != nil {
		return nil, err
	}
	v.fs.begin()
	it, err := store.Create(ctx, p.draft())
	if err != nil {
		return nil, fmt.Errorf("create item: %w", err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"item": it, "writes": writes}, nil
}

// itemUpdate applies a sparse patch under an optimistic lock.
func (v *Vault) itemUpdate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID    string          `json:"id"`
		Patch itemPatchParams `json:"patch"`
		Rev   string          `json:"rev"`
	}](raw)
	if err != nil {
		return nil, err
	}
	store, err := v.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	v.fs.begin()
	it, err := store.Update(ctx, core.ItemID(p.ID), p.Patch.patch(), core.Rev(p.Rev))
	if err != nil {
		return nil, fmt.Errorf("update %s: %w", p.ID, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"item": it, "writes": writes}, nil
}

// itemMove changes the status of an item, honoring the declared workflow.
func (v *Vault) itemMove(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Rev    string `json:"rev"`
	}](raw)
	if err != nil {
		return nil, err
	}
	store, err := v.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	v.fs.begin()
	it, err := store.Move(ctx, core.ItemID(p.ID), core.Status(p.Status), core.Rev(p.Rev))
	if err != nil {
		return nil, fmt.Errorf("move %s: %w", p.ID, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"item": it, "writes": writes}, nil
}

// itemDelete soft-deletes an item, or removes the file when hard is set.
func (v *Vault) itemDelete(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID   string `json:"id"`
		Rev  string `json:"rev"`
		Hard bool   `json:"hard,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	store, err := v.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	v.fs.begin()
	opts := core.DeleteOptions{Hard: p.Hard}
	if err := store.DeleteWith(ctx, core.ItemID(p.ID), core.Rev(p.Rev), opts); err != nil {
		return nil, fmt.Errorf("delete %s: %w", p.ID, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"writes": writes}, nil
}

// itemValidate validates an indexed item, or a candidate file the editor holds
// but has not written yet.
func (v *Vault) itemValidate(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID   string `json:"id,omitempty"`
		Text string `json:"text,omitempty"`
		Path string `json:"path,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	var item *core.Item
	switch {
	case p.Text != "":
		filePath := p.Path
		if filePath == "" {
			filePath = "untitled.md"
		}
		parsed, err := core.ParseItem(filePath, []byte(p.Text))
		if err != nil {
			return diagnosticsOf(err, filePath), nil
		}
		item = parsed
	case p.ID != "":
		found, err := v.index.Item(core.ItemID(p.ID))
		if err != nil {
			return nil, fmt.Errorf("validate %s: %w", p.ID, err)
		}
		item = found
	default:
		return nil, failf("invalid_request", "item.validate needs an id or a text")
	}
	cfg := v.configFor(item)
	diags := core.ValidateItem(item, cfg)
	if diags == nil {
		diags = []core.Diagnostic{}
	}
	return diags, nil
}

// diagnosticsOf turns a parse failure into the one-element diagnostic list the
// editor renders in its gutter.
func diagnosticsOf(err error, filePath string) []core.Diagnostic {
	var pe *core.ParseError
	if errors.As(err, &pe) {
		return []core.Diagnostic{pe.Diagnostic()}
	}
	return []core.Diagnostic{{
		Code: core.CodeFMMissing, Severity: core.SeverityError,
		Path: filePath, Message: err.Error(),
	}}
}

// itemParse parses one file without indexing it.
func (v *Vault) itemParse(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}](raw)
	if err != nil {
		return nil, err
	}
	it, err := core.ParseItem(p.Path, []byte(p.Text))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", p.Path, err)
	}
	return it, nil
}

// itemSerialize renders an item back to canonical Markdown.
func (v *Vault) itemSerialize(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Item core.Item `json:"item"`
	}](raw)
	if err != nil {
		return nil, err
	}
	data, err := core.SerializeItem(&p.Item)
	if err != nil {
		return nil, fmt.Errorf("serialize %s: %w", p.Item.ID, err)
	}
	return map[string]string{"text": string(data)}, nil
}

// conflictMerge merges the three versions of one conflicted file, applying the
// caller's resolution when there is one (GIT-US-0022, docs/06 section 5).
//
// It needs no mounted vault: it is a pure function of the three blobs, which is
// what lets browser-only mode resolve a conflict with the same rules as the
// companion instead of a second implementation in TypeScript.
func (v *Vault) conflictMerge(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Path       string           `json:"path"`
		Base       string           `json:"base"`
		Ours       string           `json:"ours"`
		Theirs     string           `json:"theirs"`
		Resolution *core.Resolution `json:"resolution,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	if p.Path == "" {
		return nil, failf("invalid_request", "conflict.merge needs the path of the conflicted file")
	}
	res, mergeErr := core.MergeFiles(p.Path, core.MergeInput{
		Base: p.Base, Ours: p.Ours, Theirs: p.Theirs,
	}, p.Resolution)
	if mergeErr != nil {
		return nil, failf("invalid_request", "merge %s: %v", p.Path, mergeErr)
	}
	return res, nil
}

// -------------------------------------------------------------- comments ----

// commentList returns the thread of an item, oldest first.
func (v *Vault) commentList(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID string `json:"id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	list := v.index.Comments(core.ItemID(p.ID))
	if list == nil {
		list = []core.Comment{}
	}
	return list, nil
}

// commentAdd appends one comment file to an item's thread.
func (v *Vault) commentAdd(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID        string `json:"id"`
		Author    string `json:"author"`
		Body      string `json:"body"`
		InReplyTo string `json:"inReplyTo,omitempty"`
		Rev       string `json:"rev,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	store, err := v.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	v.fs.begin()
	rev := p.Rev
	if rev == "*" {
		// The wildcard of If-Match: an explicit, deliberate write against
		// whatever the file holds now.
		rev = ""
	}
	draft := core.CommentDraft{
		Author: p.Author, Body: p.Body, InReplyTo: p.InReplyTo, ItemRev: core.Rev(rev),
	}
	comment, err := store.AddComment(ctx, core.ItemID(p.ID), draft)
	if err != nil {
		return nil, fmt.Errorf("add comment to %s: %w", p.ID, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"comment": comment, "writes": writes}, nil
}

// -------------------------------------------------------- knowledge base ----

// kbTree returns the knowledge base as a forest of folders and pages.
func (v *Vault) kbTree(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Project string `json:"project,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	root := v.index.KbTree()
	if root == nil {
		return []kbNode{}, nil
	}
	node := root
	if p.Project != "" {
		if found := findNode(root, v.docsPathOf(core.ProjectKey(p.Project))); found != nil {
			node = found
		}
	}
	out := make([]kbNode, 0, len(node.Children))
	for _, child := range node.Children {
		out = append(out, treeNodeOf(child))
	}
	return out, nil
}

// docsPathOf returns the documentation folder of a project, or the empty string.
func (v *Vault) docsPathOf(key core.ProjectKey) string {
	for _, p := range v.projects {
		if p.Key == key {
			return p.DocsPath
		}
	}
	return ""
}

// findNode walks the tree looking for a folder by path.
func findNode(n *core.TreeNode, target string) *core.TreeNode {
	if n == nil || target == "" {
		return nil
	}
	if n.Path == target {
		return n
	}
	for _, child := range n.Children {
		if found := findNode(child, target); found != nil {
			return found
		}
	}
	return nil
}

// treeNodeOf converts one core tree node into the contract's KbNode.
func treeNodeOf(n *core.TreeNode) kbNode {
	out := kbNode{Path: n.Path, Name: n.Name, Kind: "page", Title: n.Title}
	if n.IsDir {
		out.Kind = "dir"
	}
	for _, child := range n.Children {
		out.Children = append(out.Children, treeNodeOf(child))
	}
	return out
}

// kbPage returns one page with its body and its link neighborhood.
func (v *Vault) kbPage(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Path string `json:"path"`
	}](raw)
	if err != nil {
		return nil, err
	}
	page, ok := v.index.Page(p.Path)
	if !ok {
		return nil, &Error{
			Code: "not_found", Message: fmt.Sprintf("page %s is not indexed", p.Path), Path: p.Path,
		}
	}
	return v.pageResult(page), nil
}

// pageResult projects a KBPage onto the contract's KbPage.
func (v *Vault) pageResult(page *core.KBPage) kbPageResult {
	node := core.PageNode(page.Path)
	graph := v.index.LinkGraph()
	out := kbPageResult{
		Path:        page.Path,
		Title:       page.Title,
		FrontMatter: page.FrontMatter,
		Body:        page.Body,
		Rev:         string(page.Rev),
		Outgoing:    []string{},
		Backlinks:   []string{},
		Project:     string(page.Project),
		RelPath:     page.RelPath,
	}
	if out.FrontMatter == nil {
		out.FrontMatter = map[string]any{}
	}
	if graph == nil {
		return out
	}
	out.Outgoing = referenceTargets(graph.References(node), false)
	out.Backlinks = referenceTargets(graph.Backlinks(node), true)
	return out
}

// referenceTargets renders the other end of a set of wikilink edges: the item id
// or the page path, deduplicated and sorted.
func referenceTargets(refs []core.Reference, incoming bool) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		value := r.Target
		switch {
		case incoming:
			value = r.From.Value()
		case r.Resolved:
			value = r.To.Value()
		}
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// kbWrite creates or replaces a knowledge-base page under an optimistic lock.
func (v *Vault) kbWrite(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Path string `json:"path"`
		Text string `json:"text"`
		Rev  string `json:"rev,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	ref, store, err := v.projectOfPath(p.Path)
	if err != nil {
		return nil, err
	}
	rel := relativeToDocs(ref.DocsPath, p.Path)
	v.fs.begin()
	page, err := store.WritePage(ctx, ref.Key, rel, []byte(p.Text), core.Rev(p.Rev))
	if err != nil {
		return nil, fmt.Errorf("write page %s: %w", p.Path, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	indexed, ok := v.index.Page(page.Path)
	if !ok {
		indexed = page
	} else {
		indexed.Body = page.Body
	}
	return map[string]any{"page": v.pageResult(indexed), "writes": writes}, nil
}

// relativeToDocs turns a vault-relative page path into the documentation-folder
// relative form the store addresses pages by.
func relativeToDocs(docsPath, p string) string {
	clean := path.Clean(p)
	if docsPath == "" || docsPath == "." {
		return clean
	}
	if trimmed := strings.TrimPrefix(clean, docsPath+"/"); trimmed != clean {
		return trimmed
	}
	return clean
}

// ---------------------------------------------------------------- search ----

// search runs the ranked substring search over items and pages.
func (v *Vault) search(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Q       string `json:"q"`
		Limit   int    `json:"limit,omitempty"`
		Project string `json:"project,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	limit := p.Limit
	if p.Project != "" && limit > 0 {
		// Filtering after ranking would shrink the page below the requested size,
		// so ask for more and cut afterwards.
		limit *= 4
	}
	hits := v.index.Search(p.Q, limit)
	out := make([]searchHit, 0, len(hits))
	for _, h := range hits {
		if p.Project != "" && string(h.Project) != p.Project {
			continue
		}
		out = append(out, searchHit{
			Kind: h.Kind, ID: string(h.ID), Path: h.Path, Title: h.Title,
			Snippet: h.Snippet, Score: h.Score, Project: string(h.Project),
		})
		if p.Limit > 0 && len(out) >= p.Limit {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- writes ----

// commit reports what the last mutating call wrote and folds it back into the
// index, so that the next query already sees the change.
func (v *Vault) commit(ctx context.Context) (WriteSet, error) {
	written, removed := v.fs.take()
	out := WriteSet{Written: make([]File, 0, len(written)), Removed: removed}
	if out.Removed == nil {
		out.Removed = []string{}
	}
	events := make([]core.FileEvent, 0, len(written)+len(removed))
	for _, p := range written {
		data, err := v.base.ReadFile(p)
		if err != nil {
			return WriteSet{}, fmt.Errorf("read back %s: %w", p, err)
		}
		out.Written = append(out.Written, File{Path: p, Text: string(data)})
		events = append(events, core.FileEvent{Kind: core.FileModified, Path: p})
	}
	for _, p := range removed {
		events = append(events, core.FileEvent{Kind: core.FileRemoved, Path: p})
	}
	if _, err := v.index.ApplyFileEvents(ctx, events); err != nil {
		return WriteSet{}, fmt.Errorf("reindex written files: %w", err)
	}
	return out, nil
}

// storeFor returns the writable store of a project. An empty key is allowed when
// the vault holds exactly one project, which is the browser-only common case.
func (v *Vault) storeFor(key core.ProjectKey) (*core.FileStore, error) {
	if key == "" {
		if len(v.stores) != 1 {
			return nil, failf("invalid_request", "this vault holds %d projects: name one", len(v.stores))
		}
		for _, s := range v.stores {
			return s, nil
		}
	}
	store, ok := v.stores[key]
	if !ok {
		return nil, failf("not_found", "project %q is not open for writing", key)
	}
	return store, nil
}

// storeForItem returns the store that owns an item, taken from the project key
// embedded in its id.
func (v *Vault) storeForItem(id core.ItemID) (*core.FileStore, error) {
	key, _, _, err := core.ParseItemID(string(id))
	if err != nil {
		return v.storeFor("")
	}
	return v.storeFor(key)
}

// projectOfPath returns the project whose documentation folder holds a path, the
// longest match winning so that nested projects resolve to the inner one.
func (v *Vault) projectOfPath(p string) (core.ProjectRef, *core.FileStore, error) {
	clean := path.Clean(p)
	best := -1
	for i, ref := range v.projects {
		if ref.DocsPath == "." || strings.HasPrefix(clean, ref.DocsPath+"/") {
			if best < 0 || len(ref.DocsPath) > len(v.projects[best].DocsPath) {
				best = i
			}
		}
	}
	if best < 0 {
		return core.ProjectRef{}, nil, &Error{
			Code: "not_found", Message: fmt.Sprintf("no project owns %s", p), Path: p,
		}
	}
	ref := v.projects[best]
	store, err := v.storeFor(ref.Key)
	if err != nil {
		return core.ProjectRef{}, nil, err
	}
	return ref, store, nil
}

// configFor returns the project configuration an item is validated against, or
// nil when the vault has none for it.
func (v *Vault) configFor(it *core.Item) *core.ProjectConfig {
	key, _, _, err := core.ParseItemID(string(it.ID))
	if err == nil {
		for _, p := range v.projects {
			if p.Key == key {
				return p.Config
			}
		}
	}
	if len(v.projects) == 1 {
		return v.projects[0].Config
	}
	return nil
}

// -------------------------------------------------------------- envelope ----

// decodeParams decodes the params of one request. An absent or null payload
// decodes to the zero value, which is what a method without params sends.
func decodeParams[T any](raw []byte) (T, error) {
	var v T
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return v, nil
	}
	if err := json.Unmarshal(trimmed, &v); err != nil {
		return v, failf("invalid_request", "decode params: %v", err)
	}
	return v, nil
}

// successEnvelope wraps a payload in the success envelope.
func successEnvelope(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return failureEnvelope(failf("internal", "encode result: %v", err))
	}
	var env strings.Builder
	env.Grow(len(data) + 16)
	env.WriteString(`{"ok":true,"result":`)
	env.Write(data)
	env.WriteString(`}`)
	return env.String()
}

// failureEnvelope classifies an error and wraps it in the failure envelope.
func failureEnvelope(err error) string {
	payload := errorPayload(err)
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return `{"ok":false,"error":{"code":"internal","message":"encode error"}}`
	}
	var env strings.Builder
	env.Grow(len(data) + 16)
	env.WriteString(`{"ok":false,"error":`)
	env.Write(data)
	env.WriteString(`}`)
	return env.String()
}

// errorPayload renders an error as the `error` half of the JSON envelope.
func errorPayload(err error) map[string]any {
	e, ok := AsError(err)
	if !ok {
		return map[string]any{"code": "internal", "message": "unknown error"}
	}
	out := map[string]any{"code": e.Code, "message": e.Message}
	if e.Path != "" {
		out["path"] = e.Path
	}
	// A rejected conditional write carries what a retry needs: the revision the
	// file holds now, and where the two versions disagree.
	if e.Current != "" {
		out["currentRev"] = e.Current
	}
	if len(e.Conflicts) > 0 {
		out["conflicts"] = e.Conflicts
	}
	return out
}

// classify fills in the stable code, and the file the failure is about, for an
// error the core reported. Anything it does not recognize keeps the "internal"
// code the caller set.
func classify(err error, out *Error) {
	var stale *core.StaleRevisionError
	var transition *core.TransitionError
	var parse *core.ParseError
	var diag *core.DiagnosticError
	switch {
	case errors.As(err, &stale):
		out.Code = core.StaleRevisionCode
		out.Path = stale.Path
		out.Current = string(stale.Current)
		out.Conflicts = stale.Fields
	case errors.As(err, &transition):
		out.Code = core.TransitionDeniedCode
	case errors.As(err, &parse):
		out.Code = "invalid_front_matter"
		out.Path = parse.Path
	case errors.As(err, &diag):
		out.Code = "validation_failed"
		out.Path = diag.Diagnostic.Path
	case errors.Is(err, core.ErrItemNotFound), errors.Is(err, core.ErrNotExist):
		out.Code = "not_found"
	case errors.Is(err, core.ErrDuplicateID):
		out.Code = "duplicate_id"
	case errors.Is(err, core.ErrReadOnly):
		out.Code = "read_only"
	case errors.Is(err, core.ErrRevMismatch):
		out.Code = core.StaleRevisionCode
	case errors.Is(err, core.ErrInvalidFrontMatter):
		out.Code = "invalid_front_matter"
	}
}
