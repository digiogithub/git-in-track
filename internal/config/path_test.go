package config

import (
	"path/filepath"
	"testing"
)

// fakeEnv turns a map into a Reader.
func fakeEnv(values map[string]string) Reader {
	return func(key string) string { return values[key] }
}

func TestPathFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		goos string
		env  map[string]string
		want string
	}{
		{
			name: "linux uses ~/.config",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/jose"},
			want: filepath.Join("/home/jose", ".config", "gintrack", "config.yaml"),
		},
		{
			name: "linux honors XDG_CONFIG_HOME",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/jose", "XDG_CONFIG_HOME": "/home/jose/cfg"},
			want: filepath.Join("/home/jose/cfg", "gintrack", "config.yaml"),
		},
		{
			name: "macOS uses Application Support",
			goos: "darwin",
			env:  map[string]string{"HOME": "/Users/jose"},
			want: filepath.Join("/Users/jose", "Library", "Application Support", "gintrack", "config.yaml"),
		},
		{
			name: "windows uses APPDATA",
			goos: "windows",
			env:  map[string]string{"APPDATA": `C:\Users\jose\AppData\Roaming`},
			want: filepath.Join(`C:\Users\jose\AppData\Roaming`, "gintrack", "config.yaml"),
		},
		{
			name: "windows falls back to the user profile",
			goos: "windows",
			env:  map[string]string{"USERPROFILE": `C:\Users\jose`},
			want: filepath.Join(`C:\Users\jose`, "AppData", "Roaming", "gintrack", "config.yaml"),
		},
		{
			name: "windows ignores XDG_CONFIG_HOME",
			goos: "windows",
			env:  map[string]string{"APPDATA": `C:\AppData`, "XDG_CONFIG_HOME": "/home/jose/cfg"},
			want: filepath.Join(`C:\AppData`, "gintrack", "config.yaml"),
		},
		{
			name: "GINTRACK_CONFIG wins everywhere",
			goos: "darwin",
			env:  map[string]string{"HOME": "/Users/jose", EnvConfig: "/etc/gintrack.yaml"},
			want: "/etc/gintrack.yaml",
		},
		{
			name: "GINTRACK_CONFIG expands a leading tilde",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/jose", EnvConfig: "~/alt/config.yaml"},
			want: filepath.Join("/home/jose", "alt", "config.yaml"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := PathFor(tt.goos, fakeEnv(tt.env))
			if err != nil {
				t.Fatalf("PathFor(%q): %v", tt.goos, err)
			}
			if got != tt.want {
				t.Errorf("PathFor(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

func TestPathForWithoutAHome(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"linux", "darwin", "windows"} {
		if _, err := PathFor(goos, fakeEnv(nil)); err == nil {
			t.Errorf("PathFor(%q) with an empty environment succeeded", goos)
		}
	}
}

func TestCacheDirDefaultsToTheStateDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join("/home/jose", ".config", "gintrack", "config.yaml")
	c := Default()
	if got, want := c.CacheDir(path), filepath.Dir(path); got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
	c.Index.CacheDir = "/var/cache/gintrack"
	if got := c.CacheDir(path); got != "/var/cache/gintrack" {
		t.Errorf("CacheDir = %q, want the configured one", got)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()

	env := fakeEnv(map[string]string{"HOME": "/home/jose"})
	got, err := Expand("~/code/acme", env)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if want := filepath.Join("/home/jose", "code", "acme"); got != want {
		t.Errorf("Expand = %q, want %q", got, want)
	}
	if _, err := Expand("", env); err == nil {
		t.Error("expanding an empty path succeeded")
	}
}
