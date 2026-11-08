package rag

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"

	"github.com/digiogithub/git-in-track/internal/core"
)

// MemoryStore is the dependency-free Store the hub runs until the pando/rag
// module is tagged: documents in memory, ranked by how many query words they
// contain, with the same filters, watermark and rename semantics. It is also
// what the tests run against.
type MemoryStore struct {
	mu        sync.RWMutex
	docs      map[string]*doc
	watermark string
	// reads counts the documents read from content or the file system, so a
	// test can prove that a rename of an unchanged blob reads nothing.
	reads atomic.Int64
}

type doc struct {
	path, title, body, kind, status, rev, blob string
	labels                                     []string
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{docs: map[string]*doc{}} }

// Reads is how many documents the store has read.
func (s *MemoryStore) Reads() int64 { return s.reads.Load() }

// ContentRev is the rev a store records for content: the core's content hash.
func ContentRev(content []byte) string { return string(core.ComputeRev(content)) }

// ApplyChanges implements Store.
func (s *MemoryStore) ApplyChanges(ctx context.Context, fsys fs.FS, changes []Change, watermark string) (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st Stats
	if watermark != "" && watermark == s.watermark {
		return st, nil
	}
	for _, c := range changes {
		if err := ctx.Err(); err != nil {
			return st, fmt.Errorf("apply changes: %w", err)
		}
		switch c.Op {
		case OpDelete:
			delete(s.docs, c.Path)
			st.Deleted++
		case OpRename:
			if old, ok := s.docs[c.OldPath]; ok && c.Blob != "" && old.blob == c.Blob {
				delete(s.docs, c.OldPath)
				old.path = c.Path
				s.docs[c.Path] = old
				st.Renamed++
				continue
			}
			delete(s.docs, c.OldPath)
			fallthrough
		case OpAdd, OpModify:
			content := c.Content
			if content == nil {
				data, err := fs.ReadFile(fsys, c.Path)
				if err != nil {
					return st, fmt.Errorf("read %s: %w", c.Path, err)
				}
				content = data
			}
			s.reads.Add(1)
			d := parseDoc(c.Path, content)
			d.blob = c.Blob
			if prev, ok := s.docs[c.Path]; ok && prev.rev == d.rev {
				st.Skipped++
				continue
			}
			if _, ok := s.docs[c.Path]; ok {
				st.Updated++
			} else {
				st.Added++
			}
			s.docs[c.Path] = d
		default:
			return st, fmt.Errorf("unknown change op %q", c.Op)
		}
	}
	s.watermark = watermark
	return st, nil
}

// parseDoc reads the front matter a filter needs: kind from type, status,
// title and labels. A page without front matter is an active page.
func parseDoc(path string, content []byte) *doc {
	d := &doc{path: path, kind: "page", status: "active", rev: ContentRev(content), body: string(content)}
	if fm, body, err := core.SplitFrontMatter(content); err == nil && len(fm) > 0 {
		d.body = body
		// Only what the filters read: an index must not drop a document
		// because the rest of its front matter does not validate.
		var meta struct {
			Type   string   `yaml:"type"`
			Title  string   `yaml:"title"`
			Status string   `yaml:"status"`
			Labels []string `yaml:"labels"`
		}
		if yaml.Unmarshal(fm, &meta) == nil {
			if meta.Type != "" {
				d.kind = meta.Type
			}
			if meta.Status != "" {
				d.status = meta.Status
			}
			d.title, d.labels = meta.Title, meta.Labels
		}
	}
	if d.title == "" {
		for _, line := range strings.Split(d.body, "\n") {
			if t := strings.TrimSpace(strings.TrimLeft(line, "# ")); t != "" {
				d.title = t
				break
			}
		}
	}
	return d
}

// Watermark implements Store.
func (s *MemoryStore) Watermark(context.Context) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watermark, nil
}

// Search implements Store.
func (s *MemoryStore) Search(_ context.Context, q string, query Query) ([]Hit, error) {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return nil, nil
	}
	statuses := query.Statuses
	if len(statuses) == 0 {
		statuses = []string{"active"}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hits []Hit
	for _, d := range s.docs {
		if !admits(d, query, statuses) {
			continue
		}
		hay := strings.ToLower(d.title + "\n" + d.body)
		score := 0
		for _, w := range words {
			score += strings.Count(hay, w)
		}
		if score == 0 {
			continue
		}
		hits = append(hits, Hit{
			Path: d.path, Chunk: excerpt(d.body, words[0]), Score: float64(score), Kind: d.kind,
			Status: d.status, Rev: d.rev, GitBlob: d.blob, Title: d.title,
			StartLine: 1, EndLine: strings.Count(d.body, "\n") + 1,
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})
	if query.Limit > 0 && len(hits) > query.Limit {
		hits = hits[:query.Limit]
	}
	return hits, nil
}

func admits(d *doc, q Query, statuses []string) bool {
	if len(q.PathPrefixes) > 0 && !hasPrefix(d.path, q.PathPrefixes) {
		return false
	}
	if hasPrefix(d.path, q.ExcludePrefixes) {
		return false
	}
	if len(q.Kinds) > 0 && !contains(q.Kinds, d.kind) {
		return false
	}
	if !contains(statuses, AnyStatus) && !contains(statuses, d.status) {
		return false
	}
	for _, tag := range q.Tags {
		if !contains(d.labels, tag) {
			return false
		}
	}
	return true
}

func hasPrefix(p string, prefixes []string) bool {
	for _, pre := range prefixes {
		if pre != "" && (p == pre || strings.HasPrefix(p, strings.TrimSuffix(pre, "/")+"/")) {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

func excerpt(body, word string) string {
	i := strings.Index(strings.ToLower(body), word)
	if i < 0 {
		return ""
	}
	start, end := max(0, i-60), min(len(body), i+len(word)+60)
	return strings.TrimSpace(body[start:end])
}

// Close implements Store.
func (s *MemoryStore) Close() error { return nil }
