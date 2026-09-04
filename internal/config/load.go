package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// fileHeader is prepended to every file this package writes.
const fileHeader = "# gintrack configuration — see docs/07-cli-and-api.md, section 3.\n" +
	"# The server token lives here, which is why the file is created with mode 0600.\n"

// fileMode is the permission a configuration file is created with. It holds the
// bearer token of the local API, so no other user may read it.
const fileMode = 0o600

// dirMode is the permission the state directory is created with.
const dirMode = 0o700

// The environment variables of docs/07 section 3.3.
const (
	EnvWorkspace  = "GINTRACK_WORKSPACE"
	EnvPort       = "GINTRACK_PORT"
	EnvBind       = "GINTRACK_BIND"
	EnvToken      = "GINTRACK_TOKEN"
	EnvGitBackend = "GINTRACK_GIT_BACKEND"
	// EnvGitCommitOnSave overrides git.commitOnSave; it accepts anything
	// strconv.ParseBool does.
	EnvGitCommitOnSave = "GINTRACK_GIT_COMMIT_ON_SAVE"
	EnvLogLevel        = "GINTRACK_LOG_LEVEL"
	EnvLogFormat       = "GINTRACK_LOG_FORMAT"
)

// Env returns a Reader over the process environment.
func Env() Reader { return os.Getenv }

// Load reads a configuration file. A missing file is not an error: it yields
// the built-in defaults, so that a first run needs no setup.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path comes from the flag, the env or the platform default
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes configuration bytes on top of the defaults.
func Parse(data []byte) (*Config, error) {
	if strings.TrimSpace(string(data)) == "" {
		return Default(), nil
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse configuration: %w", err)
	}
	if cfg.Version == 0 {
		cfg.Version = SchemaVersion
	}
	cfg.EnsureWorkspace(cfg.DefaultWorkspace)
	return cfg, nil
}

// Save writes the configuration atomically with mode 0600, creating the parent
// directories with mode 0700. The bytes go to a temporary file in the target
// directory and are renamed into place, so a reader never sees a half file.
func Save(path string, c *Config) error {
	if path == "" {
		return errors.New("save configuration: empty path")
	}
	if c == nil {
		return errors.New("save configuration: nil configuration")
	}
	body, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".gintrack-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // a no-op once the rename succeeded

	if _, err := tmp.WriteString(fileHeader); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(name, fileMode); err != nil {
		return fmt.Errorf("set the permissions of %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Flags carries the values the command line supplies. A zero field means the
// flag was not given, which is what makes the precedence chain work.
type Flags struct {
	ConfigPath string
	Workspace  string
	Bind       string
	Port       int
	Token      string
	GitBackend string
	LogLevel   string
	LogFormat  string
}

// Resolution is the outcome of Resolve: the effective configuration and where
// it came from.
type Resolution struct {
	// Config is the effective configuration.
	Config *Config
	// Path is the configuration file that was consulted, whether or not it
	// exists. It is where Save writes.
	Path string
	// Exists reports whether that file was there.
	Exists bool
	// Workspace is the workspace commands should act on.
	Workspace string
}

// Resolve applies the precedence chain of docs/07 section 3.3: flags override
// environment variables, which override the file, which overrides the built-in
// defaults. The returned configuration is validated.
func Resolve(flags Flags, env Reader) (*Resolution, error) {
	if env == nil {
		env = func(string) string { return "" }
	}
	path, err := resolvePath(flags, env)
	if err != nil {
		return nil, err
	}
	_, statErr := os.Stat(path)
	exists := statErr == nil

	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	if err := applyEnv(cfg, env); err != nil {
		return nil, err
	}
	applyFlags(cfg, flags)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Resolution{Config: cfg, Path: path, Exists: exists, Workspace: cfg.ActiveWorkspace()}, nil
}

// resolvePath picks the configuration file: the flag, then GINTRACK_CONFIG,
// then the platform default.
func resolvePath(flags Flags, env Reader) (string, error) {
	if flags.ConfigPath != "" {
		return Expand(flags.ConfigPath, env)
	}
	path, err := DefaultPath(env)
	if err != nil {
		return "", fmt.Errorf("locate the configuration file: %w", err)
	}
	return path, nil
}

// applyEnv layers the GINTRACK_* variables on top of the file.
func applyEnv(c *Config, env Reader) error {
	if v := strings.TrimSpace(env(EnvWorkspace)); v != "" {
		c.DefaultWorkspace = v
		c.EnsureWorkspace(v)
	}
	if v := strings.TrimSpace(env(EnvBind)); v != "" {
		c.Server.Bind = v
	}
	if v := strings.TrimSpace(env(EnvPort)); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return FieldErrors{{Field: EnvPort, Message: fmt.Sprintf("%q is not a port number", v)}}
		}
		c.Server.Port = port
	}
	if v := strings.TrimSpace(env(EnvToken)); v != "" {
		c.Server.Token = v
	}
	if v := strings.TrimSpace(env(EnvGitBackend)); v != "" {
		c.Git.Backend = Backend(v)
	}
	if v := strings.TrimSpace(env(EnvGitCommitOnSave)); v != "" {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return FieldErrors{{Field: EnvGitCommitOnSave, Message: fmt.Sprintf("%q is not a boolean", v)}}
		}
		c.Git.CommitOnSave = on
	}
	if v := strings.TrimSpace(env(EnvLogLevel)); v != "" {
		c.Log.Level = v
	}
	if v := strings.TrimSpace(env(EnvLogFormat)); v != "" {
		c.Log.Format = v
	}
	return nil
}

// applyFlags layers the command-line values on top of everything else.
func applyFlags(c *Config, flags Flags) {
	if flags.Workspace != "" {
		c.DefaultWorkspace = flags.Workspace
		c.EnsureWorkspace(flags.Workspace)
	}
	if flags.Bind != "" {
		c.Server.Bind = flags.Bind
	}
	if flags.Port != 0 {
		c.Server.Port = flags.Port
	}
	if flags.Token != "" {
		c.Server.Token = flags.Token
	}
	if flags.GitBackend != "" {
		c.Git.Backend = Backend(flags.GitBackend)
	}
	if flags.LogLevel != "" {
		c.Log.Level = flags.LogLevel
	}
	if flags.LogFormat != "" {
		c.Log.Format = flags.LogFormat
	}
}
