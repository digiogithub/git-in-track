package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specRepo copies the trace graph fixture of internal/trace (a backlog with
// specs, Go and Vitest tests carrying Verifies: markers) into a temporary
// repository, dropping the ".txt" suffix the fixture files carry.
func specRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "acme")
	src := filepath.Join("..", "..", "internal", "trace", "testdata", "graph")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, strings.TrimSuffix(rel, ".txt"))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"go.mod": "module example.com/acme\n",
		".git":   "", // a directory marker is enough for FindRoot
	} {
		p := filepath.Join(root, name)
		if data == "" {
			err = os.Mkdir(p, 0o755)
		} else {
			err = os.WriteFile(p, []byte(data), 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSpecIngest(t *testing.T) {
	h := newHarness(t)
	root := specRepo(t)
	cache := filepath.Join(t.TempDir(), "results.json")
	report := filepath.Join(t.TempDir(), "go.json")
	stream := strings.Join([]string{
		`{"Action":"run","Package":"example.com/acme/src","Test":"TestNextID"}`,
		`{"Action":"fail","Package":"example.com/acme/src","Test":"TestNextID/stale_counter","Elapsed":0.01}`,
		`{"Action":"pass","Package":"example.com/acme/src","Test":"TestNextID/fresh","Elapsed":0.01}`,
		`{"Action":"fail","Package":"example.com/acme/src","Test":"TestNextID","Elapsed":0.02}`,
		`{"Action":"pass","Package":"example.com/other","Test":"TestGhost"}`,
	}, "\n")
	if err := os.WriteFile(report, []byte(stream), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("json", func(t *testing.T) {
		out := h.mustRun("spec", "ingest", "--repo", filepath.Join(root, "src"), "--cache", cache,
			"--commit", "0123abcd", "--json", report)
		var got specIngestPayload
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("decode %s: %v", out, err)
		}
		if got.Root != root || got.Commit != "0123abcd" || got.Cache != cache {
			t.Errorf("root/commit/cache = %s %s %s", got.Root, got.Commit, got.Cache)
		}
		rep := got.Reports[0]
		if rep.Format != "go" || rep.Tests != 4 || rep.Failed != 2 || rep.Passed != 2 || rep.Unmapped != 1 {
			t.Errorf("report = %+v", rep)
		}
		if got.Stored.Added != 4 || got.Stored.Total != 4 {
			t.Errorf("stored = %+v", got.Stored)
		}
		var r1 string
		for _, rr := range got.Requirements {
			if rr.Ref.String() == "ACME-SP-0001.R1" {
				r1 = string(rr.Result) + " " + rr.Tests[0].Test + " " + strings.Join(rr.Commits, ",")
			}
		}
		if want := "fail src/alloc_test.go#TestNextID/stale_counter 0123abcd"; r1 != want {
			t.Errorf("R1 = %q, want %q", r1, want)
		}
	})

	t.Run("text from stdin replaces the last result", func(t *testing.T) {
		h.Stdin = strings.NewReader(`{"Action":"pass","Package":"example.com/acme/src","Test":"TestNextID/stale_counter"}`)
		defer func() { h.Stdin = nil }()
		out := h.mustRun("spec", "ingest", "--repo", root, "--cache", cache, "--commit", "4567", "-")
		for _, want := range []string{
			"- (go): 1 test — 1 passed, 0 failed, 0 skipped, 0 unmapped",
			"0 added, 1 replaced, 4 total",
			"ACME-SP-0001.R1          pass     1/1 linked tests passed",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("errors", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad.xml")
		if err := os.WriteFile(bad, []byte("<html/>"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			args []string
			code int
		}{
			{[]string{"spec", "ingest", "--repo", root, "--cache", cache}, exitUsage},
			{[]string{"spec", "ingest", "--repo", root, "--cache", cache, "--format", "tap", report}, exitUsage},
			{[]string{"spec", "ingest", "--repo", root, "--cache", cache, filepath.Join(root, "missing.json")}, exitNotFound},
			{[]string{"spec", "ingest", "--repo", root, "--cache", cache, bad}, exitValidation},
		} {
			if _, _, code := h.run(tc.args...); code != tc.code {
				t.Errorf("%v: exit %d, want %d", tc.args[2:], code, tc.code)
			}
		}
	})
}
