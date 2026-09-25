package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// The verification cache (ADR-037 section 7, docs/03 R-REQ-11 and R-LOC-5,
// GIT-US-0141): one entry per requirement per recorded run — the block rev
// that was tested, the commit, the linked tests with their results, the
// aggregate result, the time and who ran it. It is derived, local data that
// is never committed, never synced and never the source of truth: deleting
// it loses only evidence that was never promoted to a `verified:` stamp, and
// a missing or corrupt cache reads as empty and is rebuilt by the next run.
//
// Natively the cache is <docs>/.pmngr/verify.json beside index.json
// (FileVerifyCache); the browser keeps the same versioned document as one
// record in IndexedDB (MemVerifyCache, which the host persists with Export
// and hydrates with NewMemVerifyCache). Both sit behind VerifyCache, and
// everything here is pure data handling over an injected FS, so it compiles
// to WebAssembly unchanged (ADR-003).

// VerifyCacheFileName is the cache file inside a backlog folder.
const VerifyCacheFileName = "verify.json"

// VerifyCacheVersion is the format version of the cache document. A document
// of any other version is discarded and rebuilt.
const VerifyCacheVersion = 1

// verifyRevsPerRef bounds the history kept per requirement: the newest entry
// of each of its last few block revs, so a text that is edited and reverted
// still finds its evidence while the file cannot grow without bound.
const verifyRevsPerRef = 4

// VerifyResult is the aggregate result of a requirement's linked tests in
// one recorded run.
type VerifyResult string

// The recorded results. A run is `pass` only when every linked test passed;
// `partial` (some passed, none failed, some have no result) is recorded so
// coverage can say so, but it is never evidence a stamp may copy.
const (
	VerifyPass    VerifyResult = "pass"
	VerifyFail    VerifyResult = "fail"
	VerifyPartial VerifyResult = "partial"
)

// Valid reports whether r is one of the recorded results.
func (r VerifyResult) Valid() bool {
	switch r {
	case VerifyPass, VerifyFail, VerifyPartial:
		return true
	}
	return false
}

// VerifyTest is one linked test of a requirement with its result in the run:
// pass, fail, skip, or missing when no result matched it.
type VerifyTest struct {
	Test   string `json:"test"`
	Result string `json:"result"`
}

// VerifyEntry is one requirement's recorded run.
type VerifyEntry struct {
	Ref RequirementRef `json:"ref"`
	// Rev is the block rev of the requirement when the run was recorded
	// (R-REQ-REV-1): the text the tests verified.
	Rev Rev `json:"rev"`
	// Commit is the full hex id the linked tests ran at, when they all ran
	// at one commit.
	Commit string `json:"commit,omitempty"`
	// Commits lists the distinct commits, sorted, when the results come from
	// several runs; Commit is then empty.
	Commits []string `json:"commits,omitempty"`
	// Uncommitted is set when some result records no commit.
	Uncommitted bool         `json:"uncommitted,omitempty"`
	Tests       []VerifyTest `json:"tests"`
	Result      VerifyResult `json:"result"`
	// At is when the run was recorded: the newest result's time, UTC,
	// truncated to the second.
	At time.Time `json:"at"`
	// By is the handle that ran the tests; empty when unknown.
	By string `json:"by,omitempty"`
}

// AllCommits returns the commits of the entry: Commit, else Commits.
func (e VerifyEntry) AllCommits() []string {
	if e.Commit != "" {
		return []string{e.Commit}
	}
	return append([]string(nil), e.Commits...)
}

// verifyCacheDoc is the document stored in verify.json and in IndexedDB.
type verifyCacheDoc struct {
	Version int           `json:"version"`
	Entries []VerifyEntry `json:"entries"`
}

// VerifyRecordStats reports what recording a run changed.
type VerifyRecordStats struct {
	Added    int `json:"added"`
	Replaced int `json:"replaced"`
	Total    int `json:"total"`
	// Rebuilt is set when the previous document was corrupt, or of another
	// version, and was discarded.
	Rebuilt bool `json:"rebuilt,omitempty"`
}

