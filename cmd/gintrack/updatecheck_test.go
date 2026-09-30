package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/selfupdate"
)

func TestNoticeAllowed(t *testing.T) {
	t.Parallel()

	ok := noticeInputs{command: "ls", stderrTTY: true, checkStart: true}
	for _, tc := range []struct {
		name   string
		mutate func(*noticeInputs)
		want   bool
	}{
		{name: "interactive opted in", mutate: func(*noticeInputs) {}, want: true},
		{name: "not opted in", mutate: func(i *noticeInputs) { i.checkStart = false }},
		{name: "--json", mutate: func(i *noticeInputs) { i.asJSON = true }},
		{name: "--quiet", mutate: func(i *noticeInputs) { i.quiet = true }},
		{name: "stderr is not a terminal", mutate: func(i *noticeInputs) { i.stderrTTY = false }},
		{name: "mcp", mutate: func(i *noticeInputs) { i.command = "mcp" }},
		{name: "serve", mutate: func(i *noticeInputs) { i.command = "serve" }},
		{name: "update", mutate: func(i *noticeInputs) { i.command = "update" }},
		{name: "version", mutate: func(i *noticeInputs) { i.command = "version" }},
		{name: "doctor", mutate: func(i *noticeInputs) { i.command = "doctor" }},
		{name: "completion", mutate: func(i *noticeInputs) { i.command = "__complete" }},
		{name: "another command", mutate: func(i *noticeInputs) { i.command = "item" }, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := ok
			tc.mutate(&in)
			if got := noticeAllowed(in); got != tc.want {
				t.Fatalf("noticeAllowed(%+v) = %v, want %v", in, got, tc.want)
			}
		})
	}
}

func TestNoticeLine(t *testing.T) {
	t.Parallel()

	if got := noticeLine(selfupdate.Status{Current: "2.2.0", Latest: "2.3.0", UpdateAvailable: true}); !strings.Contains(got, "2.3.0") ||
		!strings.Contains(got, "gintrack update") || strings.Contains(got, "\n") {
		t.Fatalf("line = %q", got)
	}
	for _, st := range []selfupdate.Status{
		{Current: "2.3.0", Latest: "2.3.0"},
		{Current: "dev", NotApplicable: "development build"},
		{Current: "2.2.0", Err: "offline"},
	} {
		if got := noticeLine(st); got != "" {
			t.Errorf("noticeLine(%+v) = %q, want empty", st, got)
		}
	}
}

// fakeRelease points the update checker of this process at a fake GitHub that
// publishes latest, and returns the number of lookups it served.
func fakeRelease(t *testing.T, latest string) *int {
	t.Helper()
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*hits++
		_, _ = w.Write([]byte(`[{"tag_name":"v` + latest + `","html_url":"https://example.test/r"}]`))
	}))
	t.Cleanup(srv.Close)
	prev, prevTTY := newUpdateChecker, stderrIsTerminal
	t.Cleanup(func() { newUpdateChecker, stderrIsTerminal = prev, prevTTY })
	newUpdateChecker = func(build buildInfo, dir string) *selfupdate.Checker {
		return &selfupdate.Checker{
			Dir: dir, Current: build.Version,
			Client:        selfupdate.New(selfupdate.Options{BaseURL: srv.URL, HTTPClient: srv.Client()}),
			NotApplicable: func() string { return installNotApplicable(build) },
		}
	}
	return hits
}

func runWithVersion(h *harness, version string, args ...string) (stderr string) {
	h.t.Helper()
	root := newRootCommand(buildInfo{Version: version, BuiltBy: "goreleaser"})
	var out, errs bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs(args)
	_ = root.Execute()
	return errs.String()
}

// The tests below swap package-level seams, so they do not run in parallel.
func TestDoctorReportsUpdates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		latest  string
		want    string
		hits    int
	}{
		{name: "update available", version: "2.2.0", latest: "2.3.0", want: "gintrack 2.3.0 is available", hits: 1},
		{name: "up to date", version: "2.3.0", latest: "2.3.0", want: "is up to date", hits: 1},
		{name: "development build", version: "dev", latest: "2.3.0", want: "not checked: development build", hits: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			hits := fakeRelease(t, tc.latest)
			_ = h
			root := newRootCommand(buildInfo{Version: tc.version, BuiltBy: "goreleaser"})
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{"doctor"})
			_ = root.Execute()
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("doctor output lacks %q:\n%s", tc.want, out.String())
			}
			if *hits > tc.hits {
				t.Fatalf("lookups = %d, want at most %d", *hits, tc.hits)
			}
		})
	}

	t.Run("lookup failure is a warning, not an error", func(t *testing.T) {
		h := newHarness(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(srv.Close)
		prev := newUpdateChecker
		t.Cleanup(func() { newUpdateChecker = prev })
		newUpdateChecker = func(build buildInfo, dir string) *selfupdate.Checker {
			return &selfupdate.Checker{Dir: dir, Current: build.Version,
				Client: selfupdate.New(selfupdate.Options{BaseURL: srv.URL, HTTPClient: srv.Client()})}
		}
		root := newRootCommand(buildInfo{Version: "2.2.0", BuiltBy: "goreleaser"})
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs([]string{"doctor", "--json"})
		if err := root.Execute(); err != nil {
			t.Fatalf("doctor failed on a lookup error: %v", err)
		}
		if !strings.Contains(out.String(), `"scope": "update"`) && !strings.Contains(out.String(), `"scope":"update"`) {
			t.Fatalf("no update check in:\n%s", out.String())
		}
		_ = h
	})
}

func TestCheckOnStartNotice(t *testing.T) {
	setup := func(t *testing.T, optIn bool) (*harness, *int) {
		t.Helper()
		h := newHarness(t)
		h.register()
		hits := fakeRelease(t, "2.3.0")
		stderrIsTerminal = func(io.Writer) bool { return true }
		if optIn {
			on := strings.Replace(string(mustRead(t, h.Config)), "checkOnStart: false", "checkOnStart: true", 1)
			if err := os.WriteFile(h.Config, []byte(on), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return h, hits
	}

	t.Run("prints one line when opted in", func(t *testing.T) {
		h, _ := setup(t, true)
		stderr := runWithVersion(h, "2.2.0", "ls")
		if n := strings.Count(stderr, "gintrack 2.3.0 is available"); n != 1 {
			t.Fatalf("notice count = %d, stderr:\n%s", n, stderr)
		}
	})
	t.Run("nothing without the opt-in, and no lookup", func(t *testing.T) {
		h, hits := setup(t, false)
		stderr := runWithVersion(h, "2.2.0", "ls")
		if strings.Contains(stderr, "available") || *hits != 0 {
			t.Fatalf("stderr = %q, lookups = %d", stderr, *hits)
		}
	})
	t.Run("nothing with --json", func(t *testing.T) {
		h, _ := setup(t, true)
		if stderr := runWithVersion(h, "2.2.0", "ls", "--json"); strings.Contains(stderr, "available") {
			t.Fatalf("stderr = %q", stderr)
		}
	})
	t.Run("nothing for version", func(t *testing.T) {
		h, _ := setup(t, true)
		if stderr := runWithVersion(h, "2.2.0", "version"); strings.Contains(stderr, "available") {
			t.Fatalf("stderr = %q", stderr)
		}
	})
	t.Run("nothing on a development build", func(t *testing.T) {
		h, hits := setup(t, true)
		if stderr := runWithVersion(h, "dev", "ls"); strings.Contains(stderr, "available") || *hits != 0 {
			t.Fatalf("stderr = %q, lookups = %d", stderr, *hits)
		}
	})
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
