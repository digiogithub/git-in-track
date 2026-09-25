package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// gitSpecRepo is specRepo with real git history: one commit holding the whole
// fixture, registered in the harness workspace.
func gitSpecRepo(t *testing.T, h *harness) string {
	t.Helper()
	root := specRepo(t)
	if err := os.Remove(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "init", "--initial-branch=main")
	identify(t, root)
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-m", "chore: seed")
	if _, stderr, code := h.run("add", root); code != exitOK {
		t.Fatalf("add: exit %d\n%s", code, stderr)
	}
	return root
}

// ingest records one go test -json stream for the spec fixture at HEAD.
func ingest(t *testing.T, h *harness, root, stream string) {
	t.Helper()
	report := filepath.Join(t.TempDir(), "go.json")
	if err := os.WriteFile(report, []byte(stream), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun("spec", "ingest", "--repo", root, report)
}

const (
	r1Fails  = `{"Action":"fail","Package":"example.com/acme/src","Test":"TestNextID/stale_counter"}`
	r1Passes = `{"Action":"pass","Package":"example.com/acme/src","Test":"TestNextID/stale_counter"}`
)

func TestSpecCommandUsage(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"impact without --since", []string{"spec", "impact"}},
		{"impact with an argument", []string{"spec", "impact", "--since", "HEAD", "extra"}},
		{"impact bad format", []string{"spec", "impact", "--since", "HEAD", "--format", "yaml"}},
		{"impact budget too large", []string{"spec", "impact", "--since", "HEAD", "--budget", "20001"}},
		{"impact negative budget", []string{"spec", "impact", "--since", "HEAD", "--budget", "-1"}},
		{"impact unknown tier", []string{"spec", "impact", "--since", "HEAD", "--tiers", "1,4"}},
		{"impact tier not a number", []string{"spec", "impact", "--since", "HEAD", "--tiers", "one"}},
		{"impact unknown fail-on state", []string{"spec", "impact", "--since", "HEAD", "--fail-on", "failing,broken"}},
		{"impact fail-on behaviour without a state", []string{"spec", "impact", "--since", "HEAD", "--fail-on", "behaviour"}},
		{"coverage unknown status", []string{"spec", "coverage", "--status", "green"}},
		{"coverage spec is a story", []string{"spec", "coverage", "--spec", "ACME-US-0001"}},
		{"verify without refs", []string{"spec", "verify"}},
		{"verify a story", []string{"spec", "verify", "ACME-US-0001"}},
		{"trace without ref", []string{"spec", "trace"}},
		{"trace two refs", []string{"spec", "trace", "ACME-SP-0001.R1", "ACME-SP-0001.R2"}},
		{"trace a spec id", []string{"spec", "trace", "ACME-SP-0001"}},
		{"lint a story", []string{"spec", "lint", "ACME-US-0001"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, stderr, code := h.run(tc.args...); code != exitUsage {
				t.Errorf("exit %d, want %d\n%s", code, exitUsage, stderr)
			}
		})
	}
}

