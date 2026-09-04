package main

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

// version is set with -ldflags by the release build, exactly as it is for the
// CLI binary. It lives in this file, and not in the build-tagged entry point,
// because the "version" method answers with it and is covered by native tests.
var version = "dev"

// protocolVersion is the version of the request envelope the worker speaks. It
// must match CORE_PROTOCOL_VERSION in web/src/core-bridge/protocol.ts.
const protocolVersion = 1

// Bridge is the whole browser-only backend: one in-memory vault, the projects
// discovered in it, the index built over it and one file store per project.
//
// It is deliberately free of syscall/js so that every method of the CoreApi
// contract is exercised by native Go tests; wasm/main_js.go only marshals
// strings in and out of JavaScript.
//
// A Bridge is safe for concurrent use, although the worker drives it from a
// single thread.
type Bridge struct {
	mu        sync.Mutex
	mem       *core.MemFS
	fs        *trackingFS
	projects  []core.ProjectRef
	index     *core.Index
	stores    map[core.ProjectKey]*core.FileStore
	rootLabel string

	// now supplies build timestamps and the created/updated stamps of writes.
	// It defaults to time.Now and exists so that tests can pin them.
	now func() time.Time
}

// NewBridge returns a bridge over an empty vault. Call "vault.load" to fill it.
func NewBridge() *Bridge {
	b := &Bridge{now: time.Now}
	b.mount(core.NewMemFS())
	return b
}

// SetClock replaces the clock used for build timestamps and for the created and
// updated fields of new items. It exists for tests and for reproducible fixtures.
func (b *Bridge) SetClock(now func() time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if now == nil {
		now = time.Now
	}
	b.now = now
	b.index.Now = now
	for _, s := range b.stores {
		s.Clock = core.ClockFunc(now)
	}
}

// bridgeError carries the stable error code the UI switches on, and the file the
// failure is about when there is one.
type bridgeError struct {
	Code    string
	Message string
	Path    string
}

// Error implements the error interface.
func (e *bridgeError) Error() string { return e.Message }

// failf builds a bridgeError with a formatted message.
func failf(code, format string, args ...any) *bridgeError {
	return &bridgeError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Call runs one CoreApi method and returns the JSON envelope, never an error:
// the boundary with JavaScript has one shape, `{"ok":true,"result":…}` or
// `{"ok":false,"error":{"code","message","path"}}`.
func (b *Bridge) Call(method, params string) string {
	result, err := b.dispatch(method, []byte(params))
	if err != nil {
		return failureEnvelope(err)
	}
	return successEnvelope(result)
}

// dispatch routes one method to its handler. The vault mutex is held for the
// whole call so that a query never observes a half-applied write.
func (b *Bridge) dispatch(method string, raw []byte) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx := context.Background()

	switch method {
	case "ping":
		return map[string]any{"pong": true, "wasm": true}, nil
	case "version":
		return map[string]any{"protocol": protocolVersion, "core": version}, nil

	case "vault.load":
		return b.vaultLoad(ctx, raw)
	case "vault.apply":
		return b.vaultApply(ctx, raw)
	case "vault.stats":
		return b.stats(), nil
	case "snapshot.export":
		return b.snapshotExport()
	case "snapshot.load":
		return b.snapshotLoad(raw)

	case "project.list":
		return b.projectList(), nil

	case "item.list":
		return b.itemList(ctx, raw)
	case "item.get":
		return b.itemGet(raw)
	case "item.children":
		return b.itemChildren(raw)
	case "item.create":
		return b.itemCreate(ctx, raw)
	case "item.update":
		return b.itemUpdate(ctx, raw)
	case "item.move":
		return b.itemMove(ctx, raw)
	case "item.delete":
		return b.itemDelete(ctx, raw)
	case "item.validate":
		return b.itemValidate(raw)
	case "item.parse":
		return b.itemParse(raw)
	case "item.serialize":
		return b.itemSerialize(raw)

	case "comment.list":
		return b.commentList(raw)
	case "comment.add":
		return b.commentAdd(ctx, raw)

	case "kb.tree":
		return b.kbTree(raw)
	case "kb.page":
		return b.kbPage(raw)
	case "kb.write":
		return b.kbWrite(ctx, raw)

	case "search":
		return b.search(raw)
	default:
		return nil, failf("unknown_method", "unknown method %q", method)
	}
}

