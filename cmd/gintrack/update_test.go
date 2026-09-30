package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/selfupdate"
)

// updateFake is a fake GitHub serving gintrack releases with real archives.
type updateFake struct {
	srv      *httptest.Server
	releases []selfupdate.Release
	files    map[string][]byte
}

func newUpdateFake(t *testing.T) *updateFake {
	t.Helper()
	f := &updateFake{files: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(f.releases)
	})
	mux.HandleFunc("/repos/o/r/releases/tags/", func(w http.ResponseWriter, r *http.Request) {
		tag := strings.TrimPrefix(r.URL.Path, "/repos/o/r/releases/tags/")
		for _, rel := range f.releases {
			if rel.TagName == tag {
				_ = json.NewEncoder(w).Encode(rel)
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// publish adds release v<version> whose binary contains content. A non-empty
// badSum is written to checksums.txt instead of the real digest.
func (f *updateFake) publish(version, content, badSum string) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "gintrack", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	_ = gz.Close()
	name := selfupdate.AssetName(version, "linux", "amd64")
	sum := sha256.Sum256(buf.Bytes())
	digest := hex.EncodeToString(sum[:])
	if badSum != "" {
		digest = badSum
	}
	checks := fmt.Sprintf("%s  %s\n", digest, name)
	f.files[name] = buf.Bytes()
	f.files["checksums-"+version+".txt"] = []byte(checks)
	f.releases = append(f.releases, selfupdate.Release{
		TagName: "v" + version,
		HTMLURL: "https://example.test/releases/v" + version,
		Assets: []selfupdate.Asset{
			{Name: name, Size: int64(buf.Len()), BrowserDownloadURL: f.srv.URL + "/dl/" + name},
			{Name: selfupdate.ChecksumsName, Size: int64(len(checks)), BrowserDownloadURL: f.srv.URL + "/dl/checksums-" + version + ".txt"},
		},
	})
}

// updateRig is one `gintrack update` invocation against a temp install.
type updateRig struct {
	t      *testing.T
	fake   *updateFake
	target string
	cache  string
	tty    bool
}

func newUpdateRig(t *testing.T) *updateRig {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "gintrack")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old binary"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GINTRACK_CONFIG", filepath.Join(dir, "state", "config.yaml"))
	return &updateRig{t: t, fake: newUpdateFake(t), target: target, cache: filepath.Join(dir, "cache")}
}

// run executes `gintrack update` as a build with the given version and builder.
func (r *updateRig) run(version, builtBy, stdin string, args ...string) (stdout, stderr string, code int) {
	r.t.Helper()
	updateSeam = &updateEnv{
		client:      selfupdate.Options{BaseURL: r.fake.srv.URL, Repo: "o/r", Token: "tok"},
		exe:         func() (string, error) { return r.target, nil },
		channel:     selfupdate.Env{ReadFile: func(string) ([]byte, error) { return nil, errors.New("absent") }},
		goos:        "linux",
		goarch:      "amd64",
		interactive: func(io.Reader) bool { return r.tty },
		cacheDir:    func() (string, error) { return r.cache, nil },
	}
	r.t.Cleanup(func() { updateSeam = nil })
	root := newRootCommand(buildInfo{Version: version, Commit: "abc", Date: "d", BuiltBy: builtBy})
	var out, errs bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"update"}, args...))
	err := root.Execute()
	if err != nil {
		fmt.Fprintln(&errs, "gintrack:", err)
	}
	return out.String(), errs.String(), exitCode(err)
}

