package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// FileName is the base name of the configuration file.
const FileName = "config.yaml"

// DirName is the folder the configuration and the rest of the companion's state
// live in, under the per-platform configuration directory.
const DirName = "gintrack"

// EnvConfig names the environment variable that overrides the file location.
const EnvConfig = "GINTRACK_CONFIG"

// Reader looks up an environment variable. It is the seam that lets the path
// resolution be tested for every platform from any platform.
type Reader func(key string) string

// DefaultPath returns the configuration file path for the running platform,
// honoring GINTRACK_CONFIG and XDG_CONFIG_HOME.
func DefaultPath(env Reader) (string, error) {
	return PathFor(runtime.GOOS, env)
}

// PathFor returns the configuration file path for a given operating system:
//
//	GINTRACK_CONFIG                                      when set
//	$XDG_CONFIG_HOME/gintrack/config.yaml                when set
//	linux    ~/.config/gintrack/config.yaml
//	darwin   ~/Library/Application Support/gintrack/config.yaml
//	windows  %APPDATA%\gintrack\config.yaml
func PathFor(goos string, env Reader) (string, error) {
	if env == nil {
		env = func(string) string { return "" }
	}
	if p := strings.TrimSpace(env(EnvConfig)); p != "" {
		return expand(p, env), nil
	}
	if xdg := strings.TrimSpace(env("XDG_CONFIG_HOME")); xdg != "" && goos != "windows" {
		return filepath.Join(xdg, DirName, FileName), nil
	}
	dir, err := configDir(goos, env)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DirName, FileName), nil
}

// configDir returns the platform configuration directory the gintrack folder
// sits in.
func configDir(goos string, env Reader) (string, error) {
	switch goos {
	case "windows":
		if appData := strings.TrimSpace(env("APPDATA")); appData != "" {
			return appData, nil
		}
		if home := windowsHome(env); home != "" {
			return filepath.Join(home, "AppData", "Roaming"), nil
		}
		return "", errors.New("neither APPDATA nor USERPROFILE is set")
	case "darwin":
		home := strings.TrimSpace(env("HOME"))
		if home == "" {
			return "", errors.New("HOME is not set")
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		home := strings.TrimSpace(env("HOME"))
		if home == "" {
			return "", errors.New("neither XDG_CONFIG_HOME nor HOME is set")
		}
		return filepath.Join(home, ".config"), nil
	}
}

// windowsHome returns the user profile directory on Windows.
func windowsHome(env Reader) string {
	if p := strings.TrimSpace(env("USERPROFILE")); p != "" {
		return p
	}
	return strings.TrimSpace(env("HOME"))
}

// StateDir returns the directory the configuration file lives in, which is also
// where the index cache and the logs go.
func StateDir(configPath string) string { return filepath.Dir(configPath) }

// CacheDir returns the directory the index cache is written to: the configured
// one, or the directory of the configuration file.
func (c *Config) CacheDir(configPath string) string {
	if c.Index.CacheDir != "" {
		return c.Index.CacheDir
	}
	return StateDir(configPath)
}

// expand replaces a leading ~ with the home directory of the environment and
// makes the result absolute when it can.
func expand(p string, env Reader) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home := strings.TrimSpace(env("HOME"))
		if home == "" {
			home = windowsHome(env)
		}
		if home != "" {
			p = filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(p[1:], "/")))
		}
	}
	return p
}

// Expand resolves a leading ~ against the environment and returns an absolute
// path. It is what the command line uses on a path typed by a user.
func Expand(p string, env Reader) (string, error) {
	if env == nil {
		env = func(string) string { return "" }
	}
	expanded := expand(p, env)
	if expanded == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p, err)
	}
	return abs, nil
}

// canonical normalizes a path for comparison: cleaned, and case-folded on the
// platforms whose file systems are case-insensitive by default.
func canonical(p string) string {
	clean := filepath.Clean(p)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(clean)
	}
	return clean
}
