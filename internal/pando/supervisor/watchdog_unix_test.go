//go:build unix

package supervisor

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func testWatchdog(t *testing.T) []string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{exe, WatchdogCommandName}
}

func TestWatchdog(t *testing.T) {
	t.Run("child runs under the watchdog and stops with it", func(t *testing.T) {
		wd := testWatchdog(t)
		r := newRig(t, nil, func(o *Options) { o.Watchdog = wd })
		r.start()
		st := r.waitState(StateReady)
		starts := r.starts()
		if len(starts) != 1 {
			t.Fatalf("starts = %d, want 1", len(starts))
		}
		if starts[0].PID == st.PID {
			t.Errorf("the recorded child pid %d is the watchdog's %d: no watchdog in between", starts[0].PID, st.PID)
		}
		if err := r.sup.Stop(context.Background()); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
		waitFor(t, 5*time.Second, "the child to be gone", func() bool { return !PIDAlive(starts[0].PID) })
		waitFor(t, 5*time.Second, "the watchdog to be gone", func() bool { return !PIDAlive(st.PID) })
	})

	// Implements: GIT-US-0187
	t.Run("child dies when the supervisor is killed with SIGKILL", func(t *testing.T) {
		tmp := t.TempDir()
		record := filepath.Join(tmp, "record.jsonl")
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		helper := exec.CommandContext(context.Background(), exe)
		helper.Env = append(os.Environ(), "SUPERVISOR_TEST_HELPER=1", "SUPERVISOR_TEST_BINARY="+fakeBinary,
			"SUPERVISOR_TEST_CACHE="+filepath.Join(tmp, "cache"), "SUPERVISOR_TEST_REPO="+filepath.Join(tmp, "repo"),
			"SUPERVISOR_TEST_RECORD="+record)
		if err := os.MkdirAll(filepath.Join(tmp, "repo"), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := helper.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := helper.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = helper.Process.Kill(); _ = helper.Wait() })

		line := make(chan string, 1)
		go func() {
			sc := bufio.NewScanner(out)
			for sc.Scan() {
				line <- sc.Text()
				return
			}
			line <- ""
		}()
		select {
		case l := <-line:
			if l != "ready" {
				t.Fatalf("helper said %q, want ready", l)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("the helper supervisor never became ready")
		}
		rg := &rig{t: t, record: record}
		starts := rg.starts()
		if len(starts) != 1 {
			t.Fatalf("starts = %d, want 1", len(starts))
		}
		child := starts[0].PID
		if !PIDAlive(child) {
			t.Fatalf("child %d is not running", child)
		}
		t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })

		if err := helper.Process.Signal(syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		_ = helper.Wait()
		waitFor(t, 15*time.Second, "the child to die with its supervisor", func() bool { return !PIDAlive(child) })
	})
}

// orphan starts a fake pando the way a crashed supervisor would have left it:
// its own process group, --cwd naming the instance directory.
func startOrphan(t *testing.T, dir string) int {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), fakeBinary, "mcp-server", "--no-stdio", "--cwd", dir)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "FAKE_PANDO_MODE=hang")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); <-done })
	return cmd.Process.Pid
}

func writeStaleState(t *testing.T, r *rig, childPID int) {
	t.Helper()
	dir := r.sup.Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	st := r.sup.Status()
	st.State, st.PID, st.SupervisorPID = StateReady, childPID, 1<<22+7 // a pid that is not running
	b, err := marshalStatus(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(dir, stateFileName), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOrphanCleanup(t *testing.T) {
	// Implements: GIT-US-0187
	t.Run("a child left by a dead supervisor is ended at the next start", func(t *testing.T) {
		r := newRig(t, nil, nil)
		orphan := startOrphan(t, r.sup.Dir())
		writeStaleState(t, r, orphan)
		r.start()
		waitFor(t, 10*time.Second, "the orphan to be ended", func() bool { return !PIDAlive(orphan) })
		st := r.waitState(StateReady)
		if st.PID == orphan {
			t.Errorf("the new child reuses the orphan pid %d", orphan)
		}
	})

	t.Run("a live process that is not ours is left alone", func(t *testing.T) {
		r := newRig(t, nil, nil)
		other := exec.CommandContext(context.Background(), "sleep", "60")
		other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := other.Start(); err != nil {
			t.Skipf("no sleep binary: %v", err)
		}
		done := make(chan struct{})
		go func() { _ = other.Wait(); close(done) }()
		t.Cleanup(func() { _ = syscall.Kill(-other.Process.Pid, syscall.SIGKILL); <-done })
		writeStaleState(t, r, other.Process.Pid)
		r.start()
		r.waitState(StateReady)
		if !PIDAlive(other.Process.Pid) {
			t.Errorf("pid %d was killed although its command line does not name %s", other.Process.Pid, r.sup.Dir())
		}
	})

}
