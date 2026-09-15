package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/pandosync"
)

// corpusItemPath is where the exported document of one item lands under a
// corpus root: <root>/<repo id>/<PROJECT>/items/<ID>.md.
func corpusItemPath(root, id string) string {
	return filepath.Join(root, testRepoID, "DEMO", "items", id+".md")
}

// waitForFile polls until want reports true of the file's contents, which is
// how an assertion about a background export stays free of a fixed sleep.
func waitForFile(t *testing.T, path string, want func(string, error) bool) bool {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path) //nolint:gosec // the path is built by the test
		if want(string(data), err) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func exists(_ string, err error) bool { return err == nil }

func TestCorpusExportsOnStartWithoutBlockingTheListener(t *testing.T) {
	t.Parallel()

	corpus := t.TempDir()
	s, _ := newPandoSearchServer(t, searchServerOptions{
		corpusDir: corpus,
		port:      freeLoopbackPort(t),
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	// The listener answers while the export is still running: the request goes
	// over a real socket, not through the router, so it proves the goroutine
	// never delayed Serve.
	deadline := time.Now().Add(5 * time.Second)
	var answered bool
	for time.Now().Before(deadline) && !answered {
		resp, err := http.Get(s.URL() + "/api/v1/health") //nolint:noctx // short-lived probe in a test
		if err == nil {
			answered = resp.StatusCode == http.StatusOK
			_ = resp.Body.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !answered {
		t.Fatal("the server never answered while the corpus was being exported")
	}

	if !waitForFile(t, corpusItemPath(corpus, "DEMO-US-0001"), exists) {
		t.Fatal("the startup export never wrote the corpus")
	}

	// A second export over an unchanged corpus writes nothing.
	before := statOf(t, corpusItemPath(corpus, "DEMO-US-0001"))
	exp, ok := s.search.exporterFor(testRepoID)
	if !ok {
		t.Fatal("the repository has no exporter")
	}
	stats, err := exp.ExportAll(t.Context())
	if err != nil {
		t.Fatalf("second export: %v", err)
	}
	if stats.Written != 0 {
		t.Errorf("a second export wrote %d files over an unchanged corpus", stats.Written)
	}
	if after := statOf(t, corpusItemPath(corpus, "DEMO-US-0001")); !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged document was rewritten, which defeats Pando's mtime skip")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start(): %v", err)
	}
}

func statOf(t *testing.T, path string) os.FileInfo {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}

func TestCorpusFollowsHubEvents(t *testing.T) {
	t.Parallel()

	corpus := t.TempDir()
	s, root := newPandoSearchServer(t, searchServerOptions{corpusDir: corpus})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.startCorpusSync(ctx)
	if !waitForFile(t, corpusItemPath(corpus, "DEMO-US-0001"), exists) {
		t.Fatal("the first export never ran")
	}

	// An edit through the API publishes item.changed, which the subscriber
	// turns into exactly one corpus write.
	rev, _ := getItem(t, s, "DEMO-US-0001")
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/items/DEMO-US-0001",
		header: map[string]string{"If-Match": rev},
		body:   map[string]any{"set": map[string]any{"title": "Guest checkout, revisited"}},
	}), http.StatusOK, nil)

	if !waitForFile(t, corpusItemPath(corpus, "DEMO-US-0001"), func(data string, err error) bool {
		return err == nil && strings.Contains(data, "Guest checkout, revisited")
	}) {
		t.Fatal("the corpus never picked the edit up")
	}

	// A deletion removes the document rather than leaving a dangling one.
	rev, _ = getItem(t, s, "DEMO-T-0001")
	decode(t, send(t, s, request{
		method: http.MethodDelete,
		target: "/api/v1/items/DEMO-T-0001",
		header: map[string]string{"If-Match": rev},
	}), http.StatusNoContent, nil)

	if !waitForFile(t, corpusItemPath(corpus, "DEMO-T-0001"), func(_ string, err error) bool {
		return os.IsNotExist(err)
	}) {
		t.Fatal("the corpus kept the document of a deleted item")
	}
	_ = root
}

func TestCorpusReExportsAfterAnOverflow(t *testing.T) {
	t.Parallel()

	corpus := t.TempDir()
	s, _ := newPandoSearchServer(t, searchServerOptions{corpusDir: corpus})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	exp, ok := s.search.exporterFor(testRepoID)
	if !ok {
		t.Fatal("the repository has no exporter")
	}
	if _, err := exp.ExportAll(ctx); err != nil {
		t.Fatalf("first export: %v", err)
	}

	// Delete a corpus file behind the exporter's back, the way a lost event
	// leaves the corpus wrong, then declare the overflow.
	target := corpusItemPath(corpus, "DEMO-US-0001")
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove the corpus file: %v", err)
	}
	s.search.applyToAll(ctx, pandosync.Event{Kind: pandosync.Overflow})
	if !waitForFile(t, target, exists) {
		t.Fatal("an overflow did not trigger a full re-export")
	}
}

func TestCorpusChangeOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		event Event
		want  pandosync.Event
		repo  string
		ok    bool
	}{
		{
			"an edited item",
			Event{Type: eventItemChanged, Data: itemChangedData{Repo: "demo", ID: "DEMO-T-0001", Op: "updated"}},
			pandosync.Event{Kind: pandosync.ItemChanged, ID: "DEMO-T-0001"}, "demo", true,
		},
		{
			"a deleted item",
			Event{Type: eventItemChanged, Data: itemChangedData{Repo: "demo", ID: "DEMO-T-0001", Op: "deleted"}},
			pandosync.Event{Kind: pandosync.ItemRemoved, ID: "DEMO-T-0001"}, "demo", true,
		},
		{
			"an edited page",
			Event{Type: eventFileChanged, Data: fileChangedData{Repo: "demo", Path: "docs/index.md", Op: "write", IsKb: true}},
			pandosync.Event{Kind: pandosync.PageChanged, Path: "docs/index.md"}, "demo", true,
		},
		{
			"a removed page",
			Event{Type: eventFileChanged, Data: fileChangedData{Repo: "demo", Path: "docs/index.md", Op: "remove", IsKb: true}},
			pandosync.Event{Kind: pandosync.PageRemoved, Path: "docs/index.md"}, "demo", true,
		},
		{
			"a backlog file is not a page",
			Event{Type: eventFileChanged, Data: fileChangedData{Repo: "demo", Path: "docs/.pmngr/x.md", Op: "write"}},
			pandosync.Event{}, "", false,
		},
		{
			"an event the corpus does not mirror",
			Event{Type: eventGitCommit, Data: map[string]any{"repo": "demo"}},
			pandosync.Event{}, "", false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := corpusChangeOf(tc.event)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if got.repo != tc.repo || got.event != tc.want {
				t.Fatalf("got %+v/%+v, want %s/%+v", got.repo, got.event, tc.repo, tc.want)
			}
		})
	}
}
