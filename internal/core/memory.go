package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// This file holds memory items (ADR-044): short, explicit facts with an author,
// a scope and a review queue, stored as ordinary item files.
//
// A memory is an item of type memory with the id code MEM. What makes it more
// than a page is the `memory:` block, which carries the fields the
// alchemai.memory.v2 protocol needs (key, version, content_hash, local_id,
// validity and supersession), and a fixed lifecycle that project.yaml cannot
// redefine. Its scope is derived from where the file lives and is never stored.

// MemoryDirName is the folder memory items live in, next to the other item
// folders of a backlog, or directly under a team repository's `.pmngr/`.
const MemoryDirName = "memory"

// The statuses of the memory lifecycle (ADR-044 section 3).
const (
	MemoryProposed   Status = "proposed"
	MemoryActive     Status = "active"
	MemorySuperseded Status = "superseded"
	MemoryForgotten  Status = "forgotten"
	MemoryCancelled  Status = "cancelled"
)

// MemoryWorkflow is the fixed lifecycle of every memory item. proposed is in
// the triage category, so the inbox is the review queue; accepting moves a
// memory to active, rejecting to cancelled, and a rejected proposal may be
// reopened.
func MemoryWorkflow() Workflow {
	return Workflow{
		Initial: MemoryProposed,
		Statuses: []StatusDef{
			{ID: MemoryProposed, Name: "Proposed", Category: CategoryTriage},
			{ID: MemoryActive, Name: "Active", Category: CategoryDone},
			{ID: MemorySuperseded, Name: "Superseded", Category: CategoryDone, Terminal: true},
			// cancelled comes before forgotten: rejecting a proposal moves it to
			// the first status of the cancelled category.
			{ID: MemoryCancelled, Name: "Cancelled", Category: CategoryCancelled},
			{ID: MemoryForgotten, Name: "Forgotten", Category: CategoryCancelled},
		},
		Transitions: map[Status][]Status{
			MemoryProposed:  {MemoryActive, MemoryCancelled},
			MemoryActive:    {MemorySuperseded, MemoryForgotten},
			MemoryForgotten: {MemoryActive},
			MemoryCancelled: {MemoryProposed},
		},
	}
}

// For returns the configuration that governs an item of type t: the project
// itself for every backlog type, and for a memory the same project with the
// fixed memory lifecycle in place of its workflow. Every check that reads a
// status of an item that may be a memory goes through it, so project.yaml can
// never redefine the memory statuses.
func (p *ProjectConfig) For(t ItemType) *ProjectConfig {
	if t != TypeMemory {
		return p
	}
	cp := ProjectConfig{}
	if p != nil {
		cp = *p
	}
	cp.Workflow = MemoryWorkflow()
	return &cp
}

// MemorySourceKind is where a memory came from.
type MemorySourceKind string

// The source kinds of ADR-044.
const (
	MemorySourceAgent        MemorySourceKind = "agent"
	MemorySourceHuman        MemorySourceKind = "human"
	MemorySourceConversation MemorySourceKind = "conversation"
	MemorySourceDocument     MemorySourceKind = "document"
	MemorySourceExternal     MemorySourceKind = "external"
)

// Valid reports whether k is one of the five source kinds.
func (k MemorySourceKind) Valid() bool {
	switch k {
	case MemorySourceAgent, MemorySourceHuman, MemorySourceConversation, MemorySourceDocument, MemorySourceExternal:
		return true
	default:
		return false
	}
}

// MemorySource records where a memory came from: a kind and a free reference
// (an item id, a page path, an agent instance, a URL).
type MemorySource struct {
	Kind MemorySourceKind `json:"kind,omitempty" yaml:"kind,omitempty"`
	Ref  string           `json:"ref,omitempty" yaml:"ref,omitempty"`
}

