package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

// writeOptInConfig writes a configuration file registering the given ids.
func writeOptInConfig(t *testing.T, ids ...string) string {
	t.Helper()
	cfg := config.Default()
	for _, id := range ids {
		cfg.Repos = append(cfg.Repos, config.Repo{ID: id, Path: "/tmp/" + id, Role: "project", Enabled: true})
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	return path
}

func optedIn(t *testing.T, path, id string) bool {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	for _, r := range cfg.Repos {
		if r.ID == id {
			return r.SemanticSearch
		}
	}
	t.Fatalf("repo %s missing", id)
	return false
}

// TestManagedOptInRoute covers PUT /api/v1/search/managed/{repo}/opt-in (GIT-US-0177).
func TestManagedOptInRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		opts       func(path string) managedRigOptions
		repo       string
		body       map[string]any
		prepare    func(t *testing.T, rig *managedRig, path string)
		wantStatus int
		check      func(t *testing.T, rig *managedRig, path string, resp managedOptInResponse)
	}{
		{
			name: "enable persists and starts the instance",
			opts: func(p string) managedRigOptions {
				return managedRigOptions{repos: []string{"a", "b"}, binary: "/bin/pando", configPath: p}
			},
			repo: "a", body: map[string]any{"enabled": true}, wantStatus: http.StatusOK,
			check: func(t *testing.T, rig *managedRig, path string, resp managedOptInResponse) {
				if !resp.OptedIn || !resp.Persisted || !optedIn(t, path, "a") || optedIn(t, path, "b") {
					t.Errorf("response = %+v, file opted in a=%v b=%v", resp, optedIn(t, path, "a"), optedIn(t, path, "b"))
				}
				eventually(t, "the instance to start", func() bool {
					starts, _, _ := rig.inst["a"].counts()
					return starts == 1
				})
			},
		},
		{
			name: "disable stops the instance and keeps the data dir",
			opts: func(p string) managedRigOptions {
				return managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/bin/pando", configPath: p}
			},
			repo: "a", body: map[string]any{"enabled": false}, wantStatus: http.StatusOK,
			prepare: func(t *testing.T, rig *managedRig, path string) {
				if err := config.SetSemanticSearch(path, "a", true); err != nil {
					t.Fatal(err)
				}
				dir := supervisor.InstanceDir(rig.s.search.managed.cacheDir, supervisor.InstanceKey(rig.roots["a"]))
				if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, rig *managedRig, path string, resp managedOptInResponse) {
				if resp.OptedIn || resp.IndexDeleted || optedIn(t, path, "a") || resp.Managed.State != "disabled" {
					t.Errorf("response = %+v", resp)
				}
				if _, stops, _ := rig.inst["a"].counts(); stops != 1 {
					t.Errorf("stops = %d, want 1", stops)
				}
				dir := supervisor.InstanceDir(rig.s.search.managed.cacheDir, supervisor.InstanceKey(rig.roots["a"]))
				if _, err := os.Stat(dir); err != nil {
					t.Errorf("the data dir was removed: %v", err)
				}
			},
		},
		{
			name: "disable with deleteIndex removes the instance directory",
			opts: func(p string) managedRigOptions {
				return managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/bin/pando", configPath: p}
			},
			repo: "a", body: map[string]any{"enabled": false, "deleteIndex": true}, wantStatus: http.StatusOK,
			prepare: func(t *testing.T, rig *managedRig, path string) {
				dir := supervisor.InstanceDir(rig.s.search.managed.cacheDir, supervisor.InstanceKey(rig.roots["a"]))
				if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, rig *managedRig, path string, resp managedOptInResponse) {
				dir := supervisor.InstanceDir(rig.s.search.managed.cacheDir, supervisor.InstanceKey(rig.roots["a"]))
				if _, err := os.Stat(dir); !os.IsNotExist(err) || !resp.IndexDeleted {
					t.Errorf("dir stat err = %v, response = %+v", err, resp)
				}
			},
		},
		{
			name: "not managed mode",
			opts: func(p string) managedRigOptions {
				return managedRigOptions{repos: []string{"a"}, mode: config.PandoModeOff, configPath: p}
			},
			repo: "a", body: map[string]any{"enabled": true}, wantStatus: http.StatusConflict,
			check: func(t *testing.T, _ *managedRig, path string, _ managedOptInResponse) {
				if optedIn(t, path, "a") {
					t.Error("the opt-in was written outside managed mode")
				}
			},
		},
		{
			name: "unknown repository",
			opts: func(p string) managedRigOptions {
				return managedRigOptions{repos: []string{"a"}, binary: "/bin/pando", configPath: p}
			},
			repo: "nope", body: map[string]any{"enabled": true}, wantStatus: http.StatusNotFound,
		},
		{
			name: "deleteIndex while enabling",
			opts: func(p string) managedRigOptions {
				return managedRigOptions{repos: []string{"a"}, binary: "/bin/pando", configPath: p}
			},
			repo: "a", body: map[string]any{"enabled": true, "deleteIndex": true}, wantStatus: http.StatusBadRequest,
		},
		{
			name: "write failure leaves the running state alone",
			opts: func(p string) managedRigOptions {
				return managedRigOptions{repos: []string{"a"}, binary: "/bin/pando", configPath: p}
			},
			repo: "a", body: map[string]any{"enabled": true}, wantStatus: http.StatusInternalServerError,
			prepare: func(t *testing.T, _ *managedRig, path string) {
				// A directory where the file is makes the read fail.
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, rig *managedRig, _ string, _ managedOptInResponse) {
				if got := rig.settings(t).row(t, "a").State; got != "disabled" {
					t.Errorf("state = %q, want disabled", got)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeOptInConfig(t, "a", "b")
			rig := newManagedRig(t, tc.opts(path))
			rig.begin(t)
			if tc.prepare != nil {
				tc.prepare(t, rig, path)
			}
			rec := send(t, rig.s, request{
				method: http.MethodPut, target: "/api/v1/search/managed/" + tc.repo + "/opt-in", body: tc.body,
			})
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			var resp managedOptInResponse
			if tc.wantStatus == http.StatusOK {
				decode(t, rec, http.StatusOK, &resp)
			}
			if tc.check != nil {
				tc.check(t, rig, path, resp)
			}
		})
	}
}

func TestSetSemanticSearch(t *testing.T) {
	t.Parallel()
	path := writeOptInConfig(t, "a")
	if err := config.SetSemanticSearch(path, "missing", true); err == nil {
		t.Fatal("an unregistered repository was accepted")
	}
	if err := config.SetSemanticSearch(path, "a", true); err != nil || !optedIn(t, path, "a") {
		t.Fatalf("SetSemanticSearch() = %v", err)
	}
}
