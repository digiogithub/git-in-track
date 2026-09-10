package watcher

import "testing"

// TestSkipDirNameMatchesTheVaultWalk keeps the watch walk in step with core's
// vault walk: a folder the index never reads is never worth a watch.
func TestSkipDirNameMatchesTheVaultWalk(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		".git": true, "node_modules": true, "dist": true, "vendor": true, ".cache": true,
		".pmngr": false, "docs": false, "src": false,
	} {
		if got := skipDirName(name); got != want {
			t.Errorf("skipDirName(%q) = %v, want %v", name, got, want)
		}
	}
}
