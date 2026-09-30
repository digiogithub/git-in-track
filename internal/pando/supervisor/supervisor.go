package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/pando"
)

// Errors a caller distinguishes.
var (
	// ErrLocked means another process supervises this instance already. Connect
	// to it through ReadStatus and ReadToken instead of starting a second one.
	ErrLocked = errors.New("supervisor: the instance is supervised by another process")
	// ErrBinary means the pando binary is missing or unusable.
	ErrBinary = errors.New("supervisor: no usable pando binary")
	// ErrVersion means the binary is older than Options.MinVersion.
	ErrVersion = errors.New("supervisor: pando is older than the required version")
	// ErrStarted is returned by Start on a Supervisor that already started.
	ErrStarted = errors.New("supervisor: already started")
)

// Options describes one managed instance. It is a plain struct on purpose: the
// configuration file (GIT-US-0174) and `gintrack serve` (GIT-US-0175) fill it
// in, and this package depends on neither. Zero values take the defaults noted.
type Options struct {
	// Binary is the pando executable: an absolute path or a name on PATH.
	// Required.
	Binary string
	// CacheDir is the gintrack cache directory; the instance lives in
	// <CacheDir>/pando/<Key>. Required, and must not be inside RepoRoot.
	CacheDir string
	// RepoRoot is the repository the instance indexes. Required.
	RepoRoot string
	// DocsFolder is the knowledge-base folder inside the repository. Default
	// "docs".
	DocsFolder string
	// Key names the instance directory. Default InstanceKey(RepoRoot).
	Key string
	// PortMin and PortMax bound the port choice, inclusive. Both zero means any
	// free loopback port.
	PortMin, PortMax int
	// MinVersion is the oldest acceptable "x.y.z"; empty accepts any.
	MinVersion string
	// Debug adds --debug to the child's command line.
	Debug bool
	// ExtraEnv is appended to the child's environment ("K=V"), after the
	// supervisor's own PANDO_CONFIG_PARENT_SEARCH=false.
	ExtraEnv []string
	// Watchdog is the command prefix that runs the child under a parent-death
	// watchdog (RunWatchdog): the child is started as `<Watchdog...> <binary>
	// <args...>` with the read end of a lifeline pipe as descriptor 3, and dies
	// when the supervisor does, even on SIGKILL. Default: none on Linux (which
	// has Pdeathsig), this binary's `__pando-watch` on other unix systems such
	// as macOS, none on Windows.
	Watchdog []string

	// PortWait bounds how long a start waits for the previous port to be
	// released (the old child may still be closing its socket) before falling
	// back to another port. Default 2s.
	PortWait time.Duration
	// ReadyTimeout bounds the wait for a fresh child to pass Health. Default 30s.
	ReadyTimeout time.Duration
	// HealthInterval is the pause between health checks of a ready child.
	// Default 30s. HealthTimeout bounds one check (default 5s) and
	// HealthFailures consecutive failures count as a crash (default 3).
	HealthInterval time.Duration
	HealthTimeout  time.Duration
	HealthFailures int
	// StopTimeout is how long SIGTERM gets before SIGKILL. Default 10s.
	StopTimeout time.Duration
	// BackoffMin and BackoffMax bound the restart delay. Defaults 1s and 60s.
	BackoffMin, BackoffMax time.Duration
	// MaxCrashes within CrashWindow mark the instance failed. Defaults 5 and
	// 10 minutes.
	MaxCrashes  int
	CrashWindow time.Duration

	// Logger receives the supervisor's own events, never the child's output nor
	// the token. Default: discard.
	Logger *slog.Logger
}