func (r *updateRig) installed() string {
	r.t.Helper()
	b, err := os.ReadFile(r.target)
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func TestUpdateUpToDate(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.0.0", "new", "")
	stdout, _, code := r.run("2.0.0", "goreleaser", "", "--json")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	res := decode[updateResult](t, stdout)
	if res.Action != "up_to_date" || res.Current != "2.0.0" || res.Latest != "2.0.0" || res.Channel != "release" {
		t.Errorf("result = %+v", res)
	}
	if r.installed() != "old binary" {
		t.Error("the binary changed")
	}
}

func TestUpdateCheckAvailable(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.0.0", "old", "")
	r.fake.publish("2.1.0", "new", "")
	stdout, _, code := r.run("2.0.0", "goreleaser", "", "--check", "--json")
	if code != exitUpdateAvailable {
		t.Fatalf("exit %d, want %d", code, exitUpdateAvailable)
	}
	res := decode[updateResult](t, stdout)
	if res.Action != "available" || res.Latest != "2.1.0" || res.URL != "https://example.test/releases/v2.1.0" {
		t.Errorf("result = %+v", res)
	}
	if r.installed() != "old binary" {
		t.Error("--check changed the binary")
	}
}

func TestUpdateApplied(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.1.0", "new binary", "")
	// A live supervisor state file: this process stands in for gintrack serve.
	dir := filepath.Join(r.cache, "pando", "repo-1234")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := fmt.Sprintf(`{"state":"ready","pid":%d,"supervisorPid":%d,"root":"/srv/repo"}`, os.Getpid(), os.Getpid())
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := r.run("2.0.0", "goreleaser", "", "--yes")
	if code != exitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "Updated gintrack 2.0.0 -> 2.1.0") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "gintrack serve (pid") || !strings.Contains(stdout, "managed Pando") || !strings.Contains(stdout, "nothing was restarted") {
		t.Errorf("no restart hint: %q", stdout)
	}
	if r.installed() != "new binary" {
		t.Errorf("installed = %q", r.installed())
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(r.target)
		if err != nil || info.Mode().Perm() != 0o750 {
			t.Errorf("mode = %v %v, want the old 0750 kept", info.Mode().Perm(), err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(r.target))
	for _, e := range entries {
		if e.Name() != "gintrack" {
			t.Errorf("leftover %s next to the binary", e.Name())
		}
	}
}

func TestUpdateNoRestartHintWithoutSupervisor(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.1.0", "new binary", "")
	stdout, _, code := r.run("2.0.0", "goreleaser", "", "--yes", "--json")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	res := decode[updateResult](t, stdout)
	if res.Action != "updated" || len(res.Running) != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestUpdateRefusedChannel(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.1.0", "new", "")
	brew := filepath.Join(t.TempDir(), "Cellar", "gintrack", "2.0.0", "bin", "gintrack")
	if err := os.MkdirAll(filepath.Dir(brew), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brew, []byte("brewed"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.target = brew
	stdout, stderr, code := r.run("2.0.0", "goreleaser", "", "--yes", "--json")
	if code != exitUpdateRefused {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	res := decode[updateResult](t, stdout)
	if res.Action != "refused" || res.Channel != "homebrew" || !strings.Contains(res.Reason, "brew upgrade") {
		t.Errorf("result = %+v", res)
	}
	if r.installed() != "brewed" {
		t.Error("a refused update changed the binary")
	}

	// --force replaces it anyway, with a warning.
	stdout, _, code = r.run("2.0.0", "goreleaser", "", "--yes", "--force", "--json")
	if code != exitOK {
		t.Fatalf("--force: exit %d", code)
	}
	res = decode[updateResult](t, stdout)
	if res.Action != "updated" || !strings.Contains(res.Warning, "homebrew") {
		t.Errorf("forced result = %+v", res)
	}
	if r.installed() != "new" {
		t.Errorf("installed = %q", r.installed())
	}
}

func TestUpdateNonTTYWithoutYes(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.1.0", "new", "")
	_, stderr, code := r.run("2.0.0", "goreleaser", "y\n")
	if code != exitUpdateRefused || !strings.Contains(stderr, "--yes") {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if r.installed() != "old binary" {
		t.Error("the binary changed without consent")
	}
}

func TestUpdateConfirmationOnTTY(t *testing.T) {
	r := newUpdateRig(t)
	r.tty = true
	r.fake.publish("2.1.0", "new", "")
	if _, _, code := r.run("2.0.0", "goreleaser", "n\n"); code != exitUpdateRefused {
		t.Fatalf("answering n: exit %d", code)
	}
	if r.installed() != "old binary" {
		t.Fatal("declined update changed the binary")
	}
	if _, stderr, code := r.run("2.0.0", "goreleaser", "y\n"); code != exitOK || !strings.Contains(stderr, "[y/N]") {
		t.Fatalf("answering y: exit %d\n%s", code, stderr)
	}
	if r.installed() != "new" {
		t.Errorf("installed = %q", r.installed())
	}
}

func TestUpdateChecksumMismatchLeavesBinaryAlone(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.1.0", "tampered", strings.Repeat("0", 64))
	stdout, stderr, code := r.run("2.0.0", "goreleaser", "", "--yes", "--json")
	if code != exitUpdateVerify {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	res := decode[updateResult](t, stdout)
	if res.Action != "failed" || !strings.Contains(res.Reason, "verification failed") {
		t.Errorf("result = %+v", res)
	}
	if r.installed() != "old binary" {
		t.Error("an unverified binary was installed")
	}
	entries, _ := os.ReadDir(filepath.Dir(r.target))
	if len(entries) != 1 {
		t.Errorf("staging left files behind: %v", entries)
	}
}

func TestUpdateSameVersionNeedsForce(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.0.0", "reinstalled", "")
	if _, _, code := r.run("2.0.0", "goreleaser", "", "--yes"); code != exitOK || r.installed() != "old binary" {
		t.Fatalf("without --force: exit %d, installed %q", code, r.installed())
	}
	if _, _, code := r.run("2.0.0", "goreleaser", "", "--yes", "--force"); code != exitOK || r.installed() != "reinstalled" {
		t.Fatalf("with --force: exit %d, installed %q", code, r.installed())
	}
}

func TestUpdateDowngrade(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("1.9.0", "older", "")
	r.fake.publish("2.0.0", "current", "")

	// Without a version the latest is not newer: nothing happens.
	stdout, _, code := r.run("2.0.0", "goreleaser", "", "--yes", "--json")
	if code != exitOK || decode[updateResult](t, stdout).Action != "up_to_date" {
		t.Fatalf("implicit: exit %d %s", code, stdout)
	}
	// An explicit older version is installed, after confirming.
	r.tty = true
	_, stderr, code := r.run("2.0.0", "goreleaser", "y\n", "1.9.0")
	if code != exitOK || !strings.Contains(stderr, "DOWNGRADE") {
		t.Fatalf("explicit: exit %d\n%s", code, stderr)
	}
	if r.installed() != "older" {
		t.Errorf("installed = %q", r.installed())
	}
}

func TestUpdateDevBuildNeedsAVersion(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.1.0", "new", "")
	// A source build is refused by the channel check without a version.
	stdout, _, code := r.run("dev", "source", "", "--yes", "--json")
	if code != exitUpdateRefused || decode[updateResult](t, stdout).Channel != "source" {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	// A release-built binary with a non-semver version is refused too.
	stdout, _, code = r.run("dev", "goreleaser", "", "--yes", "--json")
	if code != exitUpdateRefused || !strings.Contains(decode[updateResult](t, stdout).Reason, "explicit version") {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	if r.installed() != "old binary" {
		t.Fatal("a refused update changed the binary")
	}
	// An explicit version is accepted on a source build.
	if _, stderr, code := r.run("dev", "source", "", "--yes", "2.1.0"); code != exitOK {
		t.Fatalf("explicit version: exit %d\n%s", code, stderr)
	}
	if r.installed() != "new" {
		t.Errorf("installed = %q", r.installed())
	}
}

func TestUpdateUnknownVersion(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.1.0", "new", "")
	stdout, _, code := r.run("2.0.0", "goreleaser", "", "--yes", "--json", "9.9.9")
	if code != exitNotFound || decode[updateResult](t, stdout).Action != "failed" {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

func TestUpdateExitCodesAreDistinct(t *testing.T) {
	seen := map[int]string{}
	for name, c := range map[string]int{
		"ok": exitOK, "failure": exitFailure, "usage": exitUsage, "validation": exitValidation,
		"notFound": exitNotFound, "conflict": exitConflict, "git": exitGit, "gate": exitGate,
		"available": exitUpdateAvailable, "refused": exitUpdateRefused, "verify": exitUpdateVerify, "apply": exitUpdateApply,
	} {
		if prev, dup := seen[c]; dup {
			t.Errorf("exit code %d is both %s and %s", c, prev, name)
		}
		seen[c] = name
	}
}
