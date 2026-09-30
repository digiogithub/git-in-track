// This file and channel.go belong to package selfupdate (ADR-040): the part
// that swaps the running gintrack binary for an already verified one and
// decides whether this install may self-update at all. Release lookup and
// verification live in other files of the package. Native only: nothing here
// may be imported by internal/core or the WASM build.

package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// fsOps is the set of filesystem operations Apply performs. Tests inject
// failing implementations to exercise rollback.
type fsOps struct {
	stat    func(string) (fs.FileInfo, error)
	open    func(string) (io.ReadCloser, error)
	create  func(string, fs.FileMode) (writeSyncCloser, error)
	chmod   func(string, fs.FileMode) error
	chown   func(string, int, int) error
	rename  func(string, string) error
	remove  func(string) error
	geteuid func() int
}

type writeSyncCloser interface {
	io.Writer
	Sync() error
	Close() error
}

func osOps() *fsOps {
	return &fsOps{
		stat: os.Stat,
		open: func(p string) (io.ReadCloser, error) { return os.Open(p) },
		create: func(p string, m fs.FileMode) (writeSyncCloser, error) {
			return os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, m)
		},
		chmod:   os.Chmod,
		chown:   os.Lchown,
		rename:  os.Rename,
		remove:  os.Remove,
		geteuid: os.Geteuid,
	}
}

// ApplyOptions tunes Apply. The zero value is the production behaviour.
type ApplyOptions struct {
	ops *fsOps // injected by tests
}

// ApplyError describes a failed swap.
type ApplyError struct {
	// Op is the step that failed: "write", "rename-aside", "install" or "rollback".
	Op   string
	Path string
	Err  error
	// BinaryMayBeMissing is true when the target could not be restored, so the
	// old binary is at OldPath (or gone) and the target path may not exist.
	BinaryMayBeMissing bool
	// OldPath is where the previous binary was moved to.
	OldPath string
}

func (e *ApplyError) Error() string {
	msg := fmt.Sprintf("selfupdate: %s %s: %v", e.Op, e.Path, e.Err)
	if errors.Is(e.Err, fs.ErrPermission) {
		msg += " (permission denied: re-run gintrack update with enough rights to write to " +
			filepath.Dir(e.Path) + ", for example as the owner of the install; gintrack never escalates privileges)"
	}
	if e.BinaryMayBeMissing {
		msg += fmt.Sprintf(" (the gintrack binary may be missing; the previous version is at %s, move it back to restore it)", e.OldPath)
	}
	return msg
}

func (e *ApplyError) Unwrap() error { return e.Err }

// IsPermission reports whether the failure was a permission error.
func (e *ApplyError) IsPermission() bool { return errors.Is(e.Err, fs.ErrPermission) }

// CurrentExecutable returns the path of the running binary with symlinks
// resolved, which is the file Apply must replace.
func CurrentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("selfupdate: locate executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("selfupdate: resolve symlinks: %w", err)
	}
	return resolved, nil
}

// NewPath and OldPath name the side files used next to target.
func sidePath(target, suffix string) string {
	return filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+suffix)
}

// Apply replaces target with the already verified binary at newBinary.
//
// The copy is written next to target as ".<name>.new" (same filesystem, so the
// rename is atomic), keeps target's mode and, best effort, its owner, and is
// fsynced. Then target is renamed to ".<name>.old", the copy is renamed to
// target and the old file removed. If installing fails, target is restored.
// On Windows the running executable cannot be deleted, so ".old" stays until
// CleanupOld runs on the next start.
func Apply(newBinary, target string, opts ApplyOptions) error {
	ops := opts.ops
	if ops == nil {
		ops = osOps()
	}
	newPath := sidePath(target, ".new")
	oldPath := sidePath(target, ".old")

	info, err := ops.stat(target)
	if err != nil {
		return &ApplyError{Op: "write", Path: target, Err: err}
	}
	mode := info.Mode().Perm()

	if err := writeCopy(ops, newBinary, newPath, mode, info); err != nil {
		_ = ops.remove(newPath)
		return &ApplyError{Op: "write", Path: newPath, Err: err}
	}

	// A leftover from a previous run (Windows) would make the rename fail.
	_ = ops.remove(oldPath)
	if err := ops.rename(target, oldPath); err != nil {
		_ = ops.remove(newPath)
		return &ApplyError{Op: "rename-aside", Path: target, Err: err, OldPath: oldPath}
	}
	if err := ops.rename(newPath, target); err != nil {
		ae := &ApplyError{Op: "install", Path: target, Err: err, OldPath: oldPath}
		if rerr := ops.rename(oldPath, target); rerr != nil {
			ae.BinaryMayBeMissing = true
			ae.Err = fmt.Errorf("%w; rollback failed: %w", err, rerr)
		}
		_ = ops.remove(newPath)
		return ae
	}
	// Best effort: fails on Windows while the old image is running.
	_ = ops.remove(oldPath)
	return nil
}

func writeCopy(ops *fsOps, src, dst string, mode fs.FileMode, old fs.FileInfo) error {
	_ = ops.remove(dst) // stale leftover
	in, err := ops.open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := ops.create(dst, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return fmt.Errorf("fsync: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	// The umask may have stripped bits at create time.
	if err := ops.chmod(dst, mode); err != nil {
		return err
	}
	if ops.geteuid() == 0 {
		if uid, gid, ok := ownerOf(old); ok {
			_ = ops.chown(dst, uid, gid) // best effort
		}
	}
	return nil
}

// CleanupOld removes the ".<name>.old" file a previous Apply left next to
// target (Windows cannot delete it while the old image runs). It is cheap and
// silent, so the CLI may call it on every start.
func CleanupOld(target string) {
	_ = os.Remove(sidePath(target, ".old"))
}
