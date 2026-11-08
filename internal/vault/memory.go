package vault

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the memory surface of the CoreApi (ADR-044, GIT-US-0211):
// memory.put, memory.get, memory.list and memory.changes. It knows nothing of
// any wire protocol; the hub's alchemai.memory.v2 server and its MCP tools are
// thin layers over it.
//
// A memory is upserted by key within a scope, deduplicated by the content
// hash the server recomputes, and versioned by a counter kept in the file so
// that a changes cursor survives an index rebuild. A batch writes every file
// first and commits once, so it is one commit per repository.

// MemoryContentKind is the kind fed to the content hash for a fact, the only
// kind this layer writes (alchemai.memory.v2 calls it "fact").
const MemoryContentKind = "fact"

// The per-item outcomes of memory.put.
const (
	MemoryCreated   = "created"
	MemoryUpdated   = "updated"
	MemoryDuplicate = "duplicate"
	MemoryForgotten = "forgotten"
	MemoryRejected  = "rejected"
)

// MemoryWrite is one entry of a memory.put batch.
type MemoryWrite struct {
	// Op is "upsert" (the default) or "forget".
	Op string `json:"op,omitempty"`
	// Key is the upsert key, unique in the scope.
	Key string `json:"key"`
	// Title is the fact in one line; empty derives it from the content.
	Title string `json:"title,omitempty"`
	// Content is the body of the memory.
	Content string   `json:"content,omitempty"`
	Labels  []string `json:"labels,omitempty"`
	// Status is "proposed" or "active". Empty makes a new memory proposed and
	// leaves an existing one where it is.
	Status string `json:"status,omitempty"`
	// Author is the handle recorded as the author.
	Author string             `json:"author,omitempty"`
	Source *core.MemorySource `json:"source,omitempty"`
	// Supersedes names the memories, by key or id, this one replaces.
	Supersedes []string `json:"supersedes,omitempty"`
	// BaseVersion and Rev are optimistic locks on an existing memory: the
	// version or the rev the caller read. Either one that no longer matches
	// rejects the entry with stale_revision.
	BaseVersion int64  `json:"baseVersion,omitempty"`
	Rev         string `json:"rev,omitempty"`
}

// MemoryWriteResult is the outcome of one entry.
type MemoryWriteResult struct {
	Index   int         `json:"index"`
	Outcome string      `json:"outcome"`
	ID      core.ItemID `json:"id,omitempty"`
	Version int64       `json:"version,omitempty"`
	Rev     core.Rev    `json:"rev,omitempty"`
	Code    string      `json:"code,omitempty"`
	Message string      `json:"message,omitempty"`
	// Current is the version that refused a stale write.
	Current int64 `json:"current,omitempty"`
}

// MemoryPutResult answers memory.put.
type MemoryPutResult struct {
	Scope   string              `json:"scope"`
	Results []MemoryWriteResult `json:"results"`
	Writes  WriteSet            `json:"writes"`
}

// MemoryTombstone is a memory that left the visible set: forgotten. A
// rejected proposal was never visible and has none.
type MemoryTombstone struct {
	ID        core.ItemID    `json:"id"`
	Key       string         `json:"key"`
	LocalID   string         `json:"localId"`
	Version   int64          `json:"version"`
	DeletedAt core.Timestamp `json:"deletedAt"`
}

// MemoryChanges answers memory.changes.
type MemoryChanges struct {
	Scope      string            `json:"scope"`
	Records    []core.Item       `json:"records"`
	Tombstones []MemoryTombstone `json:"tombstones"`
	// Cursor is the highest version this page covers; pass it back as since.
	Cursor  int64 `json:"cursor"`
	HasMore bool  `json:"hasMore"`
}

// memoryScope is one scope's store and identity.
type memoryScope struct {
	key   core.ProjectKey
	scope string
	store *core.FileStore
}