func (o *Options) defaults() error {
	if o.Binary == "" {
		return fmt.Errorf("%w: Options.Binary is empty", ErrBinary)
	}
	if o.CacheDir == "" || o.RepoRoot == "" {
		return errors.New("supervisor: Options.CacheDir and Options.RepoRoot are required")
	}
	o.CacheDir, o.RepoRoot = absPath(o.CacheDir), absPath(o.RepoRoot)
	if rel, err := filepath.Rel(o.RepoRoot, o.CacheDir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("supervisor: CacheDir is inside the repository; nothing may be written there")
	}
	if o.PortMin < 0 || o.PortMax < o.PortMin || o.PortMax > 65535 {
		return errors.New("supervisor: invalid port range")
	}
	if o.DocsFolder == "" {
		o.DocsFolder = "docs"
	}
	if o.Key == "" {
		o.Key = InstanceKey(o.RepoRoot)
	}
	dur := func(p *time.Duration, d time.Duration) {
		if *p <= 0 {
			*p = d
		}
	}
	dur(&o.PortWait, 2*time.Second)
	dur(&o.ReadyTimeout, 30*time.Second)
	dur(&o.HealthInterval, 30*time.Second)
	dur(&o.HealthTimeout, 5*time.Second)
	dur(&o.StopTimeout, 10*time.Second)
	dur(&o.BackoffMin, time.Second)
	dur(&o.BackoffMax, 60*time.Second)
	dur(&o.CrashWindow, 10*time.Minute)
	if o.HealthFailures <= 0 {
		o.HealthFailures = 3
	}
	if o.MaxCrashes <= 0 {
		o.MaxCrashes = 5
	}
	if o.BackoffMax < o.BackoffMin {
		o.BackoffMax = o.BackoffMin
	}
	if o.Watchdog == nil {
		o.Watchdog = defaultWatchdog()
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return nil
}

// Supervisor runs one managed Pando instance. Its methods are safe for
// concurrent use.
type Supervisor struct {
	opts    Options
	dir     string
	project string

	mu      sync.Mutex
	status  Status
	started bool
	lock    *dirLock
	cancel  context.CancelFunc
	done    chan struct{}
	restart chan struct{}
	crashes []time.Time
	token   string
}

// New validates opts and returns a Supervisor. It touches nothing on disk.
func New(opts Options) (*Supervisor, error) {
	if err := opts.defaults(); err != nil {
		return nil, err
	}
	dir := InstanceDir(opts.CacheDir, opts.Key)
	s := &Supervisor{
		opts:    opts,
		dir:     dir,
		project: pando.SanitizeProjectID(opts.RepoRoot),
		restart: make(chan struct{}, 1),
	}
	s.status = Status{
		State: StateStopped, Key: opts.Key, Root: opts.RepoRoot, Project: s.project,
		TokenFile: filepath.Join(dir, tokenFileName), Binary: opts.Binary, Since: time.Now().UTC(),
	}
	return s, nil
}

// Dir is the instance directory.
func (s *Supervisor) Dir() string { return s.dir }

// Status returns a snapshot of the instance's state.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.LastStderr = append([]string(nil), st.LastStderr...)
	return st
}

// Endpoint returns the MCP URL and bearer token of the running child, for a
// pando.Client. ok is false until the instance is ready.
func (s *Supervisor) Endpoint() (mcpURL, token string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State != StateReady {
		return "", "", false
	}
	return s.status.MCPURL, s.token, true
}

// update mutates the status under the lock and persists it. A failed write is
// logged and never fatal: state.json is derived data.
func (s *Supervisor) update(fn func(*Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.status.State
	fn(&s.status)
	if s.status.State != prev {
		s.status.Since = time.Now().UTC()
	}
	if err := s.writeStateLocked(); err != nil {
		s.opts.Logger.Warn("write state.json", "err", err)
	}
}

func (s *Supervisor) writeStateLocked() error {
	b, err := marshalStatus(s.status)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.dir, stateFileName), b, 0o600)
}