// ItemMemory is the `memory:` block of a memory item.
type ItemMemory struct {
	// Key is the stable slug a memory is upserted by, unique in its scope.
	Key string `json:"key" yaml:"key"`
	// Version is a positive integer, strictly increasing per project; it is the
	// cursor of the protocol's changes feed, so it lives in the file and
	// survives an index rebuild.
	Version int64 `json:"version" yaml:"version"`
	// ContentHash is the sha256 of MemoryContentHash, written by the vault.
	ContentHash string `json:"contentHash,omitempty" yaml:"content_hash,omitempty"`
	// LocalID is the protocol identity, <scope>/<key>.
	LocalID string `json:"localId,omitempty" yaml:"local_id,omitempty"`
	// ValidFrom and ValidTo bound when the fact holds. ValidTo is required once
	// the memory is forgotten.
	ValidFrom Timestamp `json:"validFrom,omitempty" yaml:"valid_from,omitempty"`
	ValidTo   Timestamp `json:"validTo,omitempty" yaml:"valid_to,omitempty"`
	// SupersededBy mirrors the supersedes link of the memory that replaced this
	// one. It is computed, never hand-written.
	SupersededBy ItemID `json:"supersededBy,omitempty" yaml:"superseded_by,omitempty"`
	// Source is where the memory came from.
	Source *MemorySource `json:"source,omitempty" yaml:"source,omitempty"`

	// Extra preserves the keys inside the block this version does not know.
	Extra map[string]any `json:"extra,omitempty" yaml:"-"`
}

// Clone returns a deep copy, including the preserved unknown keys.
func (m *ItemMemory) Clone() *ItemMemory {
	if m == nil {
		return nil
	}
	out := *m
	if m.Source != nil {
		src := *m.Source
		out.Source = &src
	}
	if m.Extra != nil {
		out.Extra = make(map[string]any, len(m.Extra))
		for k, v := range m.Extra {
			out.Extra[k] = v
		}
	}
	return &out
}

// memoryKeyOrder is the canonical order of the block's known keys.
var memoryKeyOrder = []string{
	"key", "version", "content_hash", "local_id", "valid_from", "valid_to", "superseded_by", "source",
}

// memoryKnownKeys is the set of keys inside the block this version understands.
var memoryKnownKeys = func() map[string]bool {
	out := make(map[string]bool, len(memoryKeyOrder))
	for _, k := range memoryKeyOrder {
		out[k] = true
	}
	return out
}()

