package trace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// resultCacheVersion is the format version of a test-result cache file. A
// file of any other version is discarded and rebuilt.
const resultCacheVersion = 1

// ResultStore is the test-result cache of one repository: the last result of
// every test ever ingested for it on this machine. It is derived data — a
// JSON file outside the repository, rebuilt by ingesting the reports again —
// and never the source of truth: deleting it loses nothing the repository
// records. A missing or corrupt file reads as empty.
type ResultStore struct {
	path string
}

// resultCacheFile is the on-disk shape of the cache.
type resultCacheFile struct {
	Version int          `json:"version"`
	Root    string       `json:"root,omitempty"`
	Results []TestResult `json:"results"`
}

// DefaultResultCachePath is where the test-result cache of the repository at
// root lives: <cacheDir>/test-results/<hash of the absolute root>.json, next
// to the index cache of the same machine (docs/07 section 4.19).
func DefaultResultCachePath(cacheDir, root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(abs)))
	return filepath.Join(cacheDir, "test-results", hex.EncodeToString(sum[:8])+".json")
}

// NewResultStore returns the store backed by the file at path.
func NewResultStore(path string) *ResultStore { return &ResultStore{path: path} }

// Path is the cache file.
func (s *ResultStore) Path() string { return s.path }

// Load returns every cached result, sorted by trace ref. A missing file is an
// empty cache; a corrupt one, or one of another version, is an empty cache
// too, reported by corrupt so the caller can say it is being rebuilt.
func (s *ResultStore) Load() (results []TestResult, corrupt bool, err error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return []TestResult{}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read the test-result cache: %w", err)
	}
	var f resultCacheFile
	if uerr := json.Unmarshal(data, &f); uerr != nil || f.Version != resultCacheVersion {
		// A corrupt cache is rebuilt, never fatal: it is derived data.
		return []TestResult{}, true, nil //nolint:nilerr // corrupt is the report
	}
	if f.Results == nil {
		f.Results = []TestResult{}
	}
	sortResults(f.Results)
	return f.Results, false, nil
}

// MergeStats reports what a Merge changed.
type MergeStats struct {
	Added    int `json:"added"`
	Replaced int `json:"replaced"`
	Total    int `json:"total"`
	// Rebuilt is set when the previous file was corrupt and was discarded.
	Rebuilt bool `json:"rebuilt,omitempty"`
}

// Merge records fresh results: each replaces the cached result of the same
// test (the same trace ref, or the same format and id for a result mapped
// to no file), whatever its outcome — the cache keeps the last result, not
// the best. The file is replaced atomically.
func (s *ResultStore) Merge(root string, fresh []TestResult) (MergeStats, error) {
	cur, corrupt, err := s.Load()
	if err != nil {
		return MergeStats{}, err
	}
	st := MergeStats{Rebuilt: corrupt}
	byKey := make(map[string]int, len(cur))
	for i, r := range cur {
		byKey[r.key()] = i
	}
	for _, r := range fresh {
		if i, ok := byKey[r.key()]; ok {
			cur[i] = r
			st.Replaced++
			continue
		}
		byKey[r.key()] = len(cur)
		cur = append(cur, r)
		st.Added++
	}
	sortResults(cur)
	st.Total = len(cur)
	data, err := json.MarshalIndent(resultCacheFile{Version: resultCacheVersion, Root: root, Results: cur}, "", "  ")
	if err != nil {
		return MergeStats{}, fmt.Errorf("encode the test-result cache: %w", err)
	}
	if err := writeFileAtomic(s.path, append(data, '\n')); err != nil {
		return MergeStats{}, fmt.Errorf("write the test-result cache: %w", err)
	}
	return st, nil
}

func sortResults(rs []TestResult) {
	sort.Slice(rs, func(i, j int) bool {
		if a, b := rs[i].TraceRef(), rs[j].TraceRef(); a != b {
			return a < b
		}
		return rs[i].Format < rs[j].Format
	})
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".test-results-*")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// ---- ingest ----

// IngestReport is one parsed report file.
type IngestReport struct {
	File    string       `json:"file"`
	Format  ReportFormat `json:"format"`
	Tests   int          `json:"tests"`
	Passed  int          `json:"passed"`
	Failed  int          `json:"failed"`
	Skipped int          `json:"skipped"`
	// Unmapped counts the tests that map to no file of the working tree.
	Unmapped int `json:"unmapped"`
}

// Stamp resolves raw results with r and stamps them with the commit and the
// ingest time; it returns the results and the per-report counts.
func Stamp(r *TestResolver, raws []RawResult, commit string, at time.Time) ([]TestResult, IngestReport) {
	var rep IngestReport
	out := make([]TestResult, 0, len(raws))
	for _, raw := range raws {
		t := r.Resolve(raw)
		t.Commit, t.At = commit, at.UTC()
		out = append(out, t)
		rep.Tests++
		switch t.Result {
		case OutcomePass:
			rep.Passed++
		case OutcomeFail:
			rep.Failed++
		case OutcomeSkip:
			rep.Skipped++
		}
		if t.Path == "" {
			rep.Unmapped++
		}
	}
	return out, rep
}

// RepositoryGraph builds the trace graph of the repository at root from
// scratch: its backlog index, a full marker scan and the working tree. It
// returns nil and no error when the repository holds no backlog.
func RepositoryGraph(ctx context.Context, root string) (*Graph, error) {
	fsys, err := osfs.New(root)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", root, err)
	}
	projects, err := core.DiscoverProjects(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("discover the projects of %s: %w", root, err)
	}
	if len(projects) == 0 {
		return nil, nil
	}
	ix := core.NewIndex(fsys, projects)
	if _, err := ix.Build(ctx, true); err != nil {
		return nil, fmt.Errorf("index %s: %w", root, err)
	}
	c, err := Scan(ctx, root, Options{})
	if err != nil {
		return nil, err
	}
	return BuildGraph(ix, c.Markers(), os.DirFS(root))
}

// FindRoot returns the working-tree root enclosing dir: the nearest ancestor
// holding a .git or .jj entry, or dir itself when there is none.
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	for d := abs; ; {
		for _, marker := range []string{".git", ".jj"} {
			if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
				return d, nil
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return abs, nil
		}
		d = parent
	}
}
