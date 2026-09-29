package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// writeInstance lays down an instance directory the way a supervisor does.
func writeInstance(t *testing.T, cache, root string, st supervisor.Status, token string) string {
	t.Helper()

	dir := supervisor.InstanceDir(cache, supervisor.InstanceKey(root))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if token != "" {
		if err := os.WriteFile(filepath.Join(dir, "token"), []byte(token+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestDiscoveredInstance covers how a connect-only process finds the instance
// a running serve supervises.
func TestDiscoveredInstance(t *testing.T) {
	t.Parallel()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	self := os.Getpid()
	const dead = 2147483000

	ready := func(supervisorPID, pid, port int) supervisor.Status {
		return supervisor.Status{
			State: supervisor.StateReady, SupervisorPID: supervisorPID, PID: pid, Port: port,
			MCPURL: "http://127.0.0.1:" + strconv.Itoa(port) + "/mcp",
		}
	}
	tests := []struct {
		name      string
		state     *supervisor.Status
		token     string
		wantFound bool
		wantMsg   string
	}{
		{"found", ptr(ready(self, self, port)), "secret-token", true, ""},
		{"stale state file", ptr(ready(dead, dead, port)), "secret-token", false, MsgManagedNotRunning},
		{"child gone", ptr(ready(self, dead, port)), "secret-token", false, MsgManagedNotRunning},
		{"nothing listens", ptr(ready(self, self, 1)), "secret-token", false, MsgManagedNotRunning},
		{"no token file", ptr(ready(self, self, port)), "", false, MsgManagedNotRunning},
		{"not running", nil, "", false, MsgManagedNotRunning},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cache, root := t.TempDir(), t.TempDir()
			dir := supervisor.InstanceDir(cache, supervisor.InstanceKey(root))
			if tc.state != nil {
				dir = writeInstance(t, cache, root, *tc.state, tc.token)
			}
			inst := discoveredInstance{dir: dir}
			url, token, ok := inst.Endpoint()
			if ok != tc.wantFound {
				t.Fatalf("Endpoint() ok = %v (%s), want %v", ok, url, tc.wantFound)
			}
			if tc.wantFound && token != tc.token {
				t.Errorf("token = %q, want the token file's content", token)
			}
			if !tc.wantFound && !strings.Contains(inst.Status().LastError, tc.wantMsg) {
				t.Errorf("Status().LastError = %q, want %q", inst.Status().LastError, tc.wantMsg)
			}
		})
	}
}

func TestInstallDiscoveredSemanticSearchNeverStarts(t *testing.T) {
	t.Parallel()

	cache, root := t.TempDir(), copyTree(t, fixtureRoot)
	fsys, err := osfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(fsys, "a")
	if err != nil {
		t.Fatal(err)
	}
	space := vault.NewWorkspace()
	if _, err := space.Attach("a", roleProject, v); err != nil {
		t.Fatal(err)
	}
	host := InstallDiscoveredSemanticSearch(cache, space, []SemanticRepo{
		{ID: "a", Path: root, Role: roleProject, SemanticSearch: true, Vault: v},
	}, nil)
	t.Cleanup(func() { _ = host.Close() })

	if host.discovered == nil {
		t.Fatal("an opted-in repository must give a connect-only host")
	}
	_, err = host.discovered.searcherOf(host.discovered.slot("a"))
	if !errors.Is(err, pando.ErrUnreachable) || !strings.Contains(err.Error(), MsgManagedNotRunning) {
		t.Errorf("searcherOf() = %v, want unreachable naming %q", err, MsgManagedNotRunning)
	}
	if _, statErr := os.Stat(supervisor.InstanceDir(cache, supervisor.InstanceKey(root))); statErr == nil {
		t.Error("connecting must not create an instance directory")
	}
	if g, ok := host.CallGraphFor("a").(unavailableGraph); !ok || !strings.Contains(g.err.Error(), MsgManagedNotRunning) {
		t.Errorf("CallGraphFor(a) = %#v, want a graph that says why", host.CallGraphFor("a"))
	}
	if host.CallGraphFor("nope") != nil {
		t.Error("a repository without an instance has no graph")
	}

	none := InstallDiscoveredSemanticSearch(cache, vault.NewWorkspace(), []SemanticRepo{{ID: "a", Path: root, Role: roleProject, Vault: v}}, nil)
	if none.discovered != nil {
		t.Error("no opted-in repository must leave the host empty")
	}
}

// TestManagedControlRoutes covers POST /api/v1/search/managed/{repo}/{verb}.
func TestManagedControlRoutes(t *testing.T) {
	t.Parallel()

	post := func(rig *managedRig, repo, verb string) (int, string) {
		rec := send(t, rig.s, request{method: http.MethodPost, target: "/api/v1/search/managed/" + repo + "/" + verb})
		return rec.Code, rec.Body.String()
	}
	tests := []struct {
		name string
		opts managedRigOptions
		repo string
		verb string
		want int
		// check runs after the call.
		check func(t *testing.T, rig *managedRig, body string)
	}{
		{"start an opted-out repository", managedRigOptions{repos: []string{"a", "b"}, optIn: []string{"a"}, binary: "/bin/pando"},
			"b", "start", http.StatusOK, func(t *testing.T, rig *managedRig, _ string) {
				eventually(t, "the instance to start", func() bool { s, _, _ := rig.inst["b"].counts(); return s == 1 })
			}},
		{"stop keeps the data", managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/bin/pando"},
			"a", "stop", http.StatusOK, func(t *testing.T, rig *managedRig, body string) {
				if _, stops, _ := rig.inst["a"].counts(); stops != 1 {
					t.Errorf("stops = %d, want 1", stops)
				}
				if rig.s.search.managed.slot("a") != nil {
					t.Error("a stopped repository keeps no slot")
				}
				if strings.Contains(body, "instance-token") {
					t.Error("the answer carries the token")
				}
			}},
		{"restart", managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/bin/pando"},
			"a", "restart", http.StatusOK, func(t *testing.T, rig *managedRig, _ string) {
				if _, _, r := rig.inst["a"].counts(); r != 1 {
					t.Errorf("restarts = %d, want 1", r)
				}
			}},
		{"restart with no instance", managedRigOptions{repos: []string{"a", "b"}, optIn: []string{"a"}, binary: "/bin/pando"},
			"b", "restart", http.StatusConflict, nil},
		{"unknown repository", managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/bin/pando"},
			"zzz", "stop", http.StatusNotFound, nil},
		{"not in managed mode", managedRigOptions{repos: []string{"a"}, mode: "external", mcpURL: "http://127.0.0.1:9/mcp"},
			"a", "start", http.StatusBadRequest, nil},
		{"reset deletes the index and starts again", managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/bin/pando"},
			"a", "reset", http.StatusOK, func(t *testing.T, rig *managedRig, _ string) {
				dir := supervisor.InstanceDir(rig.s.search.managed.cacheDir, supervisor.InstanceKey(rig.roots["a"]))
				if _, err := os.Stat(supervisor.DataDir(dir)); err == nil {
					t.Error("the data directory survived a reset")
				}
				if _, err := os.Stat(filepath.Join(dir, "keep")); err != nil {
					t.Errorf("reset must only delete the data directory: %v", err)
				}
				eventually(t, "the instance to start again", func() bool { s, _, _ := rig.inst["a"].counts(); return s == 2 })
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rig := newManagedRig(t, tc.opts)
			if tc.opts.mode != "external" {
				rig.begin(t)
				if tc.opts.optIn != nil {
					eventually(t, "the opted-in instance to start", func() bool {
						s, _, _ := rig.inst[tc.opts.optIn[0]].counts()
						return s == 1
					})
				}
			}
			if tc.verb == "reset" {
				dir := supervisor.InstanceDir(rig.s.search.managed.cacheDir, supervisor.InstanceKey(rig.roots["a"]))
				for _, f := range []string{"data/index.db", "keep"} {
					if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			code, body := post(rig, tc.repo, tc.verb)
			if code != tc.want {
				t.Fatalf("status = %d, want %d: %s", code, tc.want, body)
			}
			if tc.check != nil {
				tc.check(t, rig, body)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
