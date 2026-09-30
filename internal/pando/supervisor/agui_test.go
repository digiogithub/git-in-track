package supervisor

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// aguiFiles is an Options.Files that records the port it was called with.
func aguiFiles(ports *[]int) func(int) (map[string][]byte, error) {
	return func(port int) (map[string][]byte, error) {
		*ports = append(*ports, port)
		return map[string][]byte{
			".pando.toml":          []byte("[AGUI]\nPort = " + strconv.Itoa(port) + "\n"),
			"agents/personas/x.md": []byte("persona\n"),
			"agents/skills/s/S.md": []byte("skill\n"),
		}, nil
	}
}

func get(t *testing.T, url, bearer string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// Verifies: GIT-US-0185 (AG-UI instance lifecycle)
func TestAGUIInstance(t *testing.T) {
	t.Run("starts agui-serve with the instance token and rendered files", func(t *testing.T) {
		var ports []int
		r := newRig(t, nil, func(o *Options) { o.Kind, o.Files = KindAGUI, aguiFiles(&ports) })
		r.start()
		st := r.waitState(StateReady)

		if st.Kind != KindAGUI || st.MCPURL != "" || st.AGUIURL != "http://127.0.0.1:"+strconv.Itoa(st.Port) {
			t.Errorf("status = %+v, want an AG-UI endpoint and no MCP one", st)
		}
		if want := AGUIKey(r.repo); st.Key != want || !strings.HasSuffix(st.Key, "-agui") {
			t.Errorf("key = %q, want %q", st.Key, want)
		}
		url, token, ok := r.sup.Endpoint()
		if !ok || url != st.AGUIURL || token == "" {
			t.Fatalf("Endpoint() = %q, %q, %v", url, token, ok)
		}
		if got := get(t, url+"/api/v1/agui/info", token); got != http.StatusOK {
			t.Errorf("/info with the token = %d, want 200", got)
		}
		if got := get(t, url+"/api/v1/agui/info", ""); got != http.StatusUnauthorized {
			t.Errorf("/info without the token = %d, want 401", got)
		}

		starts := r.starts()
		if len(starts) != 1 {
			t.Fatalf("starts = %d, want 1", len(starts))
		}
		a := strings.Join(starts[0].Args, " ")
		for _, want := range []string{"agui-serve", "--cwd " + r.sup.Dir(), "--host 127.0.0.1", "--port " + strconv.Itoa(st.Port),
			"--no-tls", "--token-file " + filepath.Join(r.sup.Dir(), "token")} {
			if !strings.Contains(a, want) {
				t.Errorf("args = %q, missing %q", a, want)
			}
		}
		if strings.Contains(a, token) {
			t.Error("the token is on the command line")
		}
		if starts[0].Cwd != mustEval(t, r.sup.Dir()) || starts[0].ParentSearch != "false" {
			t.Errorf("cwd = %q parentSearch = %q, want the instance dir and false", starts[0].Cwd, starts[0].ParentSearch)
		}
		if len(ports) != 1 || ports[0] != st.Port {
			t.Errorf("Files called with %v, want the child's port %d", ports, st.Port)
		}
		if runtime.GOOS != "windows" {
			for _, f := range []string{".pando.toml", "agents/personas/x.md", "agents/skills/s/S.md", "token"} {
				fi, err := os.Stat(filepath.Join(r.sup.Dir(), filepath.FromSlash(f)))
				if err != nil {
					t.Fatalf("stat %s: %v", f, err)
				}
				if fi.Mode().Perm() != 0o600 {
					t.Errorf("%s mode = %v, want 0600", f, fi.Mode().Perm())
				}
			}
		}
		if entries, _ := os.ReadDir(r.repo); len(entries) != 1 || entries[0].Name() != "docs" {
			t.Errorf("the repository was written to: %v", entries)
		}
		disk, err := ReadStatus(r.sup.Dir())
		if err != nil || disk.AGUIURL != st.AGUIURL || disk.Kind != KindAGUI {
			t.Errorf("state.json = %+v, %v", disk, err)
		}
	})

	t.Run("runs beside the MCP instance of the same repository", func(t *testing.T) {
		var ports []int
		r := newRig(t, nil, func(o *Options) { o.Kind, o.Files = KindAGUI, aguiFiles(&ports) })
		mcp, err := New(Options{
			Binary: fakeBinary, CacheDir: r.cache, RepoRoot: r.repo, ReadyTimeout: 3 * time.Second,
			HealthInterval: 100 * time.Millisecond, StopTimeout: 2 * time.Second,
			ExtraEnv: []string{"FAKE_PANDO_RECORD=" + r.record},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = mcp.Stop(context.Background()) })
		r.start()
		if err := mcp.Start(context.Background()); err != nil {
			t.Fatalf("mcp Start() = %v, want no lock clash with the AG-UI instance", err)
		}
		r.waitState(StateReady)
		waitFor(t, 10*time.Second, "the MCP instance", func() bool { return mcp.Status().State == StateReady })
		if mcp.Dir() == r.sup.Dir() {
			t.Error("both instances share a directory")
		}
	})

	t.Run("a port that does not answer to our token is a crash", func(t *testing.T) {
		var ports []int
		r := newRig(t, []string{"FAKE_PANDO_AGUI_NO_PORT=1"}, func(o *Options) {
			o.Kind, o.Files = KindAGUI, aguiFiles(&ports)
			o.ReadyTimeout, o.MaxCrashes = 300*time.Millisecond, 2
		})
		r.start()
		st := r.waitState(StateFailed)
		if !strings.Contains(st.LastError, "not healthy on port") {
			t.Errorf("LastError = %q, want the port explanation", st.LastError)
		}
	})

	t.Run("crashes are retried with the same rules", func(t *testing.T) {
		var ports []int
		r := newRig(t, []string{"FAKE_PANDO_CRASH_RUNS=2"}, func(o *Options) { o.Kind, o.Files = KindAGUI, aguiFiles(&ports) })
		r.start()
		st := r.waitState(StateReady)
		if len(r.starts()) != 3 || st.Crashes != 2 {
			t.Errorf("starts = %d crashes = %d, want 3 and 2", len(r.starts()), st.Crashes)
		}
		if len(ports) != 3 {
			t.Errorf("Files called %d times, want once per start", len(ports))
		}
	})

	t.Run("a Files error is a crash that says why", func(t *testing.T) {
		r := newRig(t, nil, func(o *Options) {
			o.Kind = KindAGUI
			o.Files = func(int) (map[string][]byte, error) { return map[string][]byte{"x": nil}, nil }
			o.MaxCrashes = 2
		})
		r.start()
		st := r.waitState(StateFailed)
		if !strings.Contains(st.LastError, "no .pando.toml") {
			t.Errorf("LastError = %q", st.LastError)
		}
	})

	t.Run("the AG-UI port is reused across restarts like the MCP one (GIT-US-0184)", func(t *testing.T) {
		var ports []int
		r := newRig(t, nil, func(o *Options) { o.Kind, o.Files = KindAGUI, aguiFiles(&ports) })
		r.start()
		first := r.waitState(StateReady)
		if err := r.sup.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		r.start()
		if st := r.waitState(StateReady); st.Port != first.Port || st.AGUIURL != first.AGUIURL {
			t.Errorf("endpoint after a restart = %s (port %d), want %s (port %d)", st.AGUIURL, st.Port, first.AGUIURL, first.Port)
		}
	})

	t.Run("stop ends the child and reports stopped", func(t *testing.T) {
		var ports []int
		r := newRig(t, nil, func(o *Options) { o.Kind, o.Files = KindAGUI, aguiFiles(&ports) })
		r.start()
		r.waitState(StateReady)
		pid := r.pids()[0]
		if err := r.sup.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if st := r.sup.Status(); st.State != StateStopped || st.AGUIURL != "" {
			t.Errorf("status = %+v, want stopped and no endpoint", st)
		}
		waitFor(t, 5*time.Second, "the child to be gone", func() bool { return !PIDAlive(pid) })
	})
}

func TestAGUIOptions(t *testing.T) {
	base := func() Options {
		return Options{Binary: "x", CacheDir: filepath.Join(t.TempDir(), "c"), RepoRoot: filepath.Join(t.TempDir(), "r")}
	}
	t.Run("an AG-UI instance needs Files", func(t *testing.T) {
		o := base()
		o.Kind = KindAGUI
		if _, err := New(o); err == nil {
			t.Error("New() = nil, want an error")
		}
	})
	t.Run("an unknown kind is refused", func(t *testing.T) {
		o := base()
		o.Kind = "bogus"
		if _, err := New(o); err == nil {
			t.Error("New() = nil, want an error")
		}
	})
	t.Run("a file name that escapes the directory is refused", func(t *testing.T) {
		o := base()
		o.Kind = KindAGUI
		o.Files = func(int) (map[string][]byte, error) {
			return map[string][]byte{".pando.toml": nil, "../evil": []byte("x")}, nil
		}
		s, err := New(o)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(s.Dir(), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := s.prepareRun(1234); err == nil || !strings.Contains(err.Error(), "bad file name") {
			t.Errorf("prepareRun() = %v, want a bad file name error", err)
		}
	})
}