// memoryScopeFor resolves a project key, or the team key for organization
// memory, to its store. The caller holds the lock.
func (v *Vault) memoryScopeFor(key core.ProjectKey) (memoryScope, error) {
	for _, ref := range v.projects {
		if key != "" && ref.Key != key {
			continue
		}
		if ref.Team {
			if v.teamMemory == nil {
				return memoryScope{}, failf("invalid_request",
					"team %s cannot hold organization memory: its key does not satisfy [A-Z][A-Z0-9]{1,9}", ref.Key)
			}
			return memoryScope{key: ref.Key, scope: core.MemoryScope(ref), store: v.teamMemory}, nil
		}
		store, ok := v.stores[ref.Key]
		if !ok {
			return memoryScope{}, failf("read_only", "project %s cannot be written: its project.yaml does not load", ref.Key)
		}
		return memoryScope{key: ref.Key, scope: core.MemoryScope(ref), store: store}, nil
	}
	if key == "" {
		return memoryScope{}, failf("invalid_request", "name the project or team the memory belongs to")
	}
	return memoryScope{}, failf("not_found", "project %q is not open in this repository", key)
}

// memoryPut applies a batch of writes to one scope, then commits once.
func (v *Vault) memoryPut(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Project string        `json:"project"`
		Items   []MemoryWrite `json:"items"`
	}](raw)
	if err != nil {
		return nil, err
	}
	sc, err := v.memoryScopeFor(core.ProjectKey(p.Project))
	if err != nil {
		return nil, err
	}
	b := &memoryBatch{v: v, sc: sc, now: core.NewTimestamp(v.now())}
	b.load()

	v.fs.begin()
	out := MemoryPutResult{Scope: sc.scope, Results: make([]MemoryWriteResult, 0, len(p.Items))}
	for i, w := range p.Items {
		res := b.apply(ctx, w)
		res.Index = i
		out.Results = append(out.Results, res)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	out.Writes = writes
	return out, nil
}

// memoryBatch is the state one memory.put carries from entry to entry: the
// scope's memories as the batch changes them, and the next version.
type memoryBatch struct {
	v     *Vault
	sc    memoryScope
	now   core.Timestamp
	byKey map[string]*core.Item
	byID  map[core.ItemID]*core.Item
	next  int64
}

func (b *memoryBatch) load() {
	all := b.v.index.Memories(b.sc.key)
	b.byKey = make(map[string]*core.Item, len(all))
	b.byID = make(map[core.ItemID]*core.Item, len(all))
	for _, it := range all {
		b.remember(it)
	}
	b.next = core.NextMemoryVersion(all)
}

func (b *memoryBatch) remember(it *core.Item) {
	if it.Memory != nil {
		b.byKey[it.Memory.Key] = it
	}
	b.byID[it.ID] = it
}

// version hands out the next version of the scope.
func (b *memoryBatch) version() int64 {
	v := b.next
	b.next++
	return v
}

// apply runs one entry and never fails the batch: a bad entry is rejected on
// its own, the way the protocol answers per item.
func (b *memoryBatch) apply(ctx context.Context, w MemoryWrite) MemoryWriteResult {
	w.Key = strings.TrimSpace(w.Key)
	if !core.ValidMemoryKey(w.Key) {
		return rejected("invalid_request", fmt.Sprintf("key %q does not match the memory key grammar", w.Key))
	}
	switch w.Op {
	case "", "upsert":
		return b.upsert(ctx, w)
	case "forget":
		return b.forget(ctx, w)
	default:
		return rejected("invalid_request", fmt.Sprintf("unknown op %q: use upsert or forget", w.Op))
	}
}

func rejected(code, msg string) MemoryWriteResult {
	return MemoryWriteResult{Outcome: MemoryRejected, Code: code, Message: msg}
}

