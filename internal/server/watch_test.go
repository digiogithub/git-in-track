package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/watcher"
)

// fakeWatcher is a scripted watcher: the test decides which batches the server
// sees and when.
type fakeWatcher struct {
	events chan []watcher.Event
	errs   chan error
	repos  []string
	closed bool
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{
		events: make(chan []watcher.Event, 4),
		errs:   make(chan error, 4),
	}
}

func (f *fakeWatcher) AddRepo(key, _ string) error {
	f.repos = append(f.repos, key)
	return nil
}

func (f *fakeWatcher) Events() <-chan []watcher.Event { return f.events }
func (f *fakeWatcher) Errors() <-chan error           { return f.errs }

func (f *fakeWatcher) Close() error {
	if !f.closed {
		f.closed = true
		close(f.events)
	}
	return nil
}

// watchingServer mounts the fixture with the watcher enabled and returns the
// server, the live HTTP endpoint and the directory on disk.
func watchingServer(t *testing.T, factory WatcherFactory) (*Server, *httptest.Server, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Token:      "test-token",
		Workspace:  "test",
		Repos:      []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Watch:      true,
		Debounce:   40 * time.Millisecond,
		NewWatcher: factory,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	httpSrv := httptest.NewServer(s.Handler())
	t.Cleanup(httpSrv.Close)
	return s, httpSrv, root
}

func TestWatcherBatchesReachTheEventStream(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	fake := newFakeWatcher()
	s, httpSrv, root := watchingServer(t, func(watcher.Options) (FileWatcher, error) { return fake, nil })
	s.startWatch(ctx)
	t.Cleanup(s.stopWatch)

	if !s.watching() {
		t.Fatal("the server does not report live updates")
	}
	if len(fake.repos) != 1 || fake.repos[0] != testRepoID {
		t.Fatalf("watched repos = %v", fake.repos)
	}

	conn := dialEvents(ctx, t, s, httpSrv, "")

	// An editor writes the file directly, the way a human or an agent does.
	rel := "docs/.pmngr/tasks/DEMO-T-0001-add-address-validation.md"
	onDisk := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read the fixture task: %v", err)
	}
	edited := strings.Replace(string(data), "title: Add address validation", "title: Add address validation twice", 1)
	if err := os.WriteFile(onDisk, []byte(edited), 0o644); err != nil {
		t.Fatalf("write the task: %v", err)
	}
	fake.events <- []watcher.Event{{Repo: testRepoID, Path: rel, Op: watcher.Write, Time: time.Now()}}

	file := awaitFrame(ctx, t, conn, eventFileChanged)
	if file.Data == nil {
		t.Fatal("file.changed carries no payload")
	}
	changed := awaitFrame(ctx, t, conn, eventItemChanged)
	if changed.Seq <= file.Seq {
		t.Errorf("sequence numbers are not monotonic: %d then %d", file.Seq, changed.Seq)
	}
	awaitFrame(ctx, t, conn, eventIndexUpdated)

	// The index now serves the edited title, without a reindex request.
	var item struct {
		Title string `json:"title"`
	}
	decode(t, send(t, s, request{method: "GET", target: "/api/v1/items/DEMO-T-0001"}), 200, &item)
	if item.Title != "Add address validation twice" {
		t.Errorf("title = %q, want the edit the watcher folded in", item.Title)
	}
}

func TestWatcherFailureDegradesToNoLiveUpdates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	s, _, _ := watchingServer(t, func(watcher.Options) (FileWatcher, error) {
		return nil, errors.New("inotify is exhausted")
	})
	s.startWatch(ctx)
	t.Cleanup(s.stopWatch)

	if s.watching() {
		t.Error("the server claims live updates after the watcher failed to start")
	}
	// The API is unaffected.
	decode(t, send(t, s, request{method: "GET", target: "/api/v1/items"}), 200, nil)

	var caps struct {
		Features struct {
			Watcher bool `json:"watcher"`
			Write   bool `json:"write"`
		} `json:"features"`
	}
	decode(t, send(t, s, request{method: "GET", target: "/api/v1/capabilities"}), 200, &caps)
	if caps.Features.Watcher {
		t.Error("capabilities advertise a watcher that is not running")
	}
	if !caps.Features.Write {
		t.Error("capabilities must advertise writes")
	}
}

func TestWatcherObservesARealDiskWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("the real file watcher needs a few hundred milliseconds")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s, httpSrv, root := watchingServer(t, nil)
	s.startWatch(ctx)
	t.Cleanup(s.stopWatch)
	if !s.watching() {
		t.Fatal("the file watcher did not start")
	}

	conn := dialEvents(ctx, t, s, httpSrv, "")

	page := filepath.Join(root, "docs", "architecture", "overview.md")
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("read the fixture page: %v", err)
	}
	if err := os.WriteFile(page, append(data, []byte("\n<!-- edited on disk -->\n")...), 0o644); err != nil {
		t.Fatalf("write the page: %v", err)
	}

	file := awaitFrame(ctx, t, conn, eventFileChanged)
	var payload struct {
		Repo string `json:"repo"`
		Path string `json:"path"`
		Op   string `json:"op"`
		IsKb bool   `json:"isKb"`
	}
	if err := json.Unmarshal(file.Data, &payload); err != nil {
		t.Fatalf("decode the payload: %v", err)
	}
	if payload.Repo != testRepoID || payload.Path != "docs/architecture/overview.md" {
		t.Errorf("payload = %+v", payload)
	}
	if !payload.IsKb {
		t.Errorf("payload = %+v, want a knowledge-base file", payload)
	}
	awaitFrame(ctx, t, conn, eventIndexUpdated)
}