// Start takes the instance lock, checks the binary and starts supervising in
// the background; it does not wait for the child to become ready. It returns
// ErrLocked when another process owns the instance, ErrBinary or ErrVersion when
// the binary cannot be used (the state file then says why), and ErrStarted when
// called twice. After Stop a Supervisor can be started again.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrStarted
	}
	s.started = true
	s.mu.Unlock()
	ok := false
	defer func() {
		if !ok {
			s.mu.Lock()
			s.started = false
			s.mu.Unlock()
		}
	}()

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create the instance directory: %w", err)
	}
	_ = os.Chmod(s.dir, 0o700)
	lock, err := acquireLock(filepath.Join(s.dir, lockFileName))
	if err != nil {
		return err
	}
	fail := func(err error) error {
		s.mu.Lock()
		s.status.SupervisorPID = os.Getpid()
		s.mu.Unlock()
		s.update(func(st *Status) { st.State, st.LastError, st.PID = StateFailed, err.Error(), 0 })
		lock.release()
		return err
	}

	// The port of the last run survives in state.json; a fresh Supervisor
	// (a new gintrack process) reads it back and tries it first.
	if prev, err := ReadStatus(s.dir); err == nil && prev.LastPort > 0 {
		s.mu.Lock()
		s.status.LastPort = prev.LastPort
		s.mu.Unlock()
	}
	s.reapOrphan(ctx)
	version, err := s.checkBinary(ctx)
	if err != nil {
		return fail(err)
	}
	token, err := newToken()
	if err != nil {
		return fail(err)
	}
	if err := writeFileAtomic(filepath.Join(s.dir, tokenFileName), []byte(token+"\n"), 0o600); err != nil {
		return fail(fmt.Errorf("write the token file: %w", err))
	}
	sink, err := newLogSink(s.dir, token)
	if err != nil {
		return fail(fmt.Errorf("open the log file: %w", err))
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	s.lock, s.cancel, s.token = lock, cancel, token
	s.done = make(chan struct{})
	s.crashes = nil
	s.mu.Unlock()
	s.update(func(st *Status) {
		*st = Status{
			State: StateStarting, Key: st.Key, Root: st.Root, Project: st.Project, LastPort: st.LastPort,
			TokenFile: st.TokenFile, Binary: st.Binary, Version: version, SupervisorPID: os.Getpid(),
		}
	})
	ok = true
	go s.loop(runCtx, sink)
	return nil
}

// checkBinary runs `pando --version` and applies MinVersion.
func (s *Supervisor) checkBinary(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.opts.Binary, "--version")
	cmd.Env = append(os.Environ(), s.opts.ExtraEnv...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrBinary, err)
	}
	v, line, err := parseVersion(string(out))
	if err != nil {
		return line, fmt.Errorf("%w: %w", ErrBinary, err)
	}
	if s.opts.MinVersion != "" {
		floor, _, merr := parseVersion(s.opts.MinVersion)
		if merr != nil {
			return line, fmt.Errorf("supervisor: invalid MinVersion %q", s.opts.MinVersion)
		}
		if versionLess(v, floor) {
			return line, fmt.Errorf("%w: found %s, need %s", ErrVersion, line, s.opts.MinVersion)
		}
	}
	return line, nil
}

// Restart clears a failed instance and starts it again at once, or bounces a
// running child without counting a crash. It does nothing on a stopped
// Supervisor.
func (s *Supervisor) Restart() {
	s.mu.Lock()
	running := s.started && s.cancel != nil
	if running {
		s.crashes = nil
	}
	s.mu.Unlock()
	if !running {
		return
	}
	select {
	case s.restart <- struct{}{}:
	default:
	}
}

// Stop ends the child (SIGTERM to its process group, SIGKILL after
// StopTimeout), records the state as stopped and releases the lock. It is
// idempotent. ctx only bounds the wait.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for the child to stop: %w", ctx.Err())
	}
}

// loop is the supervision goroutine. It is locked to its OS thread because
// Pdeathsig fires when the thread that started the child exits, not when the
// process does.
func (s *Supervisor) loop(ctx context.Context, sink *logSink) {
	runtime.LockOSThread()
	defer func() {
		sink.Close()
		s.update(func(st *Status) { st.State, st.PID, st.Port, st.MCPURL = StateStopped, 0, 0, "" })
		s.mu.Lock()
		lock, done := s.lock, s.done
		s.lock, s.cancel, s.started, s.done = nil, nil, false, nil
		s.mu.Unlock()
		lock.release()
		close(done)
	}()

	avoid := 0
	for ctx.Err() == nil {
		reason, unhealthy, counted := s.runOnce(ctx, sink, avoid)
		avoid = unhealthy
		if ctx.Err() != nil {
			return
		}
		if !counted { // a requested restart
			s.update(func(st *Status) { st.State = StateRestarting })
			continue
		}
		delay, failed := s.recordCrash(reason, sink.Tail())
		if failed {
			s.opts.Logger.Error("managed pando marked failed", "key", s.opts.Key, "reason", reason)
			select {
			case <-ctx.Done():
				return
			case <-s.restart:
				s.update(func(st *Status) { st.State, st.Crashes = StateRestarting, 0 })
				continue
			}
		}
		s.opts.Logger.Warn("managed pando crashed", "key", s.opts.Key, "reason", reason, "retryIn", delay)
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-s.restart:
			t.Stop()
			s.mu.Lock()
			s.crashes = nil
			s.mu.Unlock()
		case <-t.C:
		}
	}
}

