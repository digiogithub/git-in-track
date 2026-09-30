package selfupdate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, p, s string, m os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), m); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, m); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func setup(t *testing.T) (dir, target, nb string) {
	dir = t.TempDir()
	target = filepath.Join(dir, "gintrack")
	nb = filepath.Join(dir, "download")
	write(t, target, "old", 0o750)
	write(t, nb, "new", 0o600)
	return
}

func TestApply(t *testing.T) {
	t.Run("swaps and keeps mode", func(t *testing.T) {
		dir, target, nb := setup(t)
		if err := Apply(nb, target, ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
		if read(t, target) != "new" {
			t.Fatal("not replaced")
		}
		if runtime.GOOS != "windows" {
			fi, _ := os.Stat(target)
			if fi.Mode().Perm() != 0o750 {
				t.Fatalf("mode %v", fi.Mode().Perm())
			}
		}
		for _, s := range []string{".new", ".old"} {
			if _, err := os.Stat(filepath.Join(dir, ".gintrack"+s)); err == nil {
				t.Fatalf("leftover %s", s)
			}
		}
	})

	t.Run("rolls back when install rename fails", func(t *testing.T) {
		_, target, nb := setup(t)
		ops := osOps()
		ops.rename = func(a, b string) error {
			if b == target && a != sidePath(target, ".old") {
				return errors.New("boom")
			}
			return os.Rename(a, b)
		}
		err := Apply(nb, target, ApplyOptions{ops: ops})
		var ae *ApplyError
		if !errors.As(err, &ae) || ae.BinaryMayBeMissing || ae.Op != "install" {
			t.Fatalf("err = %v", err)
		}
		if read(t, target) != "old" {
			t.Fatal("not restored")
		}
		if _, e := os.Stat(sidePath(target, ".new")); e == nil {
			t.Fatal("new left behind")
		}
	})

	t.Run("reports a possibly missing binary when rollback fails", func(t *testing.T) {
		_, target, nb := setup(t)
		ops := osOps()
		ops.rename = func(a, b string) error {
			if b == target {
				return errors.New("boom")
			}
			return os.Rename(a, b)
		}
		var ae *ApplyError
		if err := Apply(nb, target, ApplyOptions{ops: ops}); !errors.As(err, &ae) || !ae.BinaryMayBeMissing {
			t.Fatalf("err = %v", err)
		}
		if read(t, ae.OldPath) != "old" {
			t.Fatal("old not kept")
		}
	})

	t.Run("failed aside leaves target untouched", func(t *testing.T) {
		_, target, nb := setup(t)
		ops := osOps()
		ops.rename = func(a, b string) error { return errors.New("boom") }
		if err := Apply(nb, target, ApplyOptions{ops: ops}); err == nil {
			t.Fatal("want error")
		}
		if read(t, target) != "old" {
			t.Fatal("target changed")
		}
	})

	t.Run("permission error suggests rights", func(t *testing.T) {
		_, target, nb := setup(t)
		ops := osOps()
		ops.create = func(string, fs.FileMode) (writeSyncCloser, error) { return nil, fs.ErrPermission }
		err := Apply(nb, target, ApplyOptions{ops: ops})
		var ae *ApplyError
		if !errors.As(err, &ae) || !ae.IsPermission() {
			t.Fatalf("err = %v", err)
		}
		if msg := err.Error(); !contains(msg, "re-run") || !contains(msg, "never escalates") {
			t.Fatal(msg)
		}
	})

	t.Run("windows style: old cannot be removed, cleaned next run", func(t *testing.T) {
		dir, target, nb := setup(t)
		old := sidePath(target, ".old")
		ops := osOps()
		ops.remove = func(p string) error {
			if p == old {
				return errors.New("file in use")
			}
			return os.Remove(p)
		}
		if err := Apply(nb, target, ApplyOptions{ops: ops}); err != nil {
			t.Fatal(err)
		}
		if read(t, target) != "new" || read(t, old) != "old" {
			t.Fatal("unexpected state")
		}
		CleanupOld(target)
		if _, err := os.Stat(old); err == nil {
			t.Fatal("old not cleaned")
		}
		_ = dir
	})

	t.Run("chowns only as root", func(t *testing.T) {
		_, target, nb := setup(t)
		for _, tc := range []struct {
			euid int
			want bool
		}{{0, runtime.GOOS != "windows"}, {1000, false}} {
			called := false
			ops := osOps()
			ops.geteuid = func() int { return tc.euid }
			ops.chown = func(string, int, int) error { called = true; return nil }
			write(t, target, "old", 0o755)
			if err := Apply(nb, target, ApplyOptions{ops: ops}); err != nil {
				t.Fatal(err)
			}
			if called != tc.want {
				t.Fatalf("euid %d: chown called=%v", tc.euid, called)
			}
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
