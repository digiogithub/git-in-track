package gitops

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// This file reads the history of a set of files out of a repository: every
// revision of every item file the metrics of GIT-US-0028 need, with the instant
// it was committed. It is the native half of the answer to "where does the time
// series come from" (ADR-017) — the domain half, turning revisions into
// observations and observations into charts, lives in internal/core.
//
// Nothing here is a source of truth. The result is derived from the commits and
// can be thrown away and rebuilt at any time, which is exactly what the cache
// below does when HEAD moves.

// DefaultHistoryLimit bounds a history walk. A repository with more commits
// touching the requested paths is read back to the limit and the result says so
// (FileHistory.Truncated), rather than either lying or taking forever.
const DefaultHistoryLimit = 2000

// HistoryRequest selects the revisions of a set of files.
type HistoryRequest struct {
	// Paths are repository-relative and forward-slashed. An empty list reads
	// nothing.
	Paths []string
	// Since drops commits older than this instant. The zero time reads back to
	// the beginning of the history.
	Since time.Time
	// Limit bounds the commits read per path. Zero means DefaultHistoryLimit.
	Limit int
}

// normalized returns the request with its paths deduplicated and sorted and its
// limit defaulted, so that two equivalent requests hash to the same cache key.
func (r HistoryRequest) normalized() HistoryRequest {
	seen := map[string]bool{}
	paths := make([]string, 0, len(r.Paths))
	for _, p := range r.Paths {
		p = strings.TrimPrefix(strings.TrimSpace(p), "./")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if r.Limit <= 0 {
		r.Limit = DefaultHistoryLimit
	}
	r.Paths = paths
	return r
}

// key is the cache key of a request. HEAD is added by the cache.
func (r HistoryRequest) key() string {
	var b strings.Builder
	b.WriteString(r.Since.UTC().Format(time.RFC3339))
	b.WriteByte('|')
	b.WriteString(strings.Join(r.Paths, ","))
	return b.String()
}

// FileRevision is one version of one file: its bytes as they stood at a commit,
// and when that commit was authored.
type FileRevision struct {
	Path string    `json:"path"`
	SHA  string    `json:"sha"`
	When time.Time `json:"when"`
	// Deleted reports a revision that removed the file; Data is then empty.
	Deleted bool   `json:"deleted,omitempty"`
	Data    []byte `json:"-"`
}

// FileHistory is what a walk produced.
type FileHistory struct {
	// Revisions are sorted by When ascending, then by path.
	Revisions []FileRevision `json:"revisions"`
	// Head is the commit the walk started from. It is what invalidates a cached
	// result: a different HEAD is a different history.
	Head string `json:"head"`
	// Commits counts the commits that touched the requested paths.
	Commits int `json:"commits"`
	// Truncated reports a walk stopped by Limit or by Since before it reached
	// the beginning of the history. The metrics then report the days it cannot
	// cover as unknown instead of as an empty backlog.
	Truncated bool `json:"truncated"`
	// Oldest is the instant of the earliest revision read.
	Oldest time.Time `json:"oldest,omitempty"`
}

// finish sorts the revisions and fills in the derived counters.
func (h *FileHistory) finish() {
	sort.SliceStable(h.Revisions, func(i, j int) bool {
		if h.Revisions[i].When.Equal(h.Revisions[j].When) {
			return h.Revisions[i].Path < h.Revisions[j].Path
		}
		return h.Revisions[i].When.Before(h.Revisions[j].When)
	})
	if len(h.Revisions) > 0 {
		h.Oldest = h.Revisions[0].When
	}
}

// HistoryCache memoizes a history walk against the commit it was read at. It is
// derived data, never storage: a HEAD the cache does not hold is simply read
// again, and dropping the whole cache costs one walk.
//
// This is the "results are cached and invalidated when new commits arrive" of
// GIT-US-0028: a commit moves HEAD, a moved HEAD misses the cache.
type HistoryCache struct {
	mu      sync.Mutex
	entries map[string]FileHistory
	head    string
	// Hits and Misses are counters for the tests and for `gintrack doctor`.
	Hits   int
	Misses int
}

// NewHistoryCache returns an empty cache.
func NewHistoryCache() *HistoryCache { return &HistoryCache{entries: map[string]FileHistory{}} }

// Do returns the cached history for req at head, calling read on a miss. A head
// different from the cached one empties the cache first.
func (c *HistoryCache) Do(
	head string, req HistoryRequest, read func() (FileHistory, error),
) (FileHistory, error) {
	req = req.normalized()
	c.mu.Lock()
	if head == "" || head != c.head {
		c.entries = map[string]FileHistory{}
		c.head = head
	}
	if cached, ok := c.entries[req.key()]; ok {
		c.Hits++
		c.mu.Unlock()
		return cached, nil
	}
	c.Misses++
	c.mu.Unlock()

	history, err := read()
	if err != nil {
		return FileHistory{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if head != "" && head == c.head {
		c.entries[req.key()] = history
	}
	return history, nil
}