func TestSpecLint(t *testing.T) {
	h := newHarness(t)
	root := specRepo(t)
	h.mustRun("add", root)

	t.Run("warnings exit 0", func(t *testing.T) {
		out := h.mustRun("spec", "lint")
		for _, want := range []string{"warning ACME-SP-0001  LINT-REQ-SCENARIO", "2 specs checked: 0 errors"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	})
	t.Run("one spec", func(t *testing.T) {
		got := decode[specLintPayload](t, h.mustRun("spec", "lint", "--json", "ACME-SP-0002"))
		if len(got.Specs) != 1 || got.Specs[0].ID != "ACME-SP-0002" || got.Errors != 0 {
			t.Errorf("payload = %+v", got)
		}
	})
	t.Run("unknown spec", func(t *testing.T) {
		if _, _, code := h.run("spec", "lint", "ACME-SP-0099"); code != exitNotFound {
			t.Errorf("exit %d, want %d", code, exitNotFound)
		}
	})
	t.Run("errors exit 3", func(t *testing.T) {
		cfg := filepath.Join(root, "docs", ".pmngr", "project.yaml")
		data, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg, append(data, []byte("specs:\n  lint: error\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		stdout, _, code := h.run("spec", "lint", "--json")
		if code != exitValidation {
			t.Fatalf("exit %d, want %d\n%s", code, exitValidation, stdout)
		}
		if got := decode[specLintPayload](t, stdout); got.Errors == 0 {
			t.Errorf("payload has no errors: %+v", got)
		}
	})
}

func TestSpecImpactFailOn(t *testing.T) {
	h := newHarness(t)
	root := gitSpecRepo(t, h)
	ingest(t, h, root, r1Fails)
	// Touch NextID, which ACME-SP-0001.R1 traces.
	src := filepath.Join(root, "src", "alloc.go")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(strings.Replace(string(data), "return 1", "return 1 + 0", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("text report without a gate", func(t *testing.T) {
		out := h.mustRun("spec", "impact", "--since", "HEAD")
		for _, want := range []string{"impact HEAD..worktree: 1 files", "tiers: 1 ok", "2 unavailable", "ACME-SP-0001.R1 t1 failing"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	})
	t.Run("fail-on failing exits 7", func(t *testing.T) {
		stdout, stderr, code := h.run("spec", "impact", "--since", "HEAD", "--fail-on", "failing,suspect")
		if code != exitGate {
			t.Fatalf("exit %d, want %d\n%s", code, exitGate, stderr)
		}
		if !strings.Contains(stdout, "ACME-SP-0001.R1") {
			t.Errorf("the report is still printed on stdout:\n%s", stdout)
		}
		if !strings.Contains(stderr, "ACME-SP-0001.R1  failing") {
			t.Errorf("stderr does not list the offender:\n%s", stderr)
		}
	})
	t.Run("fail-on json", func(t *testing.T) {
		stdout, _, code := h.run("spec", "impact", "--since", "HEAD", "--fail-on", "failing", "--json")
		if code != exitGate {
			t.Fatalf("exit %d, want %d", code, exitGate)
		}
		got := decode[specImpactPayload](t, stdout)
		if len(got.Offending) != 1 || got.Offending[0].Ref.String() != "ACME-SP-0001.R1" || len(got.Report.Hits) == 0 {
			t.Errorf("payload = %+v", got)
		}
	})
	t.Run("json is compact and within the reported tokens", func(t *testing.T) {
		// GIT-US-0160: the report's tokens estimate measures compact JSON, so
		// the printed report must not cost more than it says.
		stdout := h.mustRun("spec", "impact", "--since", "HEAD", "--format", "json")
		if strings.Count(stdout, "\n") != 1 || !strings.HasSuffix(stdout, "\n") {
			t.Fatalf("output is not one compact line:\n%s", stdout)
		}
		var raw struct {
			Report json.RawMessage `json:"report"`
		}
		if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
			t.Fatal(err)
		}
		got := decode[specImpactPayload](t, stdout)
		if got.Report.Tokens == 0 || len(got.Report.Hits) == 0 {
			t.Fatalf("payload = %+v", got)
		}
		if est := core.EstimateTokens(string(raw.Report)); est > got.Report.Tokens {
			t.Errorf("printed report = %d estimated tokens, reported %d", est, got.Report.Tokens)
		}
		// Without --fail-on the envelope {"report":…} is all that is added.
		envelope := len(`{"report":}` + "\n")
		if limit := 3*got.Report.Tokens + envelope; len(stdout) > limit {
			t.Errorf("printed %d bytes, want ≤ %d (3 × %d tokens + envelope)", len(stdout), limit, got.Report.Tokens)
		}
	})
	t.Run("pretty indents the json", func(t *testing.T) {
		stdout := h.mustRun("spec", "impact", "--since", "HEAD", "--json", "--pretty")
		if !strings.Contains(stdout, "\n  \"report\": {") {
			t.Errorf("output is not indented:\n%s", stdout)
		}
		if got := decode[specImpactPayload](t, stdout); len(got.Report.Hits) == 0 {
			t.Errorf("payload = %+v", got)
		}
	})
	t.Run("fail-on a state no hit has", func(t *testing.T) {
		if _, stderr, code := h.run("spec", "impact", "--since", "HEAD", "--fail-on", "passing"); code != exitOK {
			t.Errorf("exit %d, want 0\n%s", code, stderr)
		}
	})
	t.Run("a one-token budget does not hide an offender", func(t *testing.T) {
		if _, _, code := h.run("spec", "impact", "--since", "HEAD", "--tiers", "1", "--budget", "1", "--fail-on", "failing"); code != exitGate {
			t.Errorf("exit %d, want %d", code, exitGate)
		}
	})
	t.Run("unknown revision", func(t *testing.T) {
		if _, _, code := h.run("spec", "impact", "--since", "no-such-branch"); code != exitValidation {
			t.Errorf("exit %d, want %d", code, exitValidation)
		}
	})
}

// TestSpecImpactFailOnBehaviour is the test-only filter of GIT-US-0157: a
// diff that changes only a test verifying a failing requirement is a
// test-only hit, which trips the default gate and not a gate restricted to
// behaviour hits.
func TestSpecImpactFailOnBehaviour(t *testing.T) {
	h := newHarness(t)
	root := gitSpecRepo(t, h)
	ingest(t, h, root, r1Fails)
	// Touch the stale_counter sub-test, which verifies ACME-SP-0001.R1.
	src := filepath.Join(root, "src", "alloc_test.go")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(strings.Replace(string(data), `t.Fatal("bad")`, `t.Fatal("bad id")`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("the text report marks the hit test-only", func(t *testing.T) {
		out := h.mustRun("spec", "impact", "--since", "HEAD", "--tiers", "1")
		if !strings.Contains(out, "ACME-SP-0001.R1 t1 failing test-only") {
			t.Errorf("output lacks the test-only hit:\n%s", out)
		}
	})
	t.Run("the default gate still trips on it", func(t *testing.T) {
		stdout, stderr, code := h.run("spec", "impact", "--since", "HEAD", "--tiers", "1", "--fail-on", "failing,suspect", "--json")
		if code != exitGate {
			t.Fatalf("exit %d, want %d\n%s", code, exitGate, stderr)
		}
		got := decode[specImpactPayload](t, stdout)
		if len(got.Offending) != 1 || got.Offending[0].Kind != core.ImpactKindTestOnly {
			t.Errorf("offending = %+v, want the test-only R1", got.Offending)
		}
	})
	t.Run("behaviour leaves it out", func(t *testing.T) {
		stdout, stderr, code := h.run("spec", "impact", "--since", "HEAD", "--tiers", "1", "--fail-on", "failing,suspect,behaviour", "--json")
		if code != exitOK {
			t.Fatalf("exit %d, want 0\n%s", code, stderr)
		}
		got := decode[specImpactPayload](t, stdout)
		if strings.Join(got.FailOn, ",") != "failing,suspect,behaviour" || len(got.Offending) != 0 || len(got.Report.Hits) != 1 {
			t.Errorf("payload = %+v, want the hit reported and no offender", got)
		}
	})
}

// TestSpecImpactGateClearsAtHead is the gate of GIT-US-0148 end to end: a
// pull request that edits the traced code of a passing requirement trips
// `--fail-on suspect` until the linked tests are re-run and ingested at its
// head, and a failing re-run still trips it as failing.
func TestSpecImpactGateClearsAtHead(t *testing.T) {
	h := newHarness(t)
	root := gitSpecRepo(t, h)
	ingest(t, h, root, r1Passes) // R1 passed on main.

	gitIn(t, root, "checkout", "-b", "pr")
	src := filepath.Join(root, "src", "alloc.go")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte(strings.Replace(string(data), "return 1", "return 1 + 0", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "commit", "-am", "fix: change NextID")
	gate := []string{"spec", "impact", "--since", "main", "--tiers", "1,2", "--format", "text", "--fail-on", "failing,suspect"}

	t.Run("tests not re-run: suspect trips the gate", func(t *testing.T) {
		_, stderr, code := h.run(gate...)
		if code != exitGate || !strings.Contains(stderr, "ACME-SP-0001.R1  suspect") {
			t.Fatalf("exit %d, want %d with R1 suspect\n%s", code, exitGate, stderr)
		}
	})
	t.Run("tests re-run and ingested at head: the gate passes", func(t *testing.T) {
		ingest(t, h, root, r1Passes)
		stdout, stderr, code := h.run(gate...)
		if code != exitOK {
			t.Fatalf("exit %d, want 0\n%s\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "ACME-SP-0001.R1 t1 passing") || strings.Contains(stdout, "suspect") {
			t.Errorf("R1 is not a plain passing hit:\n%s", stdout)
		}
	})
	t.Run("a failing re-run at head still trips the gate", func(t *testing.T) {
		ingest(t, h, root, r1Fails)
		_, stderr, code := h.run(gate...)
		if code != exitGate || !strings.Contains(stderr, "ACME-SP-0001.R1  failing") {
			t.Fatalf("exit %d, want %d with R1 failing\n%s", code, exitGate, stderr)
		}
	})
}

func TestSpecVerifyCoverageTrace(t *testing.T) {
	h := newHarness(t)
	root := gitSpecRepo(t, h)
	ingest(t, h, root, r1Passes)
	spec := filepath.Join(root, "docs", ".pmngr", "specs", "ACME-SP-0001-allocation.md")
	before, err := os.ReadFile(spec)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("coverage", func(t *testing.T) {
		got := decode[specCoveragePayload](t, h.mustRun("spec", "coverage", "--spec", "ACME-SP-0001", "--json"))
		status := map[string]string{}
		for _, r := range got.Coverage {
			status[r.Ref.String()] = string(r.Status)
		}
		if status["ACME-SP-0001.R1"] != "passing" || status["ACME-SP-0001.R3"] != "untested" {
			t.Errorf("coverage = %v", status)
		}
		out := h.mustRun("spec", "coverage", "--status", "passing")
		if !strings.Contains(out, "ACME-SP-0001.R1") || strings.Contains(out, "ACME-SP-0001.R3") {
			t.Errorf("filtered coverage:\n%s", out)
		}
	})
	t.Run("trace", func(t *testing.T) {
		out := h.mustRun("spec", "trace", "ACME-SP-0001.R1")
		for _, want := range []string{"src/alloc.go#NextID", "src/alloc_test.go#TestNextID/stale_counter", "ACME-US-0001"} {
			if !strings.Contains(out, want) {
				t.Errorf("trace lacks %q:\n%s", want, out)
			}
		}
		if _, _, code := h.run("spec", "trace", "ACME-SP-0001.R9"); code != exitNotFound {
			t.Errorf("unknown requirement: exit %d, want %d", code, exitNotFound)
		}
	})
	t.Run("dry run writes nothing", func(t *testing.T) {
		out := h.mustRun("spec", "verify", "ACME-SP-0001.R1", "ACME-SP-0002")
		for _, want := range []string{"would stamp ACME-SP-0001.R1", "unstamped   ACME-SP-0002.R", "nothing was written"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
		after, err := os.ReadFile(spec)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Errorf("a dry run changed the spec:\n%s", after)
		}
	})
	t.Run("commit writes the stamp", func(t *testing.T) {
		got := decode[specVerifyPayload](t, h.mustRun("spec", "verify", "--commit", "--by", "ci", "--json", "ACME-SP-0001.R1"))
		if len(got.Stamped) != 1 || got.Stamped[0].Verified.By == "" || len(got.Written) != 1 {
			t.Fatalf("payload = %+v", got)
		}
		after, err := os.ReadFile(spec)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(after), "verified:") {
			t.Errorf("the spec holds no stamp:\n%s", after)
		}
		again := decode[specVerifyPayload](t, h.mustRun("spec", "verify", "--commit", "--by", "ci", "--json", "ACME-SP-0001.R1"))
		if len(again.Stamped) != 0 || len(again.Unstamped) != 1 || again.Unstamped[0].Reason != "unchanged" {
			t.Errorf("second run = %+v", again)
		}
	})
}
