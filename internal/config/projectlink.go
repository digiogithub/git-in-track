package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The `integrations.youtrack` block of project.yaml, documented in
// docs/03-data-model.md section 6.
//
// It is the committed half of the YouTrack connection: where the instance is,
// which remote project the backlog mirrors and how the two are mapped. A clone
// therefore knows where its items came from without holding a credential — the
// token lives on the machine, in the 0600 companion configuration (ADR-032).
//
// It is read and written here rather than in internal/core on purpose. The core
// struct models no unknown keys, and project.yaml carries hand-written comments
// and sections this build does not know about, so the block is edited in place,
// node by node, exactly the way the id allocator rewrites its counters.
const (
	// integrationsKey is the top-level key of project.yaml the block sits under.
	integrationsKey = "integrations"
	// youtrackKey is the block itself.
	youtrackKey = "youtrack"
)

// PushCommentsMode says when a comment written in git-in-track is pushed to the
// linked YouTrack issue.
type PushCommentsMode string

// The push modes of docs/03 section 6.
const (
	// PushCommentsManual pushes only what a person explicitly sends. It is the
	// default, because a comment written in a backlog is not automatically
	// meant for the tracker's audience.
	PushCommentsManual PushCommentsMode = "manual"
	// PushCommentsAuto queues every comment on a linked item for the tracker.
	PushCommentsAuto PushCommentsMode = "auto"
)

// Valid reports whether the mode is one this build knows.
func (m PushCommentsMode) Valid() bool {
	return m == PushCommentsManual || m == PushCommentsAuto
}

// KBSyncMode says when a knowledge-base page is synchronized with a YouTrack
// article.
type KBSyncMode string

// The knowledge-base sync modes of docs/03 section 6.
const (
	// KBSyncManual synchronizes a page only when asked to.
	KBSyncManual KBSyncMode = "manual"
	// KBSyncOnWrite enqueues a publish whenever a page is saved.
	KBSyncOnWrite KBSyncMode = "on_write"
)

// Valid reports whether the mode is one this build knows.
func (m KBSyncMode) Valid() bool { return m == KBSyncManual || m == KBSyncOnWrite }

// KBSyncDirection says which way knowledge-base content flows.
type KBSyncDirection string

// The directions of docs/03 section 6.
const (
	// KBSyncPush writes git-in-track pages to YouTrack articles.
	KBSyncPush KBSyncDirection = "push"
	// KBSyncPull writes YouTrack articles to git-in-track pages.
	KBSyncPull KBSyncDirection = "pull"
	// KBSyncBoth does each in turn, newest wins, conflicts written aside.
	KBSyncBoth KBSyncDirection = "both"
)

// Valid reports whether the direction is one this build knows.
func (d KBSyncDirection) Valid() bool {
	return d == KBSyncPush || d == KBSyncPull || d == KBSyncBoth
}

// FieldMapKeys are the git-in-track fields a mapping entry may name, in the
// order the settings UI shows them. A key outside this set is refused at load
// time rather than silently ignored, because a typo in a field map is otherwise
// invisible until a sync writes the wrong field.
//
// These six are exactly the fields the importer translates
// (internal/youtrack/mapping, the Key* constants). Three keys that were once
// accepted are not here: see retiredFieldMapKeys.
var FieldMapKeys = []string{
	"status", "priority", "type", "assignee", "estimate", "milestone",
}

// retiredFieldMapKeys were accepted, stored and validated by earlier builds and
// then read by nothing at all, which is the worst of both worlds: a person
// configures a mapping, the file keeps it, and no sync ever honors it.
//
// They are refused rather than kept, with a message that says why, because a
// silent no-op is not something a configuration file should be able to express.
// Each one has a reason it is not a mapping in the first place, and that reason
// is the message: labels travel as YouTrack tags rather than through a custom
// field, and neither a due date nor a sprint is read from one.
var retiredFieldMapKeys = map[string]string{
	"labels": "labels travel as YouTrack tags, not through a custom field",
	"due":    "a due date is not read from a custom field",
	"sprint": "a sprint is not read from a custom field",
}

