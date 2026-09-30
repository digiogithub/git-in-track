package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

// serveInfo is the runtime file a running `gintrack serve` leaves under the
// cache dir, `<cacheDir>/serve/<port>.json` (GIT-US-0197, docs/07 §4.24). It is
// derived state: `gintrack update` and `gintrack doctor` read it to tell the
// user that a serve still runs an old binary, and nothing depends on it.
type serveInfo struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Bind      string    `json:"bind"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
	Config    string    `json:"config,omitempty"`
}

// serveRuntimeDir is where the runtime files of one cache dir live.
func serveRuntimeDir(cacheDir string) string {
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(cacheDir, "serve")
}

// serveRuntime writes and removes this process's runtime file.
type serveRuntime struct {
	dir, version, bind, config string
	log                        *slog.Logger
	path                       string
}

// record is the server's OnListen hook. A failure is logged and never stops
// the server: the file only exists to help a later `gintrack update`.
func (r *serveRuntime) record(addr string) {
	if r.dir == "" {
		return
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return
	}
	info := serveInfo{PID: os.Getpid(), Port: port, Bind: r.bind, Version: r.version, StartedAt: time.Now().UTC(), Config: r.config}
	path := filepath.Join(r.dir, strconv.Itoa(port)+".json")
	if err := writeServeInfo(path, info); err != nil {
		if r.log != nil {
			r.log.Warn("could not record the serve runtime file", "path", path, "err", err)
		}
		return
	}
	r.path = path
}

// remove deletes the file on a clean shutdown, only if it is still ours.
func (r *serveRuntime) remove() {
	if r.path == "" {
		return
	}
	if cur, err := readServeInfo(r.path); err == nil && cur.PID != os.Getpid() {
		return
	}
	_ = os.Remove(r.path)
}

func writeServeInfo(path string, info serveInfo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the runtime file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".serve-*.tmp")
	if err != nil {
		return fmt.Errorf("stage the runtime file: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write the runtime file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("publish the runtime file: %w", err)
	}
	return nil
}

func readServeInfo(path string) (serveInfo, error) {
	var info serveInfo
	data, err := os.ReadFile(path)
	if err != nil {
		return info, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return info, fmt.Errorf("parse %s: %w", path, err)
	}
	return info, nil
}

// liveServes returns the serves recorded under cacheDir whose process is still
// alive, ordered by port. A file whose pid is dead (a crash or a kill left it
// behind) is stale: it is ignored and removed. Nothing is ever signaled.
func liveServes(cacheDir string) []serveInfo {
	dir := serveRuntimeDir(cacheDir)
	if dir == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []serveInfo
	for _, f := range files {
		info, err := readServeInfo(f)
		if err != nil || info.PID <= 0 {
			continue
		}
		if !supervisor.PIDAlive(info.PID) {
			_ = os.Remove(f)
			continue
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// describe is the one-line form used by the update hint.
func (s serveInfo) describe() string {
	v := strings.TrimPrefix(s.Version, "v")
	if v == "" {
		v = "unknown version"
	}
	return fmt.Sprintf("gintrack serve (pid %d, port %d, version %s)", s.PID, s.Port, v)
}

// staleServes are the live serves whose version differs from current.
func staleServes(cacheDir, current string) []serveInfo {
	cur := strings.TrimPrefix(current, "v")
	var out []serveInfo
	for _, s := range liveServes(cacheDir) {
		if strings.TrimPrefix(s.Version, "v") != cur {
			out = append(out, s)
		}
	}
	return out
}

// checkRunningServes is the doctor check for a serve that runs another version
// than this binary. A serve with the same version, or none at all, is silent.
func checkRunningServes(cacheDir, current string) []checkResult {
	var out []checkResult
	for _, s := range staleServes(cacheDir, current) {
		out = append(out, checkResult{
			Scope:    "serve",
			Severity: "warning",
			Message:  fmt.Sprintf("%s runs a different version than this binary (%s)", s.describe(), strings.TrimPrefix(current, "v")),
			Fix:      "restart gintrack serve",
		})
	}
	return out
}