// ---------------------------------------------------------------- vault ----

// mount replaces the in-memory vault, rediscovers its projects and rebuilds the
// index over them. The caller holds the lock, except in NewBridge.
func (b *Bridge) mount(mem *core.MemFS) {
	b.mem = mem
	b.fs = newTrackingFS(mem)
	b.projects = nil
	b.index = core.NewIndex(b.fs, nil)
	if b.now != nil {
		b.index.Now = b.now
	}
	b.stores = map[core.ProjectKey]*core.FileStore{}
}

// rediscover walks the vault again and rebuilds the per-project stores. It
// reports whether the set of projects changed, which is what forces a full
// rebuild instead of an incremental pass.
func (b *Bridge) rediscover() (bool, error) {
	found, err := core.DiscoverProjects(b.fs, ".")
	if err != nil {
		return false, fmt.Errorf("discover projects: %w", err)
	}
	changed := !sameProjects(b.projects, found)
	b.projects = found
	b.stores = make(map[core.ProjectKey]*core.FileStore, len(found))
	for _, p := range found {
		if p.Config == nil || p.Key == "" {
			// project.yaml could not be decoded: the vault opens read-only for
			// that project rather than pretending the folder is not there.
			continue
		}
		store := core.NewStore(b.fs, p.BacklogPath, p.Config)
		if b.now != nil {
			store.Clock = core.ClockFunc(b.now)
		}
		b.stores[p.Key] = store
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

// vaultLoad replaces the vault with the files the host pushed and builds the
// index from scratch.
func (b *Bridge) vaultLoad(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[vaultLoadParams](raw)
	if err != nil {
		return nil, err
	}
	mem := core.NewMemFS()
	for _, f := range p.Files {
		if err := mem.WriteFile(f.Path, []byte(f.Text)); err != nil {
			return nil, &bridgeError{Code: "invalid_request", Message: err.Error(), Path: f.Path}
		}
	}
	b.mount(mem)
	b.rootLabel = p.RootLabel
	if _, err := b.rediscover(); err != nil {
		return nil, err
	}
	b.index = core.NewIndex(b.fs, b.projects)
	b.index.Now = b.now
	if _, err := b.index.Build(ctx, true); err != nil {
		return nil, fmt.Errorf("build index: %w", err)
	}
	return b.stats(), nil
}

// vaultApply mirrors a batch of host file events into the vault and re-indexes
// only what they touched.
func (b *Bridge) vaultApply(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[vaultApplyParams](raw)
	if err != nil {
		return nil, err
	}
	events := make([]core.FileEvent, 0, len(p.Events))
	configTouched := false
	for _, ev := range p.Events {
		kind, err := fileEventKind(ev.Op)
		if err != nil {
			return nil, err
		}
		if err := b.applyToVault(kind, ev); err != nil {
			return nil, err
		}
		if isProjectFile(ev.Path) || isProjectFile(ev.From) {
			configTouched = true
		}
		events = append(events, core.FileEvent{Kind: kind, Path: ev.Path, OldPath: ev.From})
	}

	if configTouched {
		changed, err := b.rediscover()
		if err != nil {
			return nil, err
		}
		if changed {
			// A project appeared or vanished: the layout classification of every
			// file may have changed, so an incremental pass cannot be trusted.
			b.index = core.NewIndex(b.fs, b.projects)
			b.index.Now = b.now
			if _, err := b.index.Build(ctx, true); err != nil {
				return nil, fmt.Errorf("build index: %w", err)
			}
			return b.stats(), nil
		}
	}

	delta, err := b.index.ApplyFileEvents(ctx, events)
	if err != nil {
		return nil, fmt.Errorf("apply file events: %w", err)
	}
	stats := b.stats()
	stats.Delta = &delta
	return stats, nil
}

// applyToVault writes one host event into the in-memory file system. It goes
// straight to the MemFS: these bytes come from the host, which has already
// persisted them, so they must not show up in the next WriteSet.
func (b *Bridge) applyToVault(kind core.FileEventKind, ev fileEventParams) error {
	switch kind {
	case core.FileCreated, core.FileModified:
		if err := b.mem.WriteFile(ev.Path, []byte(ev.Text)); err != nil {
			return &bridgeError{Code: "invalid_request", Message: err.Error(), Path: ev.Path}
		}
	case core.FileRemoved:
		if err := b.mem.Remove(ev.Path); err != nil && !errors.Is(err, core.ErrNotExist) {
			return &bridgeError{Code: "internal", Message: err.Error(), Path: ev.Path}
		}
	case core.FileRenamed:
		if ev.From != "" {
			if err := b.mem.Rename(ev.From, ev.Path); err != nil && !errors.Is(err, core.ErrNotExist) {
				return &bridgeError{Code: "internal", Message: err.Error(), Path: ev.Path}
			}
		}
		if ev.Text != "" {
			if err := b.mem.WriteFile(ev.Path, []byte(ev.Text)); err != nil {
				return &bridgeError{Code: "invalid_request", Message: err.Error(), Path: ev.Path}
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

// isProjectFile reports whether a path is a project.yaml inside a backlog.
func isProjectFile(p string) bool {
	if p == "" {
		return false
	}
	clean := path.Clean(p)
	return path.Base(clean) == core.ProjectFileName &&
		path.Base(path.Dir(clean)) == core.BacklogDirName
}

// stats projects the current index state onto the contract's IndexStats.
func (b *Bridge) stats() indexStats {
	return newIndexStats(b.index.Stats(), b.index.Fingerprint(), b.index.Warnings())
}

// snapshotExport serializes the index for the IndexedDB cache.
func (b *Bridge) snapshotExport() (any, error) {
	snap := b.index.Snapshot()
	data, err := core.EncodeSnapshot(snap)
	if err != nil {
		return nil, fmt.Errorf("encode snapshot: %w", err)
	}
	return snapshotBlob{Fingerprint: snap.Fingerprint, JSON: string(data)}, nil
}

// snapshotLoad hydrates the index from a cached snapshot, without any files. The
// result answers structural queries immediately; bodies and search come back
// once vault.load pushes the real files.
func (b *Bridge) snapshotLoad(raw []byte) (any, error) {
	p, err := decodeParams[snapshotBlob](raw)
	if err != nil {
		return nil, err
	}
	snap, err := core.DecodeSnapshot([]byte(p.JSON))
	if err != nil {
		return nil, failf("invalid_request", "decode snapshot: %v", err)
	}
	if err := b.index.Load(snap); err != nil {
		return nil, failf("invalid_request", "load snapshot: %v", err)
	}
	b.projects = b.index.Projects()
	return b.stats(), nil
}

// -------------------------------------------------------------- projects ----

// projectList summarizes every discovered project.
func (b *Bridge) projectList() []projectSummary {
	counts := b.index.ProjectCounts()
	out := make([]projectSummary, 0, len(b.projects))
	for _, p := range b.projects {
		_, writable := b.stores[p.Key]
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
func (b *Bridge) itemList(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[itemFilterParams](raw)
	if err != nil {
		return nil, err
	}
	filter, err := b.filterOf(p)
	if err != nil {
		return nil, err
	}
	page, err := b.index.Items(ctx, filter)
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
func (b *Bridge) filterOf(p itemFilterParams) (core.Filter, error) {
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
	f.Statuses = append(f.Statuses, b.statusesOfCategories(p.Category, p.Project)...)
	if p.UpdatedSince != "" {
		ts, err := core.ParseUpdatedSince(p.UpdatedSince, b.now())
		if err != nil {
			return core.Filter{}, failf("invalid_request", "updatedSince: %v", err)
		}
		f.UpdatedSince = ts
	}
	return f, nil
}

// statusesOfCategories expands coarse status categories into the concrete
// statuses the matching projects declare.
func (b *Bridge) statusesOfCategories(categories []string, project string) []core.Status {
	if len(categories) == 0 {
		return nil
	}
	wanted := make(map[core.StatusCategory]bool, len(categories))
	for _, c := range categories {
		wanted[core.StatusCategory(c)] = true
	}
	seen := map[core.Status]bool{}
	var out []core.Status
	for _, p := range b.projects {
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
func (b *Bridge) itemGet(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID string `json:"id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	it, err := b.index.Item(core.ItemID(p.ID))
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", p.ID, err)
	}
	return it, nil
}

// itemChildren returns the direct children of an item.
func (b *Bridge) itemChildren(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID string `json:"id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	kids := b.index.Children(core.ItemID(p.ID))
	if kids == nil {
		kids = []core.Item{}
	}
	return kids, nil
}

// itemCreate allocates an id, writes the file and reports the WriteSet.
func (b *Bridge) itemCreate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[itemDraftParams](raw)
	if err != nil {
		return nil, err
	}
	store, err := b.storeFor(core.ProjectKey(p.Project))
	if err != nil {
		return nil, err
	}
	b.fs.begin()
	it, err := store.Create(ctx, p.draft())
	if err != nil {
		return nil, fmt.Errorf("create item: %w", err)
	}
	writes, err := b.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"item": it, "writes": writes}, nil
}

// itemUpdate applies a sparse patch under an optimistic lock.
func (b *Bridge) itemUpdate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID    string          `json:"id"`
		Patch itemPatchParams `json:"patch"`
		Rev   string          `json:"rev"`
	}](raw)
	if err != nil {
		return nil, err
	}
	store, err := b.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	b.fs.begin()
	it, err := store.Update(ctx, core.ItemID(p.ID), p.Patch.patch(), core.Rev(p.Rev))
	if err != nil {
		return nil, fmt.Errorf("update %s: %w", p.ID, err)
	}
	writes, err := b.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"item": it, "writes": writes}, nil
}

// itemMove changes the status of an item, honoring the declared workflow.
func (b *Bridge) itemMove(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Rev    string `json:"rev"`
	}](raw)
	if err != nil {
		return nil, err
	}
	store, err := b.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	b.fs.begin()
	it, err := store.Move(ctx, core.ItemID(p.ID), core.Status(p.Status), core.Rev(p.Rev))
	if err != nil {
		return nil, fmt.Errorf("move %s: %w", p.ID, err)
	}
	writes, err := b.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"item": it, "writes": writes}, nil
}

// itemDelete soft-deletes an item, or removes the file when hard is set.
func (b *Bridge) itemDelete(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID   string `json:"id"`
		Rev  string `json:"rev"`
		Hard bool   `json:"hard,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	store, err := b.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	b.fs.begin()
	opts := core.DeleteOptions{Hard: p.Hard}
	if err := store.DeleteWith(ctx, core.ItemID(p.ID), core.Rev(p.Rev), opts); err != nil {
		return nil, fmt.Errorf("delete %s: %w", p.ID, err)
	}
	writes, err := b.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"writes": writes}, nil
}

// itemValidate validates an indexed item, or a candidate file the editor holds
// but has not written yet.
func (b *Bridge) itemValidate(raw []byte) (any, error) {
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
		found, err := b.index.Item(core.ItemID(p.ID))
		if err != nil {
			return nil, fmt.Errorf("validate %s: %w", p.ID, err)
		}
		item = found
	default:
		return nil, failf("invalid_request", "item.validate needs an id or a text")
	}
	cfg := b.configFor(item)
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
func (b *Bridge) itemParse(raw []byte) (any, error) {
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
func (b *Bridge) itemSerialize(raw []byte) (any, error) {
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

// -------------------------------------------------------------- comments ----

// commentList returns the thread of an item, oldest first.
func (b *Bridge) commentList(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID string `json:"id"`
	}](raw)
	if err != nil {
		return nil, err
	}
	list := b.index.Comments(core.ItemID(p.ID))
	if list == nil {
		list = []core.Comment{}
	}
	return list, nil
}

// commentAdd appends one comment file to an item's thread.
func (b *Bridge) commentAdd(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID        string `json:"id"`
		Author    string `json:"author"`
		Body      string `json:"body"`
		InReplyTo string `json:"inReplyTo,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	store, err := b.storeForItem(core.ItemID(p.ID))
	if err != nil {
		return nil, err
	}
	b.fs.begin()
	draft := core.CommentDraft{Author: p.Author, Body: p.Body, InReplyTo: p.InReplyTo}
	comment, err := store.AddComment(ctx, core.ItemID(p.ID), draft)
	if err != nil {
		return nil, fmt.Errorf("add comment to %s: %w", p.ID, err)
	}
	writes, err := b.commit(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"comment": comment, "writes": writes}, nil
}

// -------------------------------------------------------- knowledge base ----

// kbTree returns the knowledge base as a forest of folders and pages.
func (b *Bridge) kbTree(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Project string `json:"project,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	root := b.index.KbTree()
	if root == nil {
		return []kbNode{}, nil
	}
	node := root
	if p.Project != "" {
		if found := findNode(root, b.docsPathOf(core.ProjectKey(p.Project))); found != nil {
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
func (b *Bridge) docsPathOf(key core.ProjectKey) string {
	for _, p := range b.projects {
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
func (b *Bridge) kbPage(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Path string `json:"path"`
	}](raw)
	if err != nil {
		return nil, err
	}
	page, ok := b.index.Page(p.Path)
	if !ok {
		return nil, &bridgeError{
			Code: "not_found", Message: fmt.Sprintf("page %s is not indexed", p.Path), Path: p.Path,
		}
	}
	return b.pageResult(page), nil
}

// pageResult projects a KBPage onto the contract's KbPage.
func (b *Bridge) pageResult(page *core.KBPage) kbPageResult {
	node := core.PageNode(page.Path)
	graph := b.index.LinkGraph()
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
func (b *Bridge) kbWrite(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Path string `json:"path"`
		Text string `json:"text"`
		Rev  string `json:"rev,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	ref, store, err := b.projectOfPath(p.Path)
	if err != nil {
		return nil, err
	}
	rel := relativeToDocs(ref.DocsPath, p.Path)
	b.fs.begin()
	page, err := store.WritePage(ctx, ref.Key, rel, []byte(p.Text), core.Rev(p.Rev))
	if err != nil {
		return nil, fmt.Errorf("write page %s: %w", p.Path, err)
	}
	writes, err := b.commit(ctx)
	if err != nil {
		return nil, err
	}
	indexed, ok := b.index.Page(page.Path)
	if !ok {
		indexed = page
	} else {
		indexed.Body = page.Body
	}
	return map[string]any{"page": b.pageResult(indexed), "writes": writes}, nil
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
func (b *Bridge) search(raw []byte) (any, error) {
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
	hits := b.index.Search(p.Q, limit)
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
func (b *Bridge) commit(ctx context.Context) (writeSet, error) {
	written, removed := b.fs.take()
	out := writeSet{Written: make([]vaultFile, 0, len(written)), Removed: removed}
	if out.Removed == nil {
		out.Removed = []string{}
	}
	events := make([]core.FileEvent, 0, len(written)+len(removed))
	for _, p := range written {
		data, err := b.mem.ReadFile(p)
		if err != nil {
			return writeSet{}, fmt.Errorf("read back %s: %w", p, err)
		}
		out.Written = append(out.Written, vaultFile{Path: p, Text: string(data)})
		events = append(events, core.FileEvent{Kind: core.FileModified, Path: p})
	}
	for _, p := range removed {
		events = append(events, core.FileEvent{Kind: core.FileRemoved, Path: p})
	}
	if _, err := b.index.ApplyFileEvents(ctx, events); err != nil {
		return writeSet{}, fmt.Errorf("reindex written files: %w", err)
	}
	return out, nil
}

// storeFor returns the writable store of a project. An empty key is allowed when
// the vault holds exactly one project, which is the browser-only common case.
func (b *Bridge) storeFor(key core.ProjectKey) (*core.FileStore, error) {
	if key == "" {
		if len(b.stores) != 1 {
			return nil, failf("invalid_request", "this vault holds %d projects: name one", len(b.stores))
		}
		for _, s := range b.stores {
			return s, nil
		}
	}
	store, ok := b.stores[key]
	if !ok {
		return nil, failf("not_found", "project %q is not open for writing", key)
	}
	return store, nil
}

// storeForItem returns the store that owns an item, taken from the project key
// embedded in its id.
func (b *Bridge) storeForItem(id core.ItemID) (*core.FileStore, error) {
	key, _, _, err := core.ParseItemID(string(id))
	if err != nil {
		return b.storeFor("")
	}
	return b.storeFor(key)
}

// projectOfPath returns the project whose documentation folder holds a path, the
// longest match winning so that nested projects resolve to the inner one.
func (b *Bridge) projectOfPath(p string) (core.ProjectRef, *core.FileStore, error) {
	clean := path.Clean(p)
	best := -1
	for i, ref := range b.projects {
		if ref.DocsPath == "." || strings.HasPrefix(clean, ref.DocsPath+"/") {
			if best < 0 || len(ref.DocsPath) > len(b.projects[best].DocsPath) {
				best = i
			}
		}
	}
	if best < 0 {
		return core.ProjectRef{}, nil, &bridgeError{
			Code: "not_found", Message: fmt.Sprintf("no project owns %s", p), Path: p,
		}
	}
	ref := b.projects[best]
	store, err := b.storeFor(ref.Key)
	if err != nil {
		return core.ProjectRef{}, nil, err
	}
	return ref, store, nil
}

// configFor returns the project configuration an item is validated against, or
// nil when the vault has none for it.
func (b *Bridge) configFor(it *core.Item) *core.ProjectConfig {
	key, _, _, err := core.ParseItemID(string(it.ID))
	if err == nil {
		for _, p := range b.projects {
			if p.Key == key {
				return p.Config
			}
		}
	}
	if len(b.projects) == 1 {
		return b.projects[0].Config
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
	var b strings.Builder
	b.Grow(len(data) + 16)
	b.WriteString(`{"ok":true,"result":`)
	b.Write(data)
	b.WriteString(`}`)
	return b.String()
}

// failureEnvelope classifies an error and wraps it in the failure envelope.
func failureEnvelope(err error) string {
	payload := errorPayload(err)
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return `{"ok":false,"error":{"code":"internal","message":"encode error"}}`
	}
	var b strings.Builder
	b.Grow(len(data) + 16)
	b.WriteString(`{"ok":false,"error":`)
	b.Write(data)
	b.WriteString(`}`)
	return b.String()
}

// errorPayload maps a Go error onto the stable code catalog the UI switches on.
func errorPayload(err error) map[string]string {
	out := map[string]string{"code": "internal", "message": err.Error()}

	var be *bridgeError
	if errors.As(err, &be) {
		out["code"] = be.Code
		if be.Path != "" {
			out["path"] = be.Path
		}
		return out
	}
	var stale *core.StaleRevisionError
	var transition *core.TransitionError
	var parse *core.ParseError
	var diag *core.DiagnosticError
	switch {
	case errors.As(err, &stale):
		out["code"] = core.StaleRevisionCode
		out["path"] = stale.Path
	case errors.As(err, &transition):
		out["code"] = core.TransitionDeniedCode
	case errors.As(err, &parse):
		out["code"] = "invalid_front_matter"
		out["path"] = parse.Path
	case errors.As(err, &diag):
		out["code"] = "validation_failed"
		out["path"] = diag.Diagnostic.Path
	case errors.Is(err, core.ErrItemNotFound), errors.Is(err, core.ErrNotExist):
		out["code"] = "not_found"
	case errors.Is(err, core.ErrDuplicateID):
		out["code"] = "duplicate_id"
	case errors.Is(err, core.ErrReadOnly):
		out["code"] = "read_only"
	case errors.Is(err, core.ErrRevMismatch):
		out["code"] = core.StaleRevisionCode
	case errors.Is(err, core.ErrInvalidFrontMatter):
		out["code"] = "invalid_front_matter"
	}
	if out["path"] == "" {
		delete(out, "path")
	}
	return out
}