// knownFieldMapKey reports whether a field-map key names a git-in-track field.
func knownFieldMapKey(key string) bool {
	for _, known := range FieldMapKeys {
		if key == known {
			return true
		}
	}
	return false
}

// YouTrackLink is the parsed `integrations.youtrack` block.
type YouTrackLink struct {
	// URL is the instance URL, context path included when the instance has one,
	// as in https://yt.example.com/youtrack.
	URL string `json:"url" yaml:"url"`
	// Project is the YouTrack project short name, the "ACME" of ACME-42.
	Project string `json:"project" yaml:"project"`
	// FieldMap maps a git-in-track field onto the YouTrack custom field that
	// carries it and, for the three fields whose values are enumerable, onto
	// what those values mean here. Keys are drawn from FieldMapKeys; see
	// fieldmap.go for the two spellings an entry accepts.
	FieldMap FieldMap `json:"fieldMap,omitempty" yaml:"field_map,omitempty"`
	// PushComments is when a comment is pushed to the linked issue.
	PushComments PushCommentsMode `json:"pushComments,omitempty" yaml:"push_comments,omitempty"`
	// KBSync is when a knowledge-base page is synchronized.
	KBSync KBSyncMode `json:"kbSync,omitempty" yaml:"kb_sync,omitempty"`
	// KBSyncDirection is which way that synchronization flows.
	KBSyncDirection KBSyncDirection `json:"kbSyncDirection,omitempty" yaml:"kb_sync_direction,omitempty"`
	// CommentTemplate is the attribution line appended to a comment pushed to
	// the linked issue, as a text/template rendered against the comment's
	// author and the git-in-track item id (GIT-US-0068). An empty value means
	// the shipped default.
	//
	// It lives in project.yaml rather than in the machine-local file because it
	// is a team decision about how this project signs what it publishes, and a
	// clone must sign the same way.
	CommentTemplate string `json:"commentTemplate,omitempty" yaml:"comment_template,omitempty"`
}

// Normalized returns the link with its defaults filled in and its values
// trimmed, so that what is written back is what would be read.
func (l YouTrackLink) Normalized() YouTrackLink {
	out := l
	out.URL = strings.TrimRight(strings.TrimSpace(l.URL), "/")
	out.Project = strings.TrimSpace(l.Project)
	if out.PushComments == "" {
		out.PushComments = PushCommentsManual
	}
	if out.KBSync == "" {
		out.KBSync = KBSyncManual
	}
	if out.KBSyncDirection == "" {
		out.KBSyncDirection = KBSyncPush
	}
	out.FieldMap = l.FieldMap.Normalized()
	return out
}

// Validate checks the block and reports every problem at once, in the same
// FieldError shape the companion configuration uses.
func (l YouTrackLink) Validate() error {
	var errs FieldErrors
	add := func(field, format string, args ...any) {
		errs = append(errs, FieldError{
			Field:   integrationsKey + "." + youtrackKey + "." + field,
			Message: fmt.Sprintf(format, args...),
		})
	}
	link := l.Normalized()

	switch parsed, err := url.Parse(link.URL); {
	case link.URL == "":
		add("url", "must not be empty: give the instance URL, context path included")
	case err != nil:
		add("url", "is not a URL")
	case !parsed.IsAbs():
		add("url", "%q is not absolute: it must start with https:// or http://", link.URL)
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		add("url", "scheme %q is not http or https", parsed.Scheme)
	case parsed.Host == "":
		add("url", "%q has no host", link.URL)
	case parsed.RawQuery != "" || parsed.Fragment != "":
		add("url", "must be a bare instance URL, with no query and no fragment")
	}
	if link.Project == "" {
		add("project", "must not be empty: give the YouTrack project short name, the \"ACME\" of ACME-42")
	} else if strings.ContainsAny(link.Project, " /\\?#") {
		add("project", "%q is not a project short name", link.Project)
	}
	if !link.PushComments.Valid() {
		add("push_comments", "unknown mode %q: use manual or auto", link.PushComments)
	}
	if !link.KBSync.Valid() {
		add("kb_sync", "unknown mode %q: use manual or on_write", link.KBSync)
	}
	if !link.KBSyncDirection.Valid() {
		add("kb_sync_direction", "unknown direction %q: use push, pull or both", link.KBSyncDirection)
	}
	link.FieldMap.validate(add)

	if len(errs) == 0 {
		return nil
	}
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Field < errs[j].Field })
	return errs
}