// stale checks the optimistic locks of an entry against the live memory.
func stale(w MemoryWrite, current *core.Item) (MemoryWriteResult, bool) {
	if w.BaseVersion > 0 && current.Memory != nil && w.BaseVersion != current.Memory.Version {
		res := rejected(core.StaleRevisionCode, fmt.Sprintf("%s is at version %d, not %d", current.ID, current.Memory.Version, w.BaseVersion))
		res.Current, res.ID = current.Memory.Version, current.ID
		return res, true
	}
	if w.Rev != "" && core.Rev(w.Rev) != current.Rev {
		res := rejected(core.StaleRevisionCode, fmt.Sprintf("%s changed since rev %s", current.ID, w.Rev))
		res.ID, res.Rev = current.ID, current.Rev
		if current.Memory != nil {
			res.Current = current.Memory.Version
		}
		return res, true
	}
	return MemoryWriteResult{}, false
}

func (b *memoryBatch) upsert(ctx context.Context, w MemoryWrite) MemoryWriteResult {
	status := core.Status(strings.TrimSpace(w.Status))
	if status != "" && status != core.MemoryProposed && status != core.MemoryActive {
		return rejected("invalid_request", fmt.Sprintf("a write may ask for proposed or active, not %q", status))
	}
	hash := core.MemoryContentHash(MemoryContentKind, b.sc.scope, w.Key, "", w.Content)
	title := memoryTitle(w)
	supersedes, res, ok := b.resolveSupersedes(w)
	if !ok {
		return res
	}

	current := b.byKey[w.Key]
	if current == nil {
		if status == "" {
			status = core.MemoryProposed
		}
		links := make([]core.Link, 0, len(supersedes))
		for _, old := range supersedes {
			links = append(links, core.Link{Kind: core.LinkSupersedes, Target: string(old.ID)})
		}
		it, err := b.sc.store.Create(ctx, core.ItemDraft{
			Type: core.TypeMemory, Title: title, Status: status, Labels: w.Labels, Author: w.Author,
			Body: w.Content, Links: links,
			Memory: &core.ItemMemory{
				Key: w.Key, Version: b.version(), ContentHash: hash,
				LocalID: b.sc.scope + "/" + w.Key, ValidFrom: b.now, Source: w.Source,
			},
		})
		if err != nil {
			return fromError(err)
		}
		b.remember(it)
		if res, ok := b.supersede(ctx, it, supersedes); !ok {
			return res
		}
		return written(MemoryCreated, it)
	}

	if res, isStale := stale(w, current); isStale {
		return res
	}
	target := current.Status
	switch {
	case status != "":
		target = status
	case current.Status == core.MemoryCancelled:
		// Writing a rejected proposal again proposes it again.
		target = core.MemoryProposed
	case current.Status == core.MemoryForgotten || current.Status == core.MemorySuperseded:
		return rejected(core.TransitionDeniedCode,
			fmt.Sprintf("%s is %s; write it with status active to restore it", current.ID, current.Status))
	}
	sameContent := current.Memory != nil && current.Memory.ContentHash == hash
	if sameContent && target == current.Status && len(supersedes) == 0 &&
		(w.Title == "" || w.Title == current.Title) && (w.Labels == nil || sameLabels(w.Labels, current.Labels)) {
		return written(MemoryDuplicate, current)
	}

	block := current.Memory.Clone()
	if block == nil {
		block = &core.ItemMemory{Key: w.Key}
	}
	block.Version, block.ContentHash = b.version(), hash
	if block.LocalID == "" {
		block.LocalID = b.sc.scope + "/" + w.Key
	}
	if w.Source != nil {
		block.Source = w.Source
	}
	if target == core.MemoryActive && current.Status == core.MemoryForgotten {
		block.ValidTo = core.Timestamp{}
	}
	patch := core.ItemPatch{Memory: block, Title: &title}
	if !sameContent {
		patch.Body = &w.Content
	}
	if w.Labels != nil {
		labels := append([]string(nil), w.Labels...)
		patch.Labels = &labels
	}
	if target != current.Status {
		patch.Status = &target
	}
	for _, old := range supersedes {
		patch.AddLinks = append(patch.AddLinks, core.Link{Kind: core.LinkSupersedes, Target: string(old.ID)})
	}
	it, _, err := b.sc.store.UpdateReport(ctx, current.ID, patch, current.Rev)
	if err != nil {
		return fromError(err)
	}
	b.remember(it)
	if res, ok := b.supersede(ctx, it, supersedes); !ok {
		return res
	}
	return written(MemoryUpdated, it)
}

