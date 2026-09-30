package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
	"github.com/digiogithub/git-in-track/internal/server"
)

func writeInfo(t *testing.T, cache string, info serveInfo) string {
	t.Helper()
	p := filepath.Join(serveRuntimeDir(cache), strconv.Itoa(info.Port)+".json")
	if err := writeServeInfo(p, info); err != nil {
		t.Fatal(err)
	}
	return p
}

// deadPID is a pid that is not alive: a child that already exited.
func deadPID(t *testing.T) int {
	t.Helper()
	for pid := 4000000; pid < 4000100; pid++ {
		if !supervisor.PIDAlive(pid) {
			return pid
		}
	}
	t.Skip("no dead pid found")
	return 0
}

func TestServeRuntimeWriteAndRemove(t *testing.T) {
	cache := t.TempDir()
	rt := &serveRuntime{dir: serveRuntimeDir(cache), version: "2.2.0", bind: "127.0.0.1", config: "/x/config.yaml"}
	rt.record("127.0.0.1:7317")
	path := filepath.Join(cache, "serve", "7317.json")
	info, err := readServeInfo(path)
	if err != nil {
		t.Fatalf("runtime file not written: %v", err)
	}
	if info.PID != os.Getpid() || info.Port != 7317 || info.Version != "2.2.0" || info.Config != "/x/config.yaml" || info.StartedAt.IsZero() {
		t.Errorf("info = %+v", info)
	}
	rt.remove()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still there after remove: %v", err)
	}
	// Nothing temporary is left behind.
	if left, _ := filepath.Glob(filepath.Join(cache, "serve", "*")); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
}

func TestLiveServes(t *testing.T) {
	t.Run("no file", func(t *testing.T) {
		if got := liveServes(t.TempDir()); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("stale pid is ignored and cleaned", func(t *testing.T) {
		cache := t.TempDir()
		p := writeInfo(t, cache, serveInfo{PID: deadPID(t), Port: 7000, Version: "1.0.0"})
		if got := liveServes(cache); len(got) != 0 {
			t.Errorf("got %v", got)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("stale file kept: %v", err)
		}
	})
	t.Run("several serves, ordered by port", func(t *testing.T) {
		cache := t.TempDir()
		writeInfo(t, cache, serveInfo{PID: os.Getpid(), Port: 8000, Version: "2.0.0"})
		writeInfo(t, cache, serveInfo{PID: os.Getpid(), Port: 7000, Version: "2.1.0"})
		if err := os.WriteFile(filepath.Join(cache, "serve", "junk.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := liveServes(cache)
		if len(got) != 2 || got[0].Port != 7000 || got[1].Port != 8000 {
			t.Errorf("got %+v", got)
		}
	})
}

func TestUpdateListsRunningServes(t *testing.T) {
	r := newUpdateRig(t)
	r.fake.publish("2.1.0", "new binary", "")
	writeInfo(t, r.cache, serveInfo{PID: os.Getpid(), Port: 7317, Version: "2.0.0"})
	writeInfo(t, r.cache, serveInfo{PID: os.Getpid(), Port: 7318, Version: "2.1.0"}) // already current
	writeInfo(t, r.cache, serveInfo{PID: deadPID(t), Port: 7319, Version: "1.0.0"})  // stale
	stdout, stderr, code := r.run("2.0.0", "goreleaser", "", "--yes", "--json")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	res := decode[updateResult](t, stdout)
	if len(res.Running) != 1 || !strings.Contains(res.Running[0], "port 7317") || !strings.Contains(res.Running[0], "version 2.0.0") {
		t.Errorf("running = %v", res.Running)
	}
	if !strings.Contains(stderr, "Restart to use the new version") {
		t.Errorf("no restart hint: %s", stderr)
	}
}

func TestCheckRunningServes(t *testing.T) {
	cache := t.TempDir()
	if got := checkRunningServes(cache, "2.1.0"); len(got) != 0 {
		t.Errorf("no serve: %v", got)
	}
	writeInfo(t, cache, serveInfo{PID: os.Getpid(), Port: 7317, Version: "2.1.0"})
	if got := checkRunningServes(cache, "v2.1.0"); len(got) != 0 {
		t.Errorf("same version: %v", got)
	}
	writeInfo(t, cache, serveInfo{PID: os.Getpid(), Port: 7318, Version: "2.0.0"})
	got := checkRunningServes(cache, "2.1.0")
	if len(got) != 1 || got[0].Severity != "warning" || !strings.Contains(got[0].Fix, "restart gintrack serve") || !strings.Contains(got[0].Message, "2.0.0") {
		t.Errorf("got %+v", got)
	}
}

func TestServeWritesRuntimeFileWhileRunning(t *testing.T) {
	cache := t.TempDir()
	rt := &serveRuntime{dir: serveRuntimeDir(cache), version: "2.2.0", bind: "127.0.0.1"}
	srv, err := server.New(server.Options{Bind: "127.0.0.1", Port: freePort(t), Token: "t", OnListen: rt.record})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { err := srv.Start(ctx); rt.remove(); done <- err }()

	deadline := time.Now().Add(5 * time.Second)
	for len(liveServes(cache)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := liveServes(cache)
	if len(got) != 1 || got[0].Port == 0 || got[0].PID != os.Getpid() {
		cancel()
		t.Fatalf("runtime file while running = %+v", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("server did not stop")
	}
	if left := liveServes(cache); len(left) != 0 {
		t.Errorf("runtime file after shutdown = %+v", left)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}
