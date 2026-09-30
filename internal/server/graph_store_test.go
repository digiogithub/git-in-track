package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/impact"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

func TestFileGraphStore(t *testing.T) {
	newStore := func(dir, gen string) fileGraphStore {
		return fileGraphStore{path: filepath.Join(dir, graphStoreFile), generation: gen, project: "acme"}
	}
	exp := time.Now().Add(time.Minute).Truncate(time.Second)

	t.Run("a second store instance reads what the first wrote", func(t *testing.T) {
		dir := t.TempDir()
		newStore(dir, "1@1").Store(true, exp)
		edges, got, ok := newStore(dir, "1@1").Load()
		if !ok || !edges || !got.Equal(exp) {
			t.Errorf("Load = (%v, %v, %v), want (true, %v, true)", edges, got, ok, exp)
		}
	})

	t.Run("a restart changes the generation and drops the answer", func(t *testing.T) {
		dir := t.TempDir()
		newStore(dir, "1@1").Store(true, exp)
		if _, _, ok := newStore(dir, "2@9").Load(); ok {
			t.Error("an answer of another run was reused")
		}
	})

	t.Run("another project's answer is ignored", func(t *testing.T) {
		dir := t.TempDir()
		newStore(dir, "1@1").Store(true, exp)
		other := newStore(dir, "1@1")
		other.project = "other"
		if _, _, ok := other.Load(); ok {
			t.Error("an answer of another project was reused")
		}
	})

	t.Run("a corrupt or missing file is ignored and rewritten", func(t *testing.T) {
		dir := t.TempDir()
		s := newStore(dir, "1@1")
		if _, _, ok := s.Load(); ok {
			t.Error("a missing file answered")
		}
		if err := os.WriteFile(s.path, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, ok := s.Load(); ok {
			t.Error("a corrupt file answered")
		}
		s.Store(false, exp)
		if edges, _, ok := s.Load(); !ok || edges {
			t.Error("the corrupt file was not replaced")
		}
		if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) != 0 {
			t.Errorf("temporary files left behind: %v", m)
		}
	})

	t.Run("concurrent writers leave a readable file", func(t *testing.T) {
		dir := t.TempDir()
		s := newStore(dir, "1@1")
		done := make(chan struct{})
		for i := 0; i < 8; i++ {
			go func(edges bool) {
				for j := 0; j < 20; j++ {
					s.Store(edges, exp)
					s.Load()
				}
				done <- struct{}{}
			}(i%2 == 0)
		}
		for i := 0; i < 8; i++ {
			<-done
		}
		if _, _, ok := s.Load(); !ok {
			t.Error("the file is unreadable after concurrent writes")
		}
	})
}

func TestGenerationOf(t *testing.T) {
	since := time.Unix(100, 5)
	if g := generationOf(supervisor.Status{State: supervisor.StateReady, PID: 7, Since: since}); g == "" {
		t.Error("a ready instance has no generation")
	}
	if g := generationOf(supervisor.Status{State: supervisor.StateStarting, PID: 7, Since: since}); g != "" {
		t.Errorf("a starting instance has generation %q", g)
	}
	a := generationOf(supervisor.Status{State: supervisor.StateReady, PID: 7, Since: since})
	b := generationOf(supervisor.Status{State: supervisor.StateReady, PID: 8, Since: since})
	c := generationOf(supervisor.Status{State: supervisor.StateReady, PID: 7, Since: since.Add(time.Second)})
	if a == b || a == c {
		t.Errorf("generations %q %q %q must differ across a restart", a, b, c)
	}
}

// Verifies: GIT-US-0188
func TestExternalGraphStore(t *testing.T) {
	dir := t.TempDir()
	exp := time.Now().Add(time.Minute).Truncate(time.Second)
	a := externalGraphStore(dir, "http://127.0.0.1:8080/mcp", "acme")
	a.Store(true, exp)

	t.Run("the same endpoint and project read the stored answer", func(t *testing.T) {
		edges, got, ok := externalGraphStore(dir, "http://127.0.0.1:8080/mcp", "acme").Load()
		if !ok || !edges || !got.Equal(exp) {
			t.Errorf("Load = (%v, %v, %v)", edges, got, ok)
		}
	})
	t.Run("another endpoint or project has its own entry", func(t *testing.T) {
		for _, s := range []impact.GraphStore{
			externalGraphStore(dir, "http://127.0.0.1:9090/mcp", "acme"),
			externalGraphStore(dir, "http://127.0.0.1:8080/mcp", "other"),
		} {
			if _, _, ok := s.Load(); ok {
				t.Error("an answer of another key was reused")
			}
		}
		externalGraphStore(dir, "http://127.0.0.1:9090/mcp", "acme").Store(false, exp)
		if edges, _, ok := a.Load(); !ok || !edges {
			t.Error("another key's write clobbered the first entry")
		}
		if m, _ := filepath.Glob(filepath.Join(dir, "pando", "external", "*.json")); len(m) != 2 {
			t.Errorf("files = %v, want two", m)
		}
	})
	t.Run("the endpoint is not written in clear", func(t *testing.T) {
		m, _ := filepath.Glob(filepath.Join(dir, "pando", "external", "*.json"))
		for _, f := range m {
			b, _ := os.ReadFile(f)
			if strings.Contains(string(b), "127.0.0.1") {
				t.Errorf("%s holds the endpoint URL", f)
			}
		}
	})
}