// resolveSupersedes finds the active memories an entry replaces, by key or id.
func (b *memoryBatch) resolveSupersedes(w MemoryWrite) ([]*core.Item, MemoryWriteResult, bool) {
	var out []*core.Item
	for _, ref := range w.Supersedes {
		ref = strings.TrimSpace(ref)
		old := b.byKey[ref]
		if old == nil {
			old = b.byID[core.ItemID(ref)]
		}
		switch {
		case old == nil:
			return nil, rejected("not_found", fmt.Sprintf("no memory %q to supersede in %s", ref, b.sc.scope)), false
		case old.Memory != nil && old.Memory.Key == w.Key:
			return nil, rejected("invalid_request", "a memory cannot supersede itself"), false
		case old.Status != core.MemoryActive:
			return nil, rejected(core.TransitionDeniedCode,
				fmt.Sprintf("%s is %s; only an active memory can be superseded", old.ID, old.Status)), false
		}
		out = append(out, old)
	}
	return out, MemoryWriteResult{}, true
}

// supersede moves each replaced memory to superseded and mirrors the newer id
// into its block, in the same batch and therefore the same commit.
func (b *memoryBatch) supersede(ctx context.Context, newer *core.Item, olds []*core.Item) (MemoryWriteResult, bool) {
	for _, old := range olds {
		block := old.Memory.Clone()
		if block == nil {
			block = &core.ItemMemory{}
		}
		block.Version, block.SupersededBy, block.ValidTo = b.version(), newer.ID, b.now
		status := core.MemorySuperseded
		it, _, err := b.sc.store.UpdateReport(ctx, old.ID, core.ItemPatch{Status: &status, Memory: block}, old.Rev)
		if err != nil {
			res := fromError(err)
			res.Message = fmt.Sprintf("%s was written but superseding %s failed: %s", newer.ID, old.ID, res.Message)
			return res, false
		}
		b.remember(it)
	}
	return MemoryWriteResult{}, true
}

func (b *memoryBatch) forget(ctx context.Context, w MemoryWrite) MemoryWriteResult {
	current := b.byKey[w.Key]
	if current == nil {
		return rejected("not_found", fmt.Sprintf("no memory %q in %s", w.Key, b.sc.scope))
	}
	if res, isStale := stale(w, current); isStale {
		return res
	}
	var target core.Status
	switch current.Status {
	case core.MemoryActive:
		target = core.MemoryForgotten
	case core.MemoryProposed:
		// A proposal nobody accepted is withdrawn, not forgotten: it was never
		// visible, so it leaves no tombstone.
		target = core.MemoryCancelled
	default:
		return written(MemoryDuplicate, current)
	}
	block := current.Memory.Clone()
	if block == nil {
		block = &core.ItemMemory{Key: w.Key}
	}
	block.Version, block.ValidTo = b.version(), b.now
	it, _, err := b.sc.store.UpdateReport(ctx, current.ID, core.ItemPatch{Status: &target, Memory: block}, current.Rev)
	if err != nil {
		return fromError(err)
	}
	b.remember(it)
	return written(MemoryForgotten, it)
}

func written(outcome string, it *core.Item) MemoryWriteResult {
	res := MemoryWriteResult{Outcome: outcome, ID: it.ID, Rev: it.Rev}
	if it.Memory != nil {
		res.Version = it.Memory.Version
	}
	return res
}

// fromError turns a store failure into a per-entry rejection.
func fromError(err error) MemoryWriteResult {
	e, _ := AsError(err)
	if e == nil {
		e = &Error{Code: "internal", Message: err.Error()}
	}
	return rejected(e.Code, e.Message)
}