// recordCrash adds a crash, prunes the window and returns the backoff delay and
// whether the instance is now failed.
func (s *Supervisor) recordCrash(reason string, tail []string) (time.Duration, bool) {
	now := time.Now()
	s.mu.Lock()
	kept := s.crashes[:0]
	for _, c := range s.crashes {
		if now.Sub(c) < s.opts.CrashWindow {
			kept = append(kept, c)
		}
	}
	kept = append(kept, now)
	s.crashes = kept
	n := len(s.crashes)
	s.mu.Unlock()

	failed := n >= s.opts.MaxCrashes
	s.update(func(st *Status) {
		st.PID, st.Crashes, st.LastError, st.LastStderr = 0, n, reason, tail
		st.State = StateRestarting
		if failed {
			st.State = StateFailed
		}
	})
	delay := s.opts.BackoffMin
	for i := 1; i < n && delay < s.opts.BackoffMax; i++ {
		delay *= 2
	}
	return min(delay, s.opts.BackoffMax), failed
}

// runOnce starts one child and supervises it until it ends. counted is false
// when the end was a requested restart or a shutdown. avoid is a port on which
// the previous child never became healthy (it moved silently); unhealthy is the
// same for this child, 0 when the port did work.
func (s *Supervisor) runOnce(ctx context.Context, sink *logSink, avoid int) (reason string, unhealthy int, counted bool) {
	s.mu.Lock()
	preferred := s.status.LastPort
	s.mu.Unlock()
	if preferred == avoid {
		preferred = 0
	}
	port, err := s.choosePort(ctx, preferred, avoid)
	if err != nil {
		return err.Error(), 0, true
	}
	changedFrom := 0
	if preferred > 0 && port != preferred {
		changedFrom = preferred
		s.opts.Logger.Warn("managed pando cannot reuse its previous port; using another", "key", s.opts.Key, "previous", preferred, "port", port)
	}
	cfg := generatedConfig(s.opts, s.dir, port, s.token)
	if err := writeFileAtomic(filepath.Join(s.dir, configFileName), cfg, 0o600); err != nil {
		return "write .pando.toml: " + err.Error(), 0, true
	}

	args := []string{"mcp-server", "--no-stdio", "--cwd", s.dir}
	if s.opts.Debug {
		args = append(args, "--debug")
	}
	cmd := exec.CommandContext(context.WithoutCancel(ctx), s.opts.Binary, args...)
	cmd.Dir = s.dir
	cmd.Env = append(os.Environ(), "PANDO_CONFIG_PARENT_SEARCH=false")
	cmd.Env = append(cmd.Env, s.opts.ExtraEnv...)
	cmd.Stdout, cmd.Stderr = sink, sink
	cmd.WaitDelay = 2 * time.Second
	life, err := prepareCmd(cmd, s.opts.Watchdog)
	if err != nil {
		return "prepare pando: " + err.Error(), 0, true
	}
	if err := cmd.Start(); err != nil {
		life.Close()
		return "start pando: " + err.Error(), 0, true
	}
	life.started()
	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		life.Close()
		exited <- err
	}()

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/mcp"
	s.update(func(st *Status) {
		st.State, st.PID, st.Port, st.MCPURL = StateStarting, cmd.Process.Pid, port, url
		st.LastPort, st.PortChangedFrom = port, changedFrom
	})

	stop := func() {
		signalGroup(cmd, false)
		t := time.NewTimer(s.opts.StopTimeout)
		defer t.Stop()
		select {
		case <-exited:
		case <-t.C:
			signalGroup(cmd, true)
			<-exited
		}
	}

	client, err := pando.New(pando.Options{MCPURL: url, Token: s.token, Timeout: s.opts.HealthTimeout})
	if err != nil {
		stop()
		return "build the health client: " + err.Error(), 0, true
	}
	defer func() { _ = client.Close() }()

	// Wait for ready.
	deadline := time.NewTimer(s.opts.ReadyTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	var lastErr error
	for ready := false; !ready; {
		select {
		case <-ctx.Done():
			stop()
			return "", 0, false
		case <-s.restart:
			stop()
			return "", 0, false
		case err := <-exited:
			return fmt.Sprintf("pando exited while starting: %v", err), 0, true
		case <-deadline.C:
			stop()
			return fmt.Sprintf("pando was not healthy on port %d within %s (it may have moved to another port): %v", port, s.opts.ReadyTimeout, lastErr), port, true
		case <-poll.C:
			hctx, cancel := context.WithTimeout(ctx, s.opts.HealthTimeout)
			lastErr = client.Health(hctx)
			cancel()
			ready = lastErr == nil
		}
	}
	s.update(func(st *Status) { st.State, st.LastError = StateReady, "" })

	// Monitor.
	tick := time.NewTicker(s.opts.HealthInterval)
	defer tick.Stop()
	fails := 0
	for {
		select {
		case <-ctx.Done():
			stop()
			return "", 0, false
		case <-s.restart:
			stop()
			return "", 0, false
		case err := <-exited:
			return fmt.Sprintf("pando exited: %v", err), 0, true
		case <-tick.C:
			hctx, cancel := context.WithTimeout(ctx, s.opts.HealthTimeout)
			err := client.Health(hctx)
			cancel()
			if ctx.Err() != nil {
				continue
			}
			if err == nil {
				fails = 0
				continue
			}
			fails++
			if fails >= s.opts.HealthFailures {
				stop()
				return fmt.Sprintf("%d health checks failed in a row: %v", fails, err), 0, true
			}
		}
	}
}