// sortedKeys returns a map's keys in a stable order, so that two runs report
// the same problems in the same sequence.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// projectFile is the decoding target for the one section of project.yaml this
// package reads. Everything else in the file is left to internal/core.
type projectFile struct {
	Integrations struct {
		YouTrack *YouTrackLink `yaml:"youtrack"`
	} `yaml:"integrations"`
}

// LoadYouTrackLink reads the `integrations.youtrack` block of a project.yaml.
// A file without the block, and a missing file, both yield a nil link and no
// error: not being connected to YouTrack is the normal state of a project.
func LoadYouTrackLink(path string) (*YouTrackLink, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is a project.yaml the caller already located
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil //nolint:nilnil // "no file, no link" is the answer, not a failure
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseYouTrackLink(data, path)
}

// ParseYouTrackLink decodes the block out of project.yaml bytes and validates
// it, so that an unusable link is a load error rather than a silent misconfig.
func ParseYouTrackLink(data []byte, path string) (*YouTrackLink, error) {
	var file projectFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if file.Integrations.YouTrack == nil {
		return nil, nil //nolint:nilnil // an absent block is not an error
	}
	link := file.Integrations.YouTrack.Normalized()
	if err := link.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &link, nil
}

// SaveYouTrackLink writes the block into project.yaml in place, editing the
// YAML node tree so that comments, key order and every section this build does
// not model survive untouched. It reports whether the file changed.
//
// The bytes go to a temporary file in the same directory and are renamed over
// the original, so a reader never sees a half-written project.yaml.
func SaveYouTrackLink(path string, link YouTrackLink) (bool, error) {
	normalized := link.Normalized()
	if err := normalized.Validate(); err != nil {
		return false, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is a project.yaml the caller already located
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	out, err := setYouTrackLink(data, normalized)
	if err != nil {
		return false, fmt.Errorf("update %s: %w", path, err)
	}
	if out == nil {
		return false, nil
	}
	if err := writeFileInPlace(path, out); err != nil {
		return false, err
	}
	return true, nil
}

// setYouTrackLink edits the parsed document. It returns nil when the file
// already says exactly this, so that a no-op settings write does not rewrite a
// tracked file and dirty the working tree.
func setYouTrackLink(data []byte, link YouTrackLink) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse the document: %w", err)
	}
	root := documentMapping(&doc)
	if root == nil {
		return nil, errors.New("the document is not a mapping")
	}
	integrations, err := ensureMapping(root, integrationsKey)
	if err != nil {
		return nil, err
	}
	block, err := ensureMapping(integrations, youtrackKey)
	if err != nil {
		return nil, err
	}

	changed := false
	changed = setScalar(block, "url", link.URL) || changed
	changed = setScalar(block, "project", link.Project) || changed
	changed = setScalar(block, "push_comments", string(link.PushComments)) || changed
	changed = setScalar(block, "kb_sync", string(link.KBSync)) || changed
	changed = setScalar(block, "kb_sync_direction", string(link.KBSyncDirection)) || changed
	changed = setFieldMap(block, link.FieldMap) || changed
	if !changed {
		return nil, nil
	}
	return encodeDocument(&doc)
}

// documentMapping returns the mapping node of a parsed document, building one
// when the file was empty.
func documentMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		return doc.Content[0]
	}
	node := doc
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			node.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
		}
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

