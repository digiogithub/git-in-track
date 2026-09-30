package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/impact"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

// graphStoreFile is the name of the persisted graph check inside an instance
// directory. It is derived cache data (GIT-US-0179): deleting it only costs
// the next tier 2 query a rerun of the check.
const graphStoreFile = "graph-probe.json"

// graphRecord is the file's content.
type graphRecord struct {
	// Generation names the run of the instance the answer was measured on; a
	// restart changes it and the answer is ignored.
	Generation string    `json:"generation"`
	Project    string    `json:"project"`
	Edges      bool      `json:"edges"`
	Expires    time.Time `json:"expires"`
}

// fileGraphStore is an [impact.GraphStore] over one file.
type fileGraphStore struct {
	path, generation, project string
}

func (s fileGraphStore) Load() (bool, time.Time, bool) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return false, time.Time{}, false
	}
	var rec graphRecord
	if json.Unmarshal(b, &rec) != nil || rec.Generation != s.generation || rec.Project != s.project {
		return false, time.Time{}, false
	}
	return rec.Edges, rec.Expires, true
}

// Store writes through a temporary file and a rename, so a reader or a
// concurrent writer never sees a half-written file. Errors are dropped: the
// file is only a cache.
func (s fileGraphStore) Store(edges bool, expires time.Time) {
	b, err := json.Marshal(graphRecord{Generation: s.generation, Project: s.project, Edges: edges, Expires: expires})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), graphStoreFile+".*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), s.path) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// generationOf names the run of an instance: its pid and the moment it last
// changed state. Empty while there is no child to measure.
func generationOf(st supervisor.Status) string {
	if st.State != supervisor.StateReady || st.PID <= 0 {
		return ""
	}
	return strconv.Itoa(st.PID) + "@" + strconv.FormatInt(st.Since.UnixNano(), 10)
}

// graphStore returns the store maker of one repository's instance for
// [impact.Options.GraphStore]; nil when the repository has no instance. The
// maker reads the instance's generation at call time and answers nil while it
// is not ready, so an answer measured on another run is never reused.
func (ms *managedState) graphStore(repo string) func(impact.CallGraph) impact.GraphStore {
	slot := ms.slot(repo)
	if slot == nil {
		return nil
	}
	return func(impact.CallGraph) impact.GraphStore {
		return ms.storeOf(slot)
	}
}

func (ms *managedState) storeOf(slot *managedRepo) impact.GraphStore {
	if ms.cacheDir == "" {
		return nil
	}
	gen := generationOf(slot.status())
	if gen == "" {
		return nil
	}
	dir := supervisor.InstanceDir(ms.cacheDir, supervisor.InstanceKey(slot.root))
	if os.MkdirAll(dir, 0o755) != nil {
		return nil
	}
	return fileGraphStore{path: filepath.Join(dir, graphStoreFile), generation: gen, project: slot.project}
}

// warmGraph runs the graph check once the code project is registered, so the
// first query after startup finds a known answer. The sample is a few source
// files of the tree; only a found graph is stored.
func (ms *managedState) warmGraph(ctx context.Context, slot *managedRepo, client pandoAPI) {
	g, ok := client.(impact.CallGraph)
	if !ok {
		return
	}
	store := ms.storeOf(slot)
	if store == nil {
		return
	}
	impact.WarmGraph(ctx, g, slot.project, sampleSourceFiles(slot.root, 5), store)
}

// state can be told apart.
var sourceExts = map[string]bool{".go": true, ".ts": true, ".tsx": true, ".js": true, ".py": true, ".rs": true, ".java": true}

// sampleSourceFiles lists up to n source files of a tree, in path order,
// skipping hidden directories and vendored or generated trees.
func sampleSourceFiles(root string, n int) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is skipped, the sample is best effort
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if sourceExts[filepath.Ext(name)] && !strings.HasSuffix(name, "_test.go") {
			if rel, e := filepath.Rel(root, p); e == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		if len(out) >= n {
			return fs.SkipAll
		}
		return nil
	})
	return out
}
