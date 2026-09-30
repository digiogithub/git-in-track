package server

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/pando"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSampleSourceFiles(t *testing.T) {
	const mainFile = "package main\n\nfunc a() { x() }\nfunc b() { y() }\n"
	const lib = `package core

import "strings"

func Alpha() string { return strings.ToUpper(Beta()) }
func Beta() string  { return strings.TrimSpace(" b ") }
func Gamma()        { Alpha() }
`
	const small = "package util\n\nfunc Only() {}\n"
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"cmd/tool/add.go":             mainFile,
		"cmd/tool/agent.go":           mainFile,
		"internal/core/index.go":      lib,
		"internal/core/small.go":      small,
		"internal/util/util.go":       small,
		"internal/core/index_test.go": lib,
		"internal/gen/x.pb.go":        lib,
		"node_modules/dep/index.js":   "export function a() {}\n",
		"testdata/fixture.go":         lib,
		"web/src/api.ts":              "export function a() {}\nexport function b() {}\nexport const c = 1\n",
		"web/src/api.test.ts":         "export function a() {}\n",
		"docs/readme.md":              "# not source\n",
		".hidden/secret.go":           lib,
		"internal/broken/broken.go":   "package broken\nfunc (",
	})

	t.Run("library packages come before main package files", func(t *testing.T) {
		got := sampleSourceFiles(root, 3)
		if len(got) != 3 || got[0] != "internal/core/index.go" {
			t.Fatalf("sample = %v, want internal/core/index.go first", got)
		}
		for _, f := range got {
			if f == "cmd/tool/add.go" || f == "cmd/tool/agent.go" {
				t.Errorf("sample %v holds main-package file %s ahead of library files", got, f)
			}
		}
	})

	t.Run("one file per directory before a second of the same", func(t *testing.T) {
		got := sampleSourceFiles(root, 4)
		dirs := map[string]bool{}
		for _, f := range got {
			dirs[filepath.Dir(f)] = true
		}
		if len(dirs) != 4 {
			t.Errorf("sample %v spans %d directories, want 4", got, len(dirs))
		}
		if slices.Contains(got, "internal/core/small.go") {
			t.Errorf("sample %v took a second file of internal/core before other directories", got)
		}
	})

	t.Run("tests, generated code, vendored and hidden trees are skipped", func(t *testing.T) {
		for _, f := range sampleSourceFiles(root, 50) {
			switch f {
			case "internal/core/index_test.go", "internal/gen/x.pb.go", "node_modules/dep/index.js",
				"testdata/fixture.go", "web/src/api.test.ts", ".hidden/secret.go", "docs/readme.md":
				t.Errorf("sample holds %s", f)
			}
		}
	})

	t.Run("the sample is deterministic and bounded", func(t *testing.T) {
		a, b := sampleSourceFiles(root, 2), sampleSourceFiles(root, 2)
		if !slices.Equal(a, b) || len(a) != 2 {
			t.Errorf("samples %v and %v, want the same two files", a, b)
		}
		if got := sampleSourceFiles(root, 0); got != nil {
			t.Errorf("n=0 gave %v", got)
		}
	})
}

func TestFileGraphStoreInflight(t *testing.T) {
	newStore := func(dir, gen string) fileGraphStore {
		return fileGraphStore{path: filepath.Join(dir, graphStoreFile), generation: gen, project: "acme"}
	}
	now := time.Now()
	const stale = 10 * time.Minute

	t.Run("a second claim fails until the first is released", func(t *testing.T) {
		dir := t.TempDir()
		release, ok := newStore(dir, "1@1").Claim(now, stale)
		if !ok {
			t.Fatal("first claim refused")
		}
		other := newStore(dir, "1@1")
		if _, ok := other.Claim(now, stale); ok {
			t.Error("a second process claimed a live marker")
		}
		if !other.Claimed(now, stale) {
			t.Error("Claimed = false for a live marker")
		}
		release()
		if other.Claimed(now, stale) {
			t.Error("Claimed = true after release")
		}
		if _, ok := other.Claim(now, stale); !ok {
			t.Error("claim after release refused")
		}
	})

	t.Run("a stale marker is ignored and replaced", func(t *testing.T) {
		dir := t.TempDir()
		if _, ok := newStore(dir, "1@1").Claim(now.Add(-2*stale), stale); !ok {
			t.Fatal("seed claim refused")
		}
		s := newStore(dir, "1@1")
		if s.Claimed(now, stale) {
			t.Error("a stale marker is live")
		}
		if _, ok := s.Claim(now, stale); !ok {
			t.Error("a stale marker blocked the claim")
		}
		if !s.Claimed(now, stale) {
			t.Error("the new claim is not live")
		}
	})

	t.Run("a marker of another instance run does not block", func(t *testing.T) {
		dir := t.TempDir()
		if _, ok := newStore(dir, "1@1").Claim(now, stale); !ok {
			t.Fatal("seed claim refused")
		}
		if _, ok := newStore(dir, "2@9").Claim(now, stale); !ok {
			t.Error("a marker of a previous run blocked the claim")
		}
	})

	t.Run("a corrupt marker is not live", func(t *testing.T) {
		dir := t.TempDir()
		s := newStore(dir, "1@1")
		if err := os.WriteFile(s.path+inflightSuffix, []byte("{nope"), 0o600); err != nil {
			t.Fatal(err)
		}
		if s.Claimed(now, stale) {
			t.Error("a corrupt marker is live")
		}
		if _, ok := s.Claim(now, stale); !ok {
			t.Error("a corrupt marker blocked the claim")
		}
	})
}

func TestSemanticHostProbeContext(t *testing.T) {
	h := &SemanticHost{}
	ctx := h.ProbeContext()
	if ctx.Err() != nil {
		t.Fatal("the probe context starts cancelled")
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Error("Close did not cancel the probe context")
	}
	if (*SemanticHost)(nil).ProbeContext() == nil {
		t.Error("a nil host has no probe context")
	}
}

type fakeLister struct {
	statuses []string
	calls    int
}

func (f *fakeLister) ListProjects(context.Context) ([]pando.Project, error) {
	i := min(f.calls, len(f.statuses)-1)
	f.calls++
	if f.statuses[i] == "err" {
		return nil, pando.ErrUnreachable
	}
	return []pando.Project{{ProjectID: "other", IndexingStatus: "completed"}, {ProjectID: "acme", IndexingStatus: f.statuses[i]}}, nil
}

func TestWaitIndexed(t *testing.T) {
	t.Run("waits for the index to complete", func(t *testing.T) {
		f := &fakeLister{statuses: []string{"running", "err", "running", "completed"}}
		if !waitIndexed(context.Background(), f, "acme", time.Millisecond, time.Minute) || f.calls != 4 {
			t.Errorf("waitIndexed after %d calls, want true after 4", f.calls)
		}
	})
	t.Run("a failed index is not waited for", func(t *testing.T) {
		if waitIndexed(context.Background(), &fakeLister{statuses: []string{"failed"}}, "acme", time.Millisecond, time.Minute) {
			t.Error("a failed index counted as ready")
		}
	})
	t.Run("gives up at the limit", func(t *testing.T) {
		if waitIndexed(context.Background(), &fakeLister{statuses: []string{"running"}}, "acme", time.Millisecond, 20*time.Millisecond) {
			t.Error("a running index counted as ready")
		}
	})
	t.Run("gives up when the context ends", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if waitIndexed(ctx, &fakeLister{statuses: []string{"running"}}, "acme", time.Millisecond, time.Minute) {
			t.Error("a cancelled wait counted as ready")
		}
	})
}