// mapGet returns the value node stored under a key of a mapping node.
func mapGet(m *yaml.Node, key string) (*yaml.Node, bool) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1], true
		}
	}
	return nil, false
}

// mapSet appends a key and its value to a mapping node.
func mapSet(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value)
}

// mapDelete removes a key and its value from a mapping node, reporting whether
// anything was there.
func mapDelete(m *yaml.Node, key string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

// ensureMapping returns the mapping stored under a key, creating an empty one
// when the key is absent. A key holding something that is not a mapping is a
// refusal rather than an overwrite: the file belongs to the user.
func ensureMapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	if existing, ok := mapGet(parent, key); ok {
		if existing.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s is not a mapping", key)
		}
		return existing, nil
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapSet(parent, key, node)
	return node, nil
}

// setScalar writes a scalar value under a key, replacing only the value node so
// that the comment above the key survives. It reports whether it changed
// anything.
func setScalar(parent *yaml.Node, key, value string) bool {
	existing, ok := mapGet(parent, key)
	if !ok {
		mapSet(parent, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
		return true
	}
	if existing.Kind == yaml.ScalarNode && existing.Value == value {
		return false
	}
	existing.Kind = yaml.ScalarNode
	existing.Tag = "!!str"
	existing.Style = 0
	existing.Value = value
	existing.Content = nil
	return true
}

// setFieldMap replaces the field_map mapping, dropping the key entirely when the
// mapping is empty rather than leaving `field_map: {}` behind.
//
// An entry with no value map is written as a scalar — the flat form — and one
// with a value map as a `{field, values}` mapping, so the file only grows the
// nesting a project actually asked for.
func setFieldMap(parent *yaml.Node, fields FieldMap) bool {
	if len(fields) == 0 {
		return mapDelete(parent, "field_map")
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, key := range fields.Keys() {
		mapSet(node, key, fieldMappingNode(fields[key]))
	}
	existing, ok := mapGet(parent, "field_map")
	if ok && sameNode(existing, node) {
		return false
	}
	if ok {
		mapDelete(parent, "field_map")
	}
	mapSet(parent, "field_map", node)
	return true
}

// fieldMappingNode renders one entry in the shortest spelling that says all of
// it.
func fieldMappingNode(entry FieldMapping) *yaml.Node {
	if len(entry.Values) == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Field}
	}
	values := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, from := range sortedKeys(entry.Values) {
		mapSet(values, from, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Values[from]})
	}
	out := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mapSet(out, "field", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Field})
	mapSet(out, "values", values)
	return out
}

// sameNode reports whether two scalar-or-mapping trees hold the same content.
// It is what decides that a settings write changes nothing and the file is left
// untouched, so it compares values rather than formatting: a comment, a quoting
// style and a key order are not content.
func sameNode(a, b *yaml.Node) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case yaml.ScalarNode:
		return a.Value == b.Value
	case yaml.MappingNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := 0; i+1 < len(a.Content); i += 2 {
			value, ok := mapGet(b, a.Content[i].Value)
			if !ok || !sameNode(a.Content[i+1], value) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// yamlIndent is the indentation project.yaml is written with; it is what
// yaml.v3 produces by default for this project's files.
const yamlIndent = 2

// encodeDocument renders the edited node tree back to bytes.
func encodeDocument(doc *yaml.Node) ([]byte, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode the document: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode the document: %w", err)
	}
	return []byte(buf.String()), nil
}

// projectFileMode is the permission a project.yaml is created with when the
// original's mode cannot be read. project.yaml is a tracked, shared file: unlike
// the companion configuration it holds no secret and is world-readable.
const projectFileMode = 0o644

// writeFileInPlace replaces a file atomically, keeping its current permissions.
func writeFileInPlace(path string, data []byte) error {
	mode := os.FileMode(projectFileMode)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".project-yaml-*.tmp")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // a no-op once the rename succeeded

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(name, mode); err != nil {
		return fmt.Errorf("set the permissions of %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
