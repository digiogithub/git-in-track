package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCleanVaultPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		want     string
		wantCode string
	}{
		{name: "a plain page", path: "docs/architecture/auth.md", want: "docs/architecture/auth.md"},
		{name: "a redundant dot", path: "docs/./auth.md", want: "docs/auth.md"},
		{name: "an interior parent that cancels out", path: "docs/x/../auth.md", want: "docs/auth.md"},

		{name: "empty", path: "", wantCode: codeInvalidRequest},
		{name: "only spaces", path: "   ", wantCode: codeInvalidRequest},
		{name: "a bare parent", path: "..", wantCode: codeForbiddenPath},
		{name: "a leading parent", path: "../secrets.md", wantCode: codeForbiddenPath},
		{name: "a nested escape", path: "docs/../../secrets.md", wantCode: codeForbiddenPath},
		{name: "a deep escape", path: "docs/../../../../etc/passwd", wantCode: codeForbiddenPath},
		{name: "an absolute POSIX path", path: "/etc/passwd", wantCode: codeForbiddenPath},
		{name: "the root itself", path: "/", wantCode: codeForbiddenPath},
		{name: "a Windows volume", path: `C:\Windows\win.ini`, wantCode: codeForbiddenPath},
		{name: "a UNC path", path: `\\server\share\secrets.md`, wantCode: codeForbiddenPath},
		{name: "a backslash separator", path: `docs\auth.md`, wantCode: codeForbiddenPath},
		{name: "a NUL byte", path: "docs/auth.md\x00.png", wantCode: codeForbiddenPath},
		{name: "an encoded parent that survives cleaning", path: "docs/..%2f../secrets.md", want: "docs/..%2f../secrets.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cleanVaultPath("path", tt.path)
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("cleanVaultPath(%q) = %v, want %q", tt.path, err, tt.want)
				}
				if got != tt.want {
					t.Errorf("cleanVaultPath(%q) = %q, want %q", tt.path, got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("cleanVaultPath(%q) = %q, want the path refused", tt.path, got)
			}
			var refused *toolError
			if !asRefusal(err, &refused) {
				t.Fatalf("error %v is not a structured tool error", err)
			}
			if refused.Code != tt.wantCode {
				t.Errorf("code = %q, want %q (%s)", refused.Code, tt.wantCode, refused.Message)
			}
			if refused.Field != "path" {
				t.Errorf("field = %q, want path", refused.Field)
			}
		})
	}
}

// asRefusal is errors.As without importing errors for one call site.
func asRefusal(err error, dst **toolError) bool {
	e, ok := err.(*toolError) //nolint:errorlint // the package never wraps these
	if ok {
		*dst = e
	}
	return ok
}

// TestPathGuardRefusesASymlinkEscape is the check no amount of string cleaning
// can make: a path that is perfectly well formed and lives inside the vault,
// but resolves through a symbolic link to a file outside it.
func TestPathGuardRefusesASymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need elevation on Windows")
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secrets.md")
	if err := os.WriteFile(secret, []byte("# not yours\n"), 0o600); err != nil {
		t.Fatalf("write the file outside the vault: %v", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("create the docs folder: %v", err)
	}
	inside := filepath.Join(root, "docs", "leak.md")
	if err := os.Symlink(secret, inside); err != nil {
		t.Skipf("this file system does not support symbolic links: %v", err)
	}
	// A page that really is inside the vault, as the control.
	if err := os.WriteFile(filepath.Join(root, "docs", "real.md"), []byte("# mine\n"), 0o600); err != nil {
		t.Fatalf("write the page inside the vault: %v", err)
	}

	guard := NewPathGuard(root)

	t.Run("a page inside the vault is served", func(t *testing.T) {
		got, err := guard.Check("path", "docs/real.md")
		if err != nil {
			t.Fatalf("Check() = %v, want the path accepted", err)
		}
		if got != "docs/real.md" {
			t.Errorf("Check() = %q, want docs/real.md", got)
		}
	})

	t.Run("a symlink out of the vault is refused", func(t *testing.T) {
		_, err := guard.Check("path", "docs/leak.md")
		if err == nil {
			t.Fatal("Check() accepted a path that resolves outside the vault")
		}
		var refused *toolError
		if !asRefusal(err, &refused) || refused.Code != codeForbiddenPath {
			t.Fatalf("error = %v, want a forbidden_path refusal", err)
		}
		if !strings.Contains(refused.Message, "symbolic link") {
			t.Errorf("message = %q, want it to name the symbolic link", refused.Message)
		}
	})

	t.Run("a guard with no roots checks the spelling only", func(t *testing.T) {
		if _, err := NewPathGuard().Check("path", "docs/leak.md"); err != nil {
			t.Errorf("Check() = %v, want the lexical check to pass", err)
		}
		if _, err := NewPathGuard().Check("path", "../secrets.md"); err == nil {
			t.Error("Check() accepted a traversal without roots")
		}
	})
}

// TestPathToolsRefuseTraversal drives the refusal through a real tool call, so
// what is asserted is what a client sees rather than what a helper returns.
func TestPathToolsRefuseTraversal(t *testing.T) {
	h := newHarness(t, false)

	tests := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"get_kb_page with a parent segment", "get_kb_page", map[string]any{"path": "../../etc/passwd"}},
		{"get_kb_page with an absolute path", "get_kb_page", map[string]any{"path": "/etc/passwd"}},
		{"get_kb_page with a Windows path", "get_kb_page", map[string]any{"path": `C:\Windows\win.ini`}},
		{"get_kb_page climbing out of docs", "get_kb_page", map[string]any{"path": "docs/../../../../etc/passwd"}},
		{"list_kb_pages with a parent prefix", "list_kb_pages", map[string]any{"prefix": "../.."}},
		{"list_kb_pages with an absolute prefix", "list_kb_pages", map[string]any{"prefix": "/etc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callFails(t, h, tt.tool, tt.args)
			if got.Code != codeForbiddenPath {
				t.Errorf("code = %q, want %q (%s)", got.Code, codeForbiddenPath, got.Message)
			}
			if got.Expected == nil {
				t.Error("the refusal does not say what a correct path looks like")
			}
		})
	}
}

// TestSymlinkEscapeIsRefusedThroughTheTool builds a vault whose index really
// does hold a page that points outside it, and checks that the tool refuses to
// serve it even though the core would happily read it.
func TestSymlinkEscapeIsRefusedThroughTheTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need elevation on Windows")
	}
	h := newHarness(t, false)
	root := h.roots[len(h.roots)-1] // the project fixture

	outside := t.TempDir()
	secret := filepath.Join(outside, "secrets.md")
	if err := os.WriteFile(secret, []byte("# Secrets\n\nnot yours\n"), 0o600); err != nil {
		t.Fatalf("write the file outside the vault: %v", err)
	}
	link := filepath.Join(root, "docs", "leak.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("this file system does not support symbolic links: %v", err)
	}
	// Rebuild the index so that the linked page really is reachable through the
	// core: without this the test would pass for the wrong reason.
	if _, err := h.space.Dispatch(t.Context(), "vault.stats", nil); err != nil {
		t.Fatalf("vault.stats: %v", err)
	}
	for _, m := range h.space.Mounts() {
		if _, err := m.Vault.Reload(t.Context()); err != nil {
			t.Fatalf("reload %s: %v", m.ID, err)
		}
	}

	got := callFails(t, h, "get_kb_page", map[string]any{"path": "docs/leak.md", "body": true})
	if got.Code != codeForbiddenPath {
		t.Fatalf("code = %q, want %q (%s)", got.Code, codeForbiddenPath, got.Message)
	}
}
