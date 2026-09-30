package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/impact"
	"github.com/digiogithub/git-in-track/internal/pando"
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

// inflightSuffix names the in-flight marker next to the answer file.
const inflightSuffix = ".inflight"

// inflightRecord is the marker's content (GIT-US-0190): who started the check
// of which run of the instance, and when. It is a lock and a hint, never an
// answer.
type inflightRecord struct {
	Generation string    `json:"generation"`
	Project    string    `json:"project"`
	PID        int       `json:"pid"`
	Started    time.Time `json:"started"`
}

// liveMarker reads the marker and reports whether it is fresh: it belongs to
// this instance run and started less than stale ago. A missing, corrupt or
// foreign marker is not live.
func (s fileGraphStore) liveMarker(now time.Time, stale time.Duration) bool {
	b, err := os.ReadFile(s.path + inflightSuffix)
	if err != nil {
		return false
	}
	var rec inflightRecord
	if json.Unmarshal(b, &rec) != nil || rec.Generation != s.generation || rec.Project != s.project {
		return false
	}
	return now.Before(rec.Started.Add(stale))
}

// Claimed reports whether a live check marker exists.
func (s fileGraphStore) Claimed(now time.Time, stale time.Duration) bool {
	return s.liveMarker(now, stale)
}

// Claim creates the marker exclusively, so of several processes that start a
// check at once only one wins. A stale or foreign marker is removed first.
// The marker is removed by release; a process killed before that leaves it to
// go stale.
func (s fileGraphStore) Claim(now time.Time, stale time.Duration) (func(), bool) {
	marker := s.path + inflightSuffix
	if s.liveMarker(now, stale) {
		return nil, false
	}
	_ = os.Remove(marker)
	b, err := json.Marshal(inflightRecord{Generation: s.generation, Project: s.project, PID: os.Getpid(), Started: now})
	if err != nil {
		return func() {}, true
	}
	f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, false // another process won the race
		}
		return func() {}, true // the cache directory is unwritable: run unguarded
	}
	_, _ = f.Write(b)
	_ = f.Close()
	return func() { _ = os.Remove(marker) }, true
}

// externalGraphStore is the store of an external (unmanaged) Pando. There is no
// instance generation to key the answer by, so the key is the endpoint URL and
// the project id, and only the record's expiry bounds it (GIT-US-0188). Each key
// has its own file under <cacheDir>/pando/external; the endpoint is hashed
// so a URL with credentials never reaches the disk. Nil when the directory
// cannot be created.
func externalGraphStore(cacheDir, endpoint, project string) impact.GraphStore {
	dir := filepath.Join(cacheDir, "pando", "external")
	if os.MkdirAll(dir, 0o755) != nil {
		return nil
	}
	sum := sha256.Sum256([]byte(endpoint + "\x00" + project))
	name := hex.EncodeToString(sum[:8])
	return fileGraphStore{
		path:       filepath.Join(dir, "graph-probe-"+name+".json"),
		generation: hex.EncodeToString(sum[:]),
		project:    project,
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

// warmSampleSize is how many files the warm-up may ask about; the check stops
// at the first coupled one.
const warmSampleSize = 5

// warmGraph runs the graph check once the code project is registered, so the
// first query after startup finds a known answer. The sample is a few source
// files chosen for likely call edges ([sampleSourceFiles]); a found graph and
// a sample with no coupled file are both stored (GIT-US-0190).
func (ms *managedState) warmGraph(ctx context.Context, slot *managedRepo, client pandoAPI) {
	g, ok := client.(impact.CallGraph)
	if !ok {
		return
	}
	// An empty answer while Pando is still indexing is no evidence of a
	// missing graph, so the check starts once the project is indexed.
	if !waitIndexed(ctx, client, slot.project, warmIndexPoll, warmIndexWait) {
		return
	}
	store := ms.storeOf(slot)
	if store == nil {
		return
	}
	impact.WarmGraph(ctx, g, slot.project, sampleSourceFiles(slot.root, warmSampleSize), store)
}

const (
	// warmIndexPoll is how often the warm-up asks whether indexing is done.
	warmIndexPoll = 5 * time.Second
	// warmIndexWait bounds the wait for the code index to complete.
	warmIndexWait = 45 * time.Minute
)

// projectLister is the part of [pandoAPI] waitIndexed reads.
type projectLister interface {
	ListProjects(ctx context.Context) ([]pando.Project, error)
}

// waitIndexed polls Pando until the project reports its indexing completed. It
// gives up (false) when ctx ends, the wait runs out or Pando reports a failed
// index; a Pando that cannot answer is asked again.
func waitIndexed(ctx context.Context, c projectLister, project string, poll, limit time.Duration) bool {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		projects, err := c.ListProjects(ctx)
		if err == nil {
			for _, p := range projects {
				if p.ProjectID != project {
					continue
				}
				switch strings.ToLower(p.IndexingStatus) {
				case "completed", "complete", "done", "indexed":
					return true
				case "failed", "error":
					return false
				}
			}
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-tick.C:
		}
	}
}
