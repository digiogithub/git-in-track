package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSetSemanticSearchGuard(t *testing.T) {
	t.Run("a file changed between read and save is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		c := Default()
		c.Repos = []Repo{{ID: "a", Path: "/tmp/a", Role: "project", Enabled: true}}
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		other := []byte("# another writer\n" + string(mustRead(t, path)) + "# extra\n")
		afterSemanticLoad = func() {
			if err := os.WriteFile(path, other, 0o600); err != nil {
				t.Error(err)
			}
		}
		t.Cleanup(func() { afterSemanticLoad = func() {} })

		err := SetSemanticSearch(path, "a", true)
		if !errors.Is(err, ErrChangedOnDisk) {
			t.Fatalf("error = %v, want ErrChangedOnDisk", err)
		}
		if got := mustRead(t, path); string(got) != string(other) {
			t.Errorf("the other writer's content was overwritten:\n%s", got)
		}
	})
	t.Run("an untouched file is written", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		c := Default()
		c.Repos = []Repo{{ID: "a", Path: "/tmp/a", Role: "project", Enabled: true}}
		if err := Save(path, c); err != nil {
			t.Fatal(err)
		}
		if err := SetSemanticSearch(path, "a", true); err != nil {
			t.Fatal(err)
		}
		got, err := Load(path)
		if err != nil || !got.Repos[0].SemanticSearch {
			t.Fatalf("Load() = %+v, %v", got, err)
		}
	})
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
