package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureName is the vault the command tests run against.
const fixtureName = "project-basic"

// harness is a temporary workspace: a copy of the fixture repository and a
// configuration file the commands are pointed at with GINTRACK_CONFIG.
type harness struct {
	t      *testing.T
	Repo   string
	Config string
	Stdin  io.Reader
}

// newHarness copies the fixture into a temporary directory, marks it as a git
// working tree and points GINTRACK_CONFIG at a fresh configuration file.
func newHarness(t *testing.T) *harness {
	t.Helper()

	root := t.TempDir()
	repo := filepath.Join(root, "acme-api")
	copyTree(t, filepath.Join("..", "..", "testdata", "fixtures", fixtureName), repo)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	cfg := filepath.Join(root, "state", "config.yaml")
	t.Setenv("GINTRACK_CONFIG", cfg)
	return &harness{t: t, Repo: repo, Config: cfg}
}

// register runs `gintrack add` on the fixture repository.
func (h *harness) register() {
	h.t.Helper()
	if _, stderr, code := h.run("add", h.Repo); code != exitOK {
		h.t.Fatalf("add: exit %d\n%s", code, stderr)
	}
}

// run executes one command and returns its streams and its exit code.
func (h *harness) run(args ...string) (stdout, stderr string, code int) {
	h.t.Helper()

	root := newRootCommand(buildInfo{Version: "test", Commit: "abc1234", Date: "2026-09-04T00:00:00Z", BuiltBy: "test"})
	var out, errs bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errs)
	if h.Stdin != nil {
		root.SetIn(h.Stdin)
	} else {
		root.SetIn(strings.NewReader(""))
	}
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		// main reports the error the same way; a test that only sees the exit
		// code would be much harder to debug.
		fmt.Fprintln(&errs, "gintrack:", err)
	}
	return out.String(), errs.String(), exitCode(err)
}

// mustRun executes a command and fails the test when it does not succeed.
func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	stdout, stderr, code := h.run(args...)
	if code != exitOK {
		h.t.Fatalf("%v: exit %d\nstdout:\n%s\nstderr:\n%s", args, code, stdout, stderr)
	}
	return stdout
}

// decode parses a JSON payload a command printed.
func decode[T any](t *testing.T, payload string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("decode: %v\n%s", err, payload)
	}
	return out
}

// copyTree copies a directory recursively.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	for _, e := range entries {
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyTree(t, from, to)
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatalf("read %s: %v", from, err)
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", to, err)
		}
	}
}

// columns splits a table line into its cells.
func columns(line string) []string {
	fields := strings.Split(line, "  ")
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// lines splits printed output into non-empty lines.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// readFile reads a file from the fixture repository.
func (h *harness) readFile(rel string) string {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.Repo, filepath.FromSlash(rel)))
	if err != nil {
		h.t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}
