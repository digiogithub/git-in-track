//go:build unix

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// WatchdogCommandName is the hidden gintrack subcommand that runs RunWatchdog.
const WatchdogCommandName = "__pando-watch"

// watchdogFD is the descriptor of the read end of the lifeline pipe in the
// watchdog process: the first entry of exec.Cmd.ExtraFiles.
const watchdogFD = 3

// watchdogGrace is how long the child gets to obey SIGTERM once the supervisor
// is gone, before SIGKILL.
const watchdogGrace = 5 * time.Second

// RunWatchdog is the body of `gintrack __pando-watch <binary> [args...]`. It
// runs the child with the watchdog's own stdio and environment, and holds the
// read end of a pipe (descriptor 3) whose write end only the supervisor has.
// When that pipe reports end-of-file the supervisor is gone, crash or SIGKILL
// included, and the watchdog ends the child's process group. This gives
// platforms without Pdeathsig (macOS) the same guarantee Linux has, with no
// cgo and no new dependency. It returns the exit code for the process.
func RunWatchdog(args []string) int {
	if len(args) == 0 {
		return 2
	}
	syscall.CloseOnExec(watchdogFD)
	lifeline := os.NewFile(uintptr(watchdogFD), "lifeline")
	if lifeline == nil {
		return 2
	}

	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...) //nolint:gosec // the supervisor chose the binary
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		_, _ = os.Stderr.WriteString("pando-watch: start " + args[0] + ": " + err.Error() + "\n")
		return 127
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// The supervisor signals the whole group on stop; when only the watchdog
	// is signaled, pass it on.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	lost := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, lifeline)
		close(lost)
	}()

	// killAll ends the child's group when the watchdog leads it (the
	// supervisor arranges that), and only the child otherwise, so a hand-run
	// watchdog never signals its shell's group.
	killAll := func(sig syscall.Signal) {
		if syscall.Getpgrp() == syscall.Getpid() {
			if err := syscall.Kill(-syscall.Getpid(), sig); err == nil || !errors.Is(err, syscall.ESRCH) {
				return
			}
		}
		_ = cmd.Process.Signal(sig)
	}
	for {
		select {
		case err := <-exited:
			return watchdogExitCode(err)
		case sig := <-sigs:
			_ = cmd.Process.Signal(sig)
		case <-lost:
			killAll(syscall.SIGTERM)
			select {
			case err := <-exited:
				return watchdogExitCode(err)
			case <-time.After(watchdogGrace):
				_ = cmd.Process.Kill()
				killAll(syscall.SIGKILL)
				<-exited
				return 1
			}
		}
	}
}

func watchdogExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() >= 0 {
		return ee.ExitCode()
	}
	return 1
}

// wrapWithWatchdog rewrites cmd to run under the watchdog and returns the
// supervisor's end of the lifeline. The caller closes it once the command has
// been waited for (closing it earlier ends the child).
func wrapWithWatchdog(cmd *exec.Cmd, watchdog []string) (*lifeline, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create the lifeline pipe: %w", err)
	}
	cmd.Args = append(append([]string{}, watchdog...), cmd.Args...)
	cmd.Path = watchdog[0]
	cmd.ExtraFiles = append([]*os.File{r}, cmd.ExtraFiles...)
	return &lifeline{r: r, w: w}, nil
}

// lifeline holds both ends until the command started: the read end is closed
// in the supervisor as soon as the child has its copy.
type lifeline struct{ r, w *os.File }

func (l *lifeline) started() {
	if l != nil {
		_ = l.r.Close()
	}
}

// Close closes the write end, which ends the watchdog's child.
func (l *lifeline) Close() {
	if l != nil {
		_ = l.r.Close()
		_ = l.w.Close()
	}
}