// memoryTitle is the title an entry asks for, or the first line of its
// content, or its key.
func memoryTitle(w MemoryWrite) string {
	if t := strings.TrimSpace(w.Title); t != "" {
		return truncate(t, 200)
	}
	for _, line := range strings.Split(w.Content, "\n") {
		if line = strings.TrimSpace(strings.TrimLeft(line, "# ")); line != "" {
			return truncate(line, 200)
		}
	}
	return w.Key
}

// truncate cuts s to at most n runes.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}

func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// memoryGet returns one memory by id, or by project and key.
func (v *Vault) memoryGet(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		ID      string `json:"id,omitempty"`
		Project string `json:"project,omitempty"`
		Key     string `json:"key,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	if p.ID != "" {
		it, err := v.index.Item(core.ItemID(p.ID))
		if err != nil {
			return nil, fmt.Errorf("get %s: %w", p.ID, err)
		}
		if it.Type != core.TypeMemory {
			return nil, failf("not_found", "%s is a %s, not a memory", p.ID, it.Type)
		}
		return it, nil
	}
	sc, err := v.memoryScopeFor(core.ProjectKey(p.Project))
	if err != nil {
		return nil, err
	}
	for _, it := range v.index.Memories(sc.key) {
		if it.Memory != nil && it.Memory.Key == p.Key {
			return it, nil
		}
	}
	return nil, failf("not_found", "no memory %q in %s", p.Key, sc.scope)
}

// memoryList returns the memories of a scope, optionally narrowed by status,
// in version order.
func (v *Vault) memoryList(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Project  string   `json:"project,omitempty"`
		Statuses []string `json:"statuses,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	sc, err := v.memoryScopeFor(core.ProjectKey(p.Project))
	if err != nil {
		return nil, err
	}
	want := map[core.Status]bool{}
	for _, s := range p.Statuses {
		want[core.Status(s)] = true
	}
	out := []core.Item{}
	for _, it := range v.index.Memories(sc.key) {
		if len(want) == 0 || want[it.Status] {
			out = append(out, *it)
		}
	}
	return map[string]any{"scope": sc.scope, "items": out}, nil
}

// memoryChanges is the changes feed of a scope: every memory whose version is
// above since, in version order, as a record while it is visible (active or
// superseded) and as a tombstone once forgotten. Proposals and rejected
// proposals were never visible and are skipped, so accepting a proposal, which
// bumps its version, is what first shows it.
func (v *Vault) memoryChanges(raw []byte) (any, error) {
	p, err := decodeParams[struct {
		Project string `json:"project,omitempty"`
		Since   int64  `json:"since,omitempty"`
		Limit   int    `json:"limit,omitempty"`
	}](raw)
	if err != nil {
		return nil, err
	}
	if p.Since < 0 {
		return nil, failf("invalid_request", "since must not be negative")
	}
	limit := p.Limit
	if limit <= 0 || limit > core.MaxLimit {
		limit = core.MaxLimit
	}
	sc, err := v.memoryScopeFor(core.ProjectKey(p.Project))
	if err != nil {
		return nil, err
	}
	out := MemoryChanges{Scope: sc.scope, Records: []core.Item{}, Tombstones: []MemoryTombstone{}, Cursor: p.Since}
	taken := 0
	for _, it := range v.index.Memories(sc.key) {
		if it.Memory == nil || it.Memory.Version <= p.Since {
			continue
		}
		if taken == limit {
			out.HasMore = true
			break
		}
		switch it.Status {
		case core.MemoryActive, core.MemorySuperseded:
			out.Records = append(out.Records, *it)
		case core.MemoryForgotten:
			out.Tombstones = append(out.Tombstones, MemoryTombstone{
				ID: it.ID, Key: it.Memory.Key, LocalID: it.Memory.LocalID,
				Version: it.Memory.Version, DeletedAt: it.Memory.ValidTo,
			})
		}
		// Skipped entries still advance the cursor: a proposal's version must
		// not be handed out again.
		out.Cursor = it.Memory.Version
		taken++
	}
	return out, nil
}