// VerifyCache is where the verification cache of one project lives.
type VerifyCache interface {
	// Load returns every entry. A missing cache is empty; a corrupt one is
	// empty too, reported by corrupt, and never an error: it is derived data.
	Load() (entries []VerifyEntry, corrupt bool, err error)
	// Record merges the entries of a run: each replaces the entry of the same
	// requirement and block rev.
	Record(fresh []VerifyEntry) (VerifyRecordStats, error)
}

// DecodeVerifyCache parses a cache document. Anything that is not a document
// of VerifyCacheVersion is corrupt and reads as empty; an entry without a
// requirement ref, a block rev or a known result is dropped.
func DecodeVerifyCache(data []byte) (entries []VerifyEntry, corrupt bool) {
	var doc verifyCacheDoc
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != VerifyCacheVersion {
		return []VerifyEntry{}, true
	}
	out := make([]VerifyEntry, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		if e.Ref.Spec == "" || e.Ref.Number <= 0 || e.Rev == "" || !e.Result.Valid() {
			continue
		}
		out = append(out, e)
	}
	sortVerifyEntries(out)
	return out, false
}

// EncodeVerifyCache renders the cache document, entries sorted by ref and
// then newest first.
func EncodeVerifyCache(entries []VerifyEntry) ([]byte, error) {
	sorted := append([]VerifyEntry(nil), entries...)
	sortVerifyEntries(sorted)
	if sorted == nil {
		sorted = []VerifyEntry{}
	}
	data, err := json.MarshalIndent(verifyCacheDoc{Version: VerifyCacheVersion, Entries: sorted}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the verification cache: %w", err)
	}
	return append(data, '\n'), nil
}

// MergeVerifyEntries records fresh entries over cur: each replaces the entry
// of the same requirement and block rev, whatever its result — the cache
// keeps the last run, not the best. Per requirement only the newest entries
// of its last few block revs are kept.
func MergeVerifyEntries(cur, fresh []VerifyEntry) ([]VerifyEntry, VerifyRecordStats) {
	type key struct {
		ref RequirementRef
		rev Rev
	}
	var st VerifyRecordStats
	out := append([]VerifyEntry(nil), cur...)
	at := make(map[key]int, len(out))
	for i, e := range out {
		at[key{e.Ref, e.Rev}] = i
	}
	for _, e := range fresh {
		e.At = e.At.UTC().Truncate(time.Second)
		k := key{e.Ref, e.Rev}
		if i, ok := at[k]; ok {
			out[i] = e
			st.Replaced++
			continue
		}
		at[k] = len(out)
		out = append(out, e)
		st.Added++
	}
	sortVerifyEntries(out)
	kept := out[:0]
	perRef := map[RequirementRef]int{}
	for _, e := range out {
		if perRef[e.Ref] >= verifyRevsPerRef {
			continue
		}
		perRef[e.Ref]++
		kept = append(kept, e)
	}
	st.Total = len(kept)
	return kept, st
}

// LatestVerifyEntry returns the newest entry of ref whose block rev is rev,
// or the newest entry of ref at any rev when rev is empty.
func LatestVerifyEntry(entries []VerifyEntry, ref RequirementRef, rev Rev) (VerifyEntry, bool) {
	var best VerifyEntry
	found := false
	for _, e := range entries {
		if e.Ref != ref || (rev != "" && e.Rev != rev) {
			continue
		}
		if !found || e.At.After(best.At) {
			best, found = e, true
		}
	}
	return best, found
}

// sortVerifyEntries orders entries by spec, number, then newest first.
func sortVerifyEntries(es []VerifyEntry) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Ref.Spec != b.Ref.Spec {
			return a.Ref.Spec < b.Ref.Spec
		}
		if a.Ref.Number != b.Ref.Number {
			return a.Ref.Number < b.Ref.Number
		}
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		return a.Rev < b.Rev
	})
}