// choosePort returns the port for the next child. It tries preferred first (the
// last port, so agents that connect to Pando directly keep working), retrying
// for up to PortWait because the previous child may not have released it yet.
// A busy or out-of-range preferred port falls back to pickPort.
func (s *Supervisor) choosePort(ctx context.Context, preferred, avoid int) (int, error) {
	inRange := s.opts.PortMin == 0 && s.opts.PortMax == 0 ||
		preferred >= s.opts.PortMin && preferred <= s.opts.PortMax
	if preferred > 0 && inRange {
		deadline := time.Now().Add(s.opts.PortWait)
		for {
			if portFree(ctx, preferred) {
				return preferred, nil
			}
			if ctx.Err() != nil || !time.Now().Before(deadline) {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return pickPort(ctx, s.opts.PortMin, s.opts.PortMax, max(avoid, preferred))
}

// portFree reports whether a loopback port can be bound right now.
func portFree(ctx context.Context, p int) bool {
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// pickPort binds a loopback port and releases it. With no range it asks the
// kernel; with one it tries every port from a random start. avoid is the port of
// the previous child, skipped when another is free.
func pickPort(ctx context.Context, lo, hi, avoid int) (int, error) {
	try := func(p int) (int, bool) {
		l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err != nil {
			return 0, false
		}
		addr, _ := l.Addr().(*net.TCPAddr)
		if addr == nil {
			_ = l.Close()
			return 0, false
		}
		got := addr.Port
		_ = l.Close()
		return got, true
	}
	if lo == 0 && hi == 0 {
		for i := 0; i < 5; i++ {
			if p, ok := try(0); ok && p != avoid {
				return p, nil
			}
		}
		return 0, errors.New("no free loopback port")
	}
	n := hi - lo + 1
	start := rand.IntN(n)
	fallback := 0
	for i := 0; i < n; i++ {
		p := lo + (start+i)%n
		if got, ok := try(p); ok {
			if got != avoid {
				return got, nil
			}
			fallback = got
		}
	}
	if fallback != 0 {
		return fallback, nil
	}
	return 0, fmt.Errorf("no free loopback port in %d-%d", lo, hi)
}

// reapOrphan ends a child left behind by a previous supervisor that died
// without stopping it. The caller holds the instance lock, so no live
// supervisor owns state.json: a recorded child pid that is still alive, and
// whose command line names this instance directory, is an orphan. The command
// line check keeps a recycled pid from being killed. It runs on every platform
// (GIT-US-0187); the watchdog and Pdeathsig make it a safety net, not the
// normal path.
func (s *Supervisor) reapOrphan(ctx context.Context) {
	st, err := ReadStatus(s.dir)
	if err != nil || st.PID <= 0 || st.PID == os.Getpid() || !PIDAlive(st.PID) {
		return
	}
	cmdline, err := processCommand(ctx, st.PID)
	if err != nil || !strings.Contains(cmdline, s.dir) {
		return
	}
	s.opts.Logger.Warn("ending a managed pando left by a previous supervisor", "key", s.opts.Key, "pid", st.PID, "supervisorPid", st.SupervisorPID)
	killOrphan(st.PID, s.opts.StopTimeout)
}
