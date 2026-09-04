package mcp

import (
	"path"
	"path/filepath"
	"strings"
)

// Path confinement, story GIT-US-0024.
//
// The MCP server must never become a general file-read primitive. Every tool
// argument that names a file is a *vault-relative* path: it is cleaned, checked
// for traversal and, when the host told us where the repositories live on disk,
// checked again after symlink resolution. A path that fails any of those is
// rejected with `forbidden_path` before it reaches the vault, so the check is
// the same whichever tool asked and whichever transport carried the call.

// cleanVaultPath validates a vault-relative path and returns its canonical
// form. It rejects the empty path, absolute paths in both POSIX and Windows
// spellings, anything containing a `..` segment, and any backslash, which is a
// path separator on the platform the companion also runs on.
//
// The check is purely lexical, which is deliberate: it does not depend on what
// exists on disk, so it behaves identically in the browser build's tests, in
// the companion and in a client that sends a path for a repository it cannot
// see.
func cleanVaultPath(field, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", invalidField(field, "a vault-relative path is required", "docs/architecture/auth.md")
	}
	if strings.ContainsRune(p, 0) {
		return "", pathRefused(field, p, "the path contains a NUL byte")
	}
	if strings.ContainsRune(p, '\\') {
		return "", pathRefused(field, p, "the path contains a backslash; vault paths always use forward slashes")
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) || volumeName(p) != "" {
		return "", pathRefused(field, p, "the path is absolute; vault paths are relative to the repository root")
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || clean == "/" {
		return "", pathRefused(field, p, "the path does not name a file inside the repository")
	}
	if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", pathRefused(field, p, "the path escapes the repository root")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".." {
			return "", pathRefused(field, p, "the path escapes the repository root")
		}
	}
	return clean, nil
}

// volumeName reports the Windows volume of a path ("C:"), on every platform.
// filepath.IsAbs only recognizes it when the binary was built for Windows, and
// a path argument arrives over the wire from wherever the agent runs.
func volumeName(p string) string {
	if len(p) < 2 || p[1] != ':' {
		return ""
	}
	c := p[0]
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return p[:2]
	}
	return ""
}

// pathRefused builds the error a rejected path argument reports. The message
// says what was wrong and the expected value shows the shape that works, so the
// agent's next attempt succeeds without reading documentation.
func pathRefused(field, p, why string) *toolError {
	return &toolError{
		Code:     codeForbiddenPath,
		Message:  "path " + p + " is refused: " + why,
		Field:    field,
		Path:     p,
		Expected: "a path relative to the repository root, without `..` segments, such as docs/architecture/auth.md",
	}
}

// A PathGuard confines a vault-relative path to the repositories the host
// mounted. It is the second half of the check: cleanVaultPath rules out the
// spellings that escape lexically, and the guard rules out the ones that escape
// through a symbolic link, which no amount of string cleaning can catch.
//
// Roots are host directories. A host that has none — the browser build, or a
// test over an in-memory vault — leaves them empty, and the guard then performs
// the lexical check only.
type PathGuard struct {
	roots []string
	// resolve maps a host path onto its symlink-free form. It is a field so
	// that a test can drive the escape case without creating a symlink on a
	// file system that may not support one.
	resolve func(string) (string, error)
}

// NewPathGuard returns a guard over the given host directories. Roots that
// cannot be resolved are kept as given: an unreadable root can only make the
// guard stricter, never laxer, because a path that does not resolve under any
// root is refused.
func NewPathGuard(roots ...string) *PathGuard {
	g := &PathGuard{resolve: filepath.EvalSymlinks}
	for _, root := range roots {
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			abs = root
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		g.roots = append(g.roots, abs)
	}
	return g
}

// Check validates a path argument and returns its canonical vault-relative
// form. A guard with no roots checks the spelling only.
func (g *PathGuard) Check(field, p string) (string, error) {
	clean, err := cleanVaultPath(field, p)
	if err != nil {
		return "", err
	}
	if g == nil || len(g.roots) == 0 {
		return clean, nil
	}
	for _, root := range g.roots {
		host := filepath.Join(root, filepath.FromSlash(clean))
		resolved, err := g.resolve(host)
		if err != nil {
			// The path does not exist under this root, or cannot be resolved.
			// Either way it is not the file this root would serve; another root
			// may still hold it, and a path that no root holds is answered with
			// `not_found` by the vault, not by the guard.
			continue
		}
		if !within(root, resolved) {
			return "", pathRefused(field, p,
				"the path resolves outside the repository root through a symbolic link")
		}
		return clean, nil
	}
	return clean, nil
}

// within reports whether host is root itself or a descendant of it.
func within(root, host string) bool {
	if host == root {
		return true
	}
	rel, err := filepath.Rel(root, host)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