// memoryKeyRE is the grammar of a memory key.
var memoryKeyRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,127}$`)

// ValidMemoryKey reports whether key matches the memory key grammar.
func ValidMemoryKey(key string) bool { return memoryKeyRE.MatchString(key) }

// contentHashRE is a lowercase sha256 in hex.
var contentHashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// MemoryContentHash is the deduplication hash of alchemai.memory.v2: the
// sha256, in lowercase hex, of kind, scope, key, path and content joined by the
// unit separator U+001F. It is computed here rather than imported from the
// protocol SDK so that the core stays free of that dependency; a fixed test
// vector pins the two together.
func MemoryContentHash(kind, scope, key, path, content string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{kind, scope, key, path, content}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// MemoryScope is the protocol scope of the memories a project or team scope
// holds: "project/<key>" for a project, "org/<key>" for a team, lower-cased.
// It is derived from where the files live and is never stored in them.
func MemoryScope(ref ProjectRef) string {
	if ref.Team {
		return "org/" + strings.ToLower(string(ref.Key))
	}
	return "project/" + strings.ToLower(string(ref.Key))
}

// memory reads the `memory:` block.
func (p *fieldReader) memory(key string) *ItemMemory {
	m := p.mapping(key)
	if m == nil {
		return nil
	}
	out := &ItemMemory{
		Key:          stringOf(m["key"]),
		ContentHash:  stringOf(m["content_hash"]),
		LocalID:      stringOf(m["local_id"]),
		SupersededBy: ItemID(stringOf(m["superseded_by"])),
	}
	if raw, has := m["version"]; has && raw != nil {
		n, err := strconv.ParseInt(stringOf(raw), 10, 64)
		if err != nil {
			p.fail(key+".version", CodeFieldType, fmt.Sprintf("want an integer, got %q", stringOf(raw)))
		} else {
			out.Version = n
		}
	}
	for _, field := range []struct {
		name string
		dst  *Timestamp
	}{{"valid_from", &out.ValidFrom}, {"valid_to", &out.ValidTo}} {
		raw, has := m[field.name]
		if !has || raw == nil {
			continue
		}
		ts, err := ParseTimestamp(stringOf(raw))
		if err != nil {
			p.fail(key+"."+field.name, CodeDateFormat,
				fmt.Sprintf("%q is not an ISO 8601 UTC timestamp (%s)", stringOf(raw), TimestampLayout))
			continue
		}
		*field.dst = ts
	}
	if raw, has := m["source"]; has && raw != nil {
		src, ok := raw.(map[string]any)
		if !ok {
			p.fail(key+".source", CodeFieldType, fmt.Sprintf("want a mapping, got %T", raw))
		} else {
			out.Source = &MemorySource{Kind: MemorySourceKind(stringOf(src["kind"])), Ref: stringOf(src["ref"])}
		}
	}
	for k, v := range m {
		if memoryKnownKeys[k] || v == nil {
			continue
		}
		if out.Extra == nil {
			out.Extra = make(map[string]any)
		}
		out.Extra[k] = v
	}
	return out
}

// memory writes the `memory:` block in canonical key order, then the keys this
// version does not know.
func (w *fmWriter) memory(key string, m *ItemMemory) error {
	if m == nil {
		return nil
	}
	w.b.WriteString(key)
	w.b.WriteString(":\n")
	for _, k := range memoryKeyOrder {
		switch k {
		case "key":
			w.b.WriteString("  key: " + yamlString(m.Key) + "\n")
		case "version":
			w.b.WriteString("  version: " + strconv.FormatInt(m.Version, 10) + "\n")
		case "content_hash":
			if m.ContentHash != "" {
				w.b.WriteString("  content_hash: " + yamlString(m.ContentHash) + "\n")
			}
		case "local_id":
			if m.LocalID != "" {
				w.b.WriteString("  local_id: " + yamlString(m.LocalID) + "\n")
			}
		case "valid_from":
			if !m.ValidFrom.IsZero() {
				w.b.WriteString("  valid_from: " + m.ValidFrom.String() + "\n")
			}
		case "valid_to":
			if !m.ValidTo.IsZero() {
				w.b.WriteString("  valid_to: " + m.ValidTo.String() + "\n")
			}
		case "superseded_by":
			if m.SupersededBy != "" {
				w.b.WriteString("  superseded_by: " + yamlString(string(m.SupersededBy)) + "\n")
			}
		case "source":
			if m.Source != nil && (m.Source.Kind != "" || m.Source.Ref != "") {
				parts := make([]string, 0, 2)
				if m.Source.Kind != "" {
					parts = append(parts, "kind: "+yamlString(string(m.Source.Kind)))
				}
				if m.Source.Ref != "" {
					parts = append(parts, "ref: "+yamlString(m.Source.Ref))
				}
				w.b.WriteString("  source: {" + strings.Join(parts, ", ") + "}\n")
			}
		}
	}
	return w.writeMapBody(2, m.Extra)
}

// validateMemory applies the E-MEM-* rules of ADR-044 to one file, and
// E-KIND-NOT-BACKLOG to an item of a knowledge-only project.
func validateMemory(d *diagSet, item *Item, cfg *ProjectConfig) {
	if cfg != nil && cfg.IsKnowledge() && item.Type != TypeMemory && item.Type != TypeComment {
		d.errorf("type", CodeKindNotBacklog,
			"project %s is knowledge-only (kind: knowledge) and holds no %s items", cfg.Key, item.Type)
	}
	if item.Type != TypeMemory {
		if item.Memory != nil {
			d.errorf("memory", CodeMemoryBlock, "a memory block only belongs on a memory item, not on a %s", item.Type)
		}
		return
	}
	m := item.Memory
	if m == nil {
		d.errorf("memory", CodeMemoryBlock, "a memory item needs a memory block with at least key and version")
		return
	}
	if !ValidMemoryKey(m.Key) {
		d.errorf("memory.key", CodeMemoryKey, "%q does not match %s", m.Key, memoryKeyRE)
	}
	if m.Version < 1 {
		d.errorf("memory.version", CodeMemoryVersion, "the version must be a positive integer, not %d", m.Version)
	}
	if m.ContentHash != "" && !contentHashRE.MatchString(m.ContentHash) {
		d.errorf("memory.content_hash", CodeMemoryHash, "%q is not a lowercase sha256 in hex", m.ContentHash)
	}
	if m.LocalID != "" && !validLocalID(m.LocalID, m.Key) {
		d.errorf("memory.local_id", CodeMemoryLocalID,
			"%q must be <scope>/%s with a project/ or org/ scope", m.LocalID, m.Key)
	}
	if item.Status == MemoryForgotten && m.ValidTo.IsZero() {
		d.errorf("memory.valid_to", CodeMemoryValidTo, "a forgotten memory records when it stopped holding")
	}
	if !m.ValidFrom.IsZero() && !m.ValidTo.IsZero() && m.ValidTo.Before(m.ValidFrom.Time) {
		d.errorf("memory.valid_to", CodeMemoryValidTo, "valid_to is before valid_from")
	}
	if m.Source != nil && m.Source.Kind != "" && !m.Source.Kind.Valid() {
		d.errorf("memory.source.kind", CodeEnum, "unknown source kind %q", m.Source.Kind)
	}
	planning := []struct {
		field string
		set   bool
	}{
		{"epic", item.Epic != ""}, {"milestone", item.Milestone != ""},
		{"sprint", item.Sprint != ""}, {"estimate", item.Estimate != nil}, {"effort", item.Effort != nil},
		{"assignees", len(item.Assignees) > 0},
	}
	for _, f := range planning {
		if f.set {
			d.errorf(f.field, CodeMemoryPlanning, "a memory is knowledge, not work: it takes no %s", f.field)
		}
	}
}

// validLocalID reports whether id is "<project|org>/<name>/<key>".
func validLocalID(id, key string) bool {
	scope, ok := strings.CutSuffix(id, "/"+key)
	if !ok {
		return false
	}
	kind, name, ok := strings.Cut(scope, "/")
	return ok && name != "" && !strings.Contains(name, "/") && (kind == "project" || kind == "org")
}

// MemoryVersionFix is one renumbering ReconcileMemoryVersions asks for.
type MemoryVersionFix struct {
	ID   ItemID `json:"id"`
	Path string `json:"path"`
	From int64  `json:"from"`
	To   int64  `json:"to"`
}

// NextMemoryVersion is the version the next memory write of a project takes:
// one more than the highest version any of its memories holds.
func NextMemoryVersion(memories []*Item) int64 {
	var top int64
	for _, it := range memories {
		if it != nil && it.Memory != nil && it.Memory.Version > top {
			top = it.Memory.Version
		}
	}
	return top + 1
}

// ReconcileMemoryVersions repairs the duplicate versions an offline merge can
// leave behind: two writers each took max+1 before they saw each other. Within
// each duplicated version the memory with the earliest id keeps it, and every
// other one moves past the highest version of the project, in id order, so the
// result is deterministic whoever runs it. Versions only ever grow, which is
// what a client's changes cursor needs: a renumbered memory is simply seen
// again.
func ReconcileMemoryVersions(memories []*Item) []MemoryVersionFix {
	byVersion := map[int64][]*Item{}
	for _, it := range memories {
		if it == nil || it.Memory == nil || it.Memory.Version < 1 {
			continue
		}
		byVersion[it.Memory.Version] = append(byVersion[it.Memory.Version], it)
	}
	var losers []*Item
	for _, group := range byVersion {
		if len(group) < 2 {
			continue
		}
		sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })
		losers = append(losers, group[1:]...)
	}
	if len(losers) == 0 {
		return nil
	}
	sort.Slice(losers, func(i, j int) bool { return losers[i].ID < losers[j].ID })
	next := NextMemoryVersion(memories)
	fixes := make([]MemoryVersionFix, 0, len(losers))
	for _, it := range losers {
		fixes = append(fixes, MemoryVersionFix{ID: it.ID, Path: it.Path, From: it.Memory.Version, To: next})
		next++
	}
	return fixes
}