// VerifyCachePath is the cache file of the project whose backlog (or docs
// folder) is projectDir: <docs>/.pmngr/verify.json.
func VerifyCachePath(projectDir string) string {
	return joinPath(BacklogDir(projectDir), VerifyCacheFileName)
}

// FileVerifyCache is the verification cache as a file of an FS: the native
// store, <docs>/.pmngr/verify.json, git-ignored by the snippet of R-LOC-5.
type FileVerifyCache struct {
	fs   FS
	path string
}

// NewFileVerifyCache returns the cache of the project whose backlog (or docs
// folder) is projectDir.
func NewFileVerifyCache(fs FS, projectDir string) *FileVerifyCache {
	return &FileVerifyCache{fs: fs, path: VerifyCachePath(projectDir)}
}

// Path is the cache file.
func (c *FileVerifyCache) Path() string { return c.path }

// Load reads the cache; see VerifyCache.
func (c *FileVerifyCache) Load() ([]VerifyEntry, bool, error) {
	data, err := c.fs.ReadFile(c.path)
	if errors.Is(err, ErrNotExist) {
		return []VerifyEntry{}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", c.path, err)
	}
	entries, corrupt := DecodeVerifyCache(data)
	return entries, corrupt, nil
}

// Record merges fresh entries and replaces the file: written beside it first
// and renamed over it, so a reader never sees half a document.
func (c *FileVerifyCache) Record(fresh []VerifyEntry) (VerifyRecordStats, error) {
	cur, corrupt, err := c.Load()
	if err != nil {
		return VerifyRecordStats{}, err
	}
	merged, st := MergeVerifyEntries(cur, fresh)
	st.Rebuilt = corrupt
	data, err := EncodeVerifyCache(merged)
	if err != nil {
		return VerifyRecordStats{}, err
	}
	tmp := c.path + ".tmp"
	if err := c.fs.WriteFile(tmp, data); err != nil {
		return VerifyRecordStats{}, fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := c.fs.Rename(tmp, c.path); err != nil {
		_ = c.fs.Remove(tmp)
		return VerifyRecordStats{}, fmt.Errorf("replace %s: %w", c.path, err)
	}
	return st, nil
}

// MemVerifyCache is the verification cache held in memory as its encoded
// document: the browser store, which the host persists as one IndexedDB
// record (Export) beside the index snapshot and hands back on reopen
// (NewMemVerifyCache). Browser-only mode cannot run tests, so it is normally
// empty.
type MemVerifyCache struct {
	mu   sync.Mutex
	data []byte
}

// NewMemVerifyCache returns a cache holding a previously exported document;
// nil or corrupt data is an empty cache.
func NewMemVerifyCache(data []byte) *MemVerifyCache {
	return &MemVerifyCache{data: append([]byte(nil), data...)}
}

// Load decodes the held document; see VerifyCache.
func (c *MemVerifyCache) Load() ([]VerifyEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.data) == 0 {
		return []VerifyEntry{}, false, nil
	}
	entries, corrupt := DecodeVerifyCache(c.data)
	return entries, corrupt, nil
}

// Record merges fresh entries into the held document.
func (c *MemVerifyCache) Record(fresh []VerifyEntry) (VerifyRecordStats, error) {
	cur, corrupt, _ := c.Load()
	merged, st := MergeVerifyEntries(cur, fresh)
	st.Rebuilt = corrupt
	data, err := EncodeVerifyCache(merged)
	if err != nil {
		return VerifyRecordStats{}, err
	}
	c.mu.Lock()
	c.data = data
	c.mu.Unlock()
	return st, nil
}

// Export returns the held document, for the host to persist.
func (c *MemVerifyCache) Export() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.data...)
}

// Ensure both stores satisfy the interface at compile time.
var (
	_ VerifyCache = (*FileVerifyCache)(nil)
	_ VerifyCache = (*MemVerifyCache)(nil)
)
