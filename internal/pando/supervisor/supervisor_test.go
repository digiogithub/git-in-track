package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/pando"
)

var fakeBinary string

func TestMain(m *testing.M) {
	// This test binary doubles as the watchdog and as a throwaway supervisor
	// process for the tests that kill the supervisor.
	if len(os.Args) > 1 && os.Args[1] == WatchdogCommandName {
		os.Exit(RunWatchdog(os.Args[2:]))
	}
	if os.Getenv("SUPERVISOR_TEST_HELPER") == "1" {
		os.Exit(helperSupervisor())
	}
	code := func() int {
		dir, err := os.MkdirTemp("", "fakepando-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(dir)
		fakeBinary = filepath.Join(dir, "pando")
		if runtime.GOOS == "windows" {
			fakeBinary += ".exe"
		}
		out, err := exec.CommandContext(context.Background(), "go", "build", "-o", fakeBinary, "./testdata/fakepando").CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "build the fake pando: %v\n%s", err, out)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

// rig is one supervised fake instance in temporary directories.
type rig struct {
	t      *testing.T
	sup    *Supervisor
	repo   string
	cache  string
	record string
}

// newRig builds a Supervisor with fast timings. env is "K=V" pairs for the fake.
func newRig(t *testing.T, env []string, mod func(*Options)) *rig {
	t.Helper()
	tmp := t.TempDir()
	r := &rig{t: t, repo: filepath.Join(tmp, "repo"), cache: filepath.Join(tmp, "cache"), record: filepath.Join(tmp, "record.jsonl")}
	if err := os.MkdirAll(filepath.Join(r.repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Binary: fakeBinary, CacheDir: r.cache, RepoRoot: r.repo,
		ReadyTimeout: 3 * time.Second, HealthInterval: 100 * time.Millisecond, HealthTimeout: time.Second,
		StopTimeout: 2 * time.Second, BackoffMin: 10 * time.Millisecond, BackoffMax: 40 * time.Millisecond,
		MaxCrashes: 5, CrashWindow: time.Minute,
		ExtraEnv: append([]string{"FAKE_PANDO_RECORD=" + r.record, "FAKE_PANDO_COUNTER=" + filepath.Join(tmp, "count")}, env...),
	}
	if mod != nil {
		mod(&opts)
	}
	sup, err := New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	r.sup = sup
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = sup.Stop(ctx)
		// No process may outlive the test.
		for _, p := range r.pids() {
			waitFor(t, 5*time.Second, fmt.Sprintf("pid %d to be gone", p), func() bool { return !PIDAlive(p) })
		}
	})
	return r
}

type startRecord struct {
	Args         []string `json:"args"`
	Cwd          string   `json:"cwd"`
	PID          int      `json:"pid"`
	Config       string   `json:"config"`
	ParentSearch string   `json:"parentSearch"`
	Port         int      `json:"port"`
}

func (r *rig) starts() []startRecord {
	f, err := os.Open(r.record)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []startRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var s startRecord
		if json.Unmarshal(sc.Bytes(), &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

func (r *rig) pids() []int {
	var out []int
	for _, s := range r.starts() {
		out = append(out, s.PID)
	}
	return out
}

func (r *rig) start() {
	r.t.Helper()
	if err := r.sup.Start(context.Background()); err != nil {
		r.t.Fatalf("Start() error = %v", err)
	}
}

func (r *rig) waitState(want State) Status {
	r.t.Helper()
	var st Status
	waitFor(r.t, 15*time.Second, "state "+string(want), func() bool {
		st = r.sup.Status()
		return st.State == want
	})
	return st
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", d, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartHealthy(t *testing.T) {
	r := newRig(t, nil, nil)
	r.start()
	st := r.waitState(StateReady)
	dir := r.sup.Dir()

	t.Run("the instance lives in the cache directory", func(t *testing.T) {
		want := filepath.Join(r.cache, "pando", InstanceKey(r.repo))
		if dir != want {
			t.Fatalf("Dir() = %q, want %q", dir, want)
		}
	})
	t.Run("files are private", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX modes")
		}
		for name, mode := range map[string]os.FileMode{
			"": 0o700, configFileName: 0o600, tokenFileName: 0o600, stateFileName: 0o600, lockFileName: 0o600,
		} {
			fi, err := os.Stat(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("stat %q: %v", name, err)
			}
			if fi.Mode().Perm() != mode {
				t.Errorf("%q mode = %o, want %o", name, fi.Mode().Perm(), mode)
			}
		}
	})
	starts := r.starts()
	if len(starts) != 1 {
		t.Fatalf("starts = %d, want 1", len(starts))
	}
	s := starts[0]
	t.Run("the child runs in the instance directory with the fixed flags", func(t *testing.T) {
		if got, _ := filepath.EvalSymlinks(s.Cwd); got != mustEval(t, dir) {
			t.Errorf("cwd = %q, want %q", s.Cwd, dir)
		}
		if want := []string{"mcp-server", "--no-stdio", "--cwd", dir}; strings.Join(s.Args, " ") != strings.Join(want, " ") {
			t.Errorf("args = %v, want %v", s.Args, want)
		}
		if s.ParentSearch != "false" {
			t.Errorf("PANDO_CONFIG_PARENT_SEARCH = %q, want false", s.ParentSearch)
		}
	})
	t.Run("the generated configuration follows ADR-039", func(t *testing.T) {
		token, err := ReadToken(dir)
		if err != nil || len(token) != 64 {
			t.Fatalf("ReadToken() = %q, %v; want 64 hex characters", token, err)
		}
		for _, want := range []string{
			`Directory = "` + filepath.Join(dir, "data") + `"`,
			`KBPath = "` + filepath.Join(r.repo, "docs") + `"`,
			"KBAutoImport = true", "KBWatch = true", "BuildCodeGraph = true",
			`HttpHost = "127.0.0.1"`, fmt.Sprintf("HttpPort = %d", st.Port), `HttpToken = "` + token + `"`,
			"HttpAllowedOrigins = []", "StdioEnabled = false", "[MCPServer.FileTools]", "[Mesnada]", "[APIServer]",
		} {
			if !strings.Contains(s.Config, want) {
				t.Errorf(".pando.toml lacks %q", want)
			}
		}
	})
	t.Run("state.json is current and never holds the token", func(t *testing.T) {
		got, err := ReadStatus(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != StateReady || got.PID != s.PID || got.Port != s.Port || got.SupervisorPID != os.Getpid() {
			t.Errorf("state.json = %+v, want ready pid %d port %d", got, s.PID, s.Port)
		}
		if want := fmt.Sprintf("http://127.0.0.1:%d/mcp", s.Port); got.MCPURL != want {
			t.Errorf("mcpUrl = %q, want %q", got.MCPURL, want)
		}
		if got.TokenFile != filepath.Join(dir, tokenFileName) || got.Version != "pando v1.1.1" || got.LastError != "" {
			t.Errorf("state.json = %+v", got)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, stateFileName))
		token, _ := ReadToken(dir)
		if strings.Contains(string(raw), token) {
			t.Error("state.json contains the token")
		}
	})
	t.Run("the endpoint answers to our token and to nothing else", func(t *testing.T) {
		url, token, ok := r.sup.Endpoint()
		if !ok {
			t.Fatal("Endpoint() not ok on a ready instance")
		}
		for name, tok := range map[string]string{"right token": token, "wrong token": "nope"} {
			c, err := pando.New(pando.Options{MCPURL: url, Token: tok, Timeout: 2 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			err = c.Health(context.Background())
			_ = c.Close()
			if (name == "right token") != (err == nil) {
				t.Errorf("%s: Health() error = %v", name, err)
			}
		}
	})
	t.Run("the log is redacted", func(t *testing.T) {
		token, _ := ReadToken(dir)
		waitFor(t, 3*time.Second, "the child's first log line", func() bool {
			b, _ := os.ReadFile(filepath.Join(dir, logFileName))
			return strings.Contains(string(b), "starting")
		})
		b, _ := os.ReadFile(filepath.Join(dir, logFileName))
		if strings.Contains(string(b), token) || !strings.Contains(string(b), "[redacted]") {
			t.Errorf("pando.log not redacted:\n%s", b)
		}
	})
	t.Run("nothing is written inside the repository", func(t *testing.T) {
		var found []string
		_ = filepath.WalkDir(r.repo, func(p string, d os.DirEntry, _ error) error {
			if p != r.repo && p != filepath.Join(r.repo, "docs") {
				found = append(found, p)
			}
			return nil
		})
		if len(found) != 0 {
			t.Errorf("repository was written to: %v", found)
		}
	})

	t.Run("Stop ends the child and records it", func(t *testing.T) {
		if err := r.sup.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		if PIDAlive(s.PID) {
			t.Error("child still alive after Stop")
		}
		got, _ := ReadStatus(dir)
		if got.State != StateStopped || got.PID != 0 {
			t.Errorf("state.json after Stop = %+v", got)
		}
		if err := r.sup.Stop(context.Background()); err != nil {
			t.Errorf("second Stop() error = %v", err)
		}
	})
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestLock(t *testing.T) {
	r := newRig(t, nil, nil)
	r.start()
	r.waitState(StateReady)

	t.Run("a second supervisor of the same instance is refused", func(t *testing.T) {
		other, err := New(r.sup.opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := other.Start(context.Background()); !errors.Is(err, ErrLocked) {
			t.Fatalf("Start() error = %v, want ErrLocked", err)
		}
	})
	t.Run("Start twice on one supervisor is refused", func(t *testing.T) {
		if err := r.sup.Start(context.Background()); !errors.Is(err, ErrStarted) {
			t.Fatalf("Start() error = %v, want ErrStarted", err)
		}
	})
	t.Run("the lock is free after Stop and the supervisor starts again", func(t *testing.T) {
		if err := r.sup.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
		r.start()
		r.waitState(StateReady)
	})
}

func TestPortChange(t *testing.T) {
	// Pando moved to another port without saying so: our token on our port
	// reaches nothing, so the run is a crash and the next run picks a new port.
	r := newRig(t, []string{"FAKE_PANDO_MODE=shift"}, func(o *Options) {
		o.ReadyTimeout = 300 * time.Millisecond
		o.MaxCrashes = 3
	})
	r.start()
	st := r.waitState(StateFailed)
	if st.Crashes != 3 || !strings.Contains(st.LastError, "not healthy on port") {
		t.Errorf("status = %+v, want 3 crashes and a port explanation", st)
	}
	ports := map[int]bool{}
	for _, s := range r.starts() {
		ports[s.Port] = true
	}
	if len(ports) < 2 {
		t.Errorf("ports tried = %v, want a new port for each run", ports)
	}
}

func TestCrashHandling(t *testing.T) {
	t.Run("crashes are retried and the instance recovers", func(t *testing.T) {
		r := newRig(t, []string{"FAKE_PANDO_CRASH_RUNS=2"}, nil)
		r.start()
		st := r.waitState(StateReady)
		if st.Crashes != 2 || len(r.starts()) != 3 {
			t.Errorf("crashes = %d starts = %d, want 2 and 3", st.Crashes, len(r.starts()))
		}
	})
	t.Run("five crashes in the window mark it failed until Restart", func(t *testing.T) {
		r := newRig(t, []string{"FAKE_PANDO_CRASH_RUNS=5"}, nil) // default MaxCrashes is 5
		r.start()
		st := r.waitState(StateFailed)
		if st.Crashes != 5 || st.PID != 0 || !strings.Contains(st.LastError, "exited") {
			t.Errorf("status = %+v", st)
		}
		token, _ := ReadToken(r.sup.Dir())
		if !strings.Contains(strings.Join(st.LastStderr, "\n"), "simulated crash") {
			t.Errorf("lastStderr = %q, want the child's last words", st.LastStderr)
		}
		if strings.Contains(strings.Join(st.LastStderr, "\n"), token) {
			t.Error("lastStderr holds the token")
		}
		// It stays failed: no new start without a request.
		n := len(r.starts())
		time.Sleep(200 * time.Millisecond)
		if len(r.starts()) != n {
			t.Error("a failed instance was restarted on its own")
		}
		r.sup.Restart()
		if got := r.waitState(StateReady); got.Crashes != 0 {
			t.Errorf("crashes after Restart = %d, want 0", got.Crashes)
		}
	})
	t.Run("a crash after ready is noticed", func(t *testing.T) {
		r := newRig(t, []string{"FAKE_PANDO_MODE=crashafter", "FAKE_PANDO_AFTER=200ms"}, func(o *Options) { o.MaxCrashes = 2 })
		r.start()
		st := r.waitState(StateFailed)
		if st.Crashes != 2 || len(r.starts()) != 2 {
			t.Errorf("crashes = %d starts = %d, want 2 and 2", st.Crashes, len(r.starts()))
		}
	})
}

func TestRestartBounce(t *testing.T) {
	r := newRig(t, nil, nil)
	r.start()
	first := r.waitState(StateReady)
	r.sup.Restart()
	waitFor(t, 10*time.Second, "a new child", func() bool {
		st := r.sup.Status()
		return st.State == StateReady && st.PID != first.PID
	})
	if st := r.sup.Status(); st.Crashes != 0 {
		t.Errorf("crashes = %d after a requested restart, want 0", st.Crashes)
	}
	if PIDAlive(first.PID) {
		t.Error("the old child survived the restart")
	}
}

func TestStopEscalatesToKill(t *testing.T) {
	r := newRig(t, []string{"FAKE_PANDO_IGNORE_TERM=1"}, func(o *Options) { o.StopTimeout = 300 * time.Millisecond })
	r.start()
	st := r.waitState(StateReady)
	begin := time.Now()
	if err := r.sup.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(begin) < 300*time.Millisecond {
		t.Errorf("Stop returned in %s, before SIGTERM's grace period", time.Since(begin))
	}
	if PIDAlive(st.PID) {
		t.Error("a child that ignores SIGTERM survived Stop")
	}
}

func TestBackoff(t *testing.T) {
	s, err := New(Options{
		Binary: "x", CacheDir: t.TempDir(), RepoRoot: filepath.Join(t.TempDir(), "r"),
		BackoffMin: time.Second, BackoffMax: 8 * time.Second, MaxCrashes: 6, CrashWindow: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i, want := range []time.Duration{1, 2, 4, 8, 8} {
		got, failed := s.recordCrash("boom", nil)
		if got != want*time.Second || failed {
			t.Errorf("crash %d: delay = %s failed = %v, want %s false", i+1, got, failed, want*time.Second)
		}
	}
	if _, failed := s.recordCrash("boom", nil); !failed {
		t.Error("the 6th crash inside the window did not mark the instance failed")
	}
}

func TestStartRefusals(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*Options)
		env  []string
		want error
	}{
		{"missing binary", func(o *Options) { o.Binary = filepath.Join(o.CacheDir, "nope") }, nil, ErrBinary},
		{"binary without a version", nil, []string{"FAKE_PANDO_VERSION=hello"}, ErrBinary},
		{"older than the floor", func(o *Options) { o.MinVersion = "1.2.0" }, nil, ErrVersion},
		{"exactly the floor", func(o *Options) { o.MinVersion = "1.1.1" }, nil, nil},
		{"a newer build passes", func(o *Options) { o.MinVersion = "1.0.9" }, []string{"FAKE_PANDO_VERSION=pando v1.10.0-rc1 (abc)"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, tt.env, tt.mod)
			err := r.sup.Start(context.Background())
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("Start() error = %v, want %v", err, tt.want)
			}
			if tt.want == nil {
				r.waitState(StateReady)
				return
			}
			st := r.sup.Status()
			if st.State != StateFailed || st.LastError == "" {
				t.Errorf("status = %+v, want failed with a reason", st)
			}
			if len(r.starts()) != 0 {
				t.Error("a refused binary was started")
			}
			// The lock must not stay held after a refusal.
			l, err := acquireLock(filepath.Join(r.sup.Dir(), lockFileName))
			if err != nil {
				t.Fatalf("lock still held after a refusal: %v", err)
			}
			l.release()
		})
	}
}

func TestOptionsValidation(t *testing.T) {
	tmp := t.TempDir()
	tests := []struct {
		name string
		opts Options
		ok   bool
	}{
		{"valid", Options{Binary: "pando", CacheDir: filepath.Join(tmp, "c"), RepoRoot: filepath.Join(tmp, "r")}, true},
		{"no binary", Options{CacheDir: filepath.Join(tmp, "c"), RepoRoot: filepath.Join(tmp, "r")}, false},
		{"no root", Options{Binary: "pando", CacheDir: filepath.Join(tmp, "c")}, false},
		{"cache inside the repository", Options{Binary: "pando", CacheDir: filepath.Join(tmp, "r", ".cache"), RepoRoot: filepath.Join(tmp, "r")}, false},
		{"cache is a sibling with the same prefix", Options{Binary: "pando", CacheDir: filepath.Join(tmp, "r-cache"), RepoRoot: filepath.Join(tmp, "r")}, true},
		{"inverted port range", Options{Binary: "pando", CacheDir: filepath.Join(tmp, "c"), RepoRoot: filepath.Join(tmp, "r"), PortMin: 9000, PortMax: 8000}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.opts); (err == nil) != tt.ok {
				t.Fatalf("New() error = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestPickPort(t *testing.T) {
	t.Run("any free port", func(t *testing.T) {
		p, err := pickPort(context.Background(), 0, 0, 0)
		if err != nil || p == 0 {
			t.Fatalf("pickPort() = %d, %v", p, err)
		}
	})
	t.Run("inside the range and off the avoided port", func(t *testing.T) {
		base, err := pickPort(context.Background(), 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 20; i++ {
			p, err := pickPort(context.Background(), base, base+50, base)
			if err != nil {
				t.Skipf("range not free: %v", err)
			}
			if p < base || p > base+50 {
				t.Fatalf("pickPort() = %d outside [%d, %d]", p, base, base+50)
			}
		}
	})
	t.Run("a range with no free port is an error", func(t *testing.T) {
		l, err := listenLoopback()
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		port := l.Addr().String()
		var n int
		_, _ = fmt.Sscanf(port[strings.LastIndex(port, ":")+1:], "%d", &n)
		if _, err := pickPort(context.Background(), n, n, 0); err == nil {
			t.Fatal("pickPort() on a busy single-port range succeeded")
		}
	})
}

func TestInstanceKey(t *testing.T) {
	a, b := InstanceKey("/home/x/src/app"), InstanceKey("/home/y/src/app")
	if a == b {
		t.Errorf("two clones share the key %q", a)
	}
	if !strings.HasPrefix(a, pando.SanitizeProjectID("/home/x/src/app")+"-") {
		t.Errorf("key %q does not start with the project id", a)
	}
	if InstanceKey("/home/x/src/app") != a {
		t.Error("InstanceKey is not deterministic")
	}
}

func TestLogSink(t *testing.T) {
	dir := t.TempDir()
	s, err := newLogSink(dir, "sekret")
	if err != nil {
		t.Fatal(err)
	}
	s.max = 200
	defer s.Close()

	t.Run("redacts the token and bearer credentials across writes", func(t *testing.T) {
		_, _ = s.Write([]byte("token=sek"))
		_, _ = s.Write([]byte("ret and Authorization: Bearer abc.def\n"))
		got := strings.Join(s.Tail(), "\n")
		if strings.Contains(got, "sekret") || strings.Contains(got, "abc.def") || !strings.Contains(got, "token=[redacted]") {
			t.Errorf("tail = %q", got)
		}
	})
	t.Run("keeps the last 20 lines", func(t *testing.T) {
		for i := 0; i < 50; i++ {
			fmt.Fprintf(s, "line %d\n", i)
		}
		tail := s.Tail()
		if len(tail) != 20 || tail[19] != "line 49" || tail[0] != "line 30" {
			t.Errorf("tail = %v", tail)
		}
	})
	t.Run("rotates and keeps two rotated files", func(t *testing.T) {
		for i := 0; i < 200; i++ {
			fmt.Fprintf(s, "filler line number %d\n", i)
		}
		for _, name := range []string{logFileName, logFileName + ".1", logFileName + ".2"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("missing %s: %v", name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, logFileName+".3")); err == nil {
			t.Error("kept a third rotated file")
		}
	})
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"pando v1.1.1", [3]int{1, 1, 1}, true},
		{"v2.0.3+dirty (abc)", [3]int{2, 0, 3}, true},
		{"pando 0.9.12-rc.1\nmore", [3]int{0, 9, 12}, true},
		{"hello", [3]int{}, false},
		{"", [3]int{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, _, err := parseVersion(tt.in)
			if (err == nil) != tt.ok || (tt.ok && got != tt.want) {
				t.Errorf("parseVersion(%q) = %v, %v", tt.in, got, err)
			}
		})
	}
}

// TestRealPando runs the supervisor against the real binary. It is opt-in,
// because the first start indexes and needs the user's global Pando setup:
//
//	GINTRACK_PANDO_SUPERVISOR_INTEGRATION=1 go test ./internal/pando/supervisor -run TestRealPando -v
func TestRealPando(t *testing.T) {
	if os.Getenv("GINTRACK_PANDO_SUPERVISOR_INTEGRATION") == "" {
		t.Skip("set GINTRACK_PANDO_SUPERVISOR_INTEGRATION=1 to run against a real pando")
	}
	bin, err := exec.LookPath("pando")
	if err != nil {
		t.Skip("no pando on PATH")
	}
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sup, err := New(Options{Binary: bin, CacheDir: filepath.Join(tmp, "cache"), RepoRoot: repo, ReadyTimeout: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = sup.Stop(context.Background()) })
	waitFor(t, 100*time.Second, "the real pando to be ready", func() bool {
		st := sup.Status()
		if st.State == StateFailed {
			t.Fatalf("pando failed: %s\n%s", st.LastError, strings.Join(st.LastStderr, "\n"))
		}
		return st.State == StateReady
	})
	url, token, _ := sup.Endpoint()
	c, err := pando.New(pando.Options{MCPURL: url, Token: token, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.ListProjects(context.Background()); err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	entries, _ := os.ReadDir(repo)
	for _, e := range entries {
		if e.Name() != "docs" && e.Name() != "main.go" {
			t.Errorf("the real pando wrote %q into the repository", e.Name())
		}
	}
}

func listenLoopback() (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
}
