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

	// The `sync.engine` overrides. They are declared here, with the rest of
	// the environment layer, so that `gintrack serve` and this package cannot
	// disagree about what a variable is called.
	EnvSyncWorkers     = "GINTRACK_SYNC_WORKERS"
	EnvSyncBatch       = "GINTRACK_SYNC_BATCH"
	EnvSyncRate        = "GINTRACK_SYNC_RATE"
	EnvSyncMaxAttempts = "GINTRACK_SYNC_MAX_ATTEMPTS"
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
	if err := refuseRetiredKeys(data); err != nil {
		return nil, err
	}
	if cfg.Version == 0 {
		cfg.Version = SchemaVersion
	}
	cfg.EnsureWorkspace(cfg.DefaultWorkspace)
	return cfg, nil
}

// retiredProbe is the shape a retired key is looked for in. Only keys this
// build no longer has a field for belong here: everything else is either
// decoded by Config or ignored, as an unknown key always has been.
type retiredProbe struct {
	Search struct {
		Pando struct {
			CorpusDir *string `yaml:"corpusDir"`
		} `yaml:"pando"`
	} `yaml:"search"`
}

// refuseRetiredKeys fails a file that still configures something this build has
// removed.
//
// Silently ignoring one would be worse than refusing it: an operator who wrote
// `search.pando.corpusDir` expects a corpus to be written somewhere, and a
// build that quietly stops writing it looks like a bug in search rather than a
// deliberate removal. So the message names the epic that removed it and says
// what to do with what is left on disk.
func refuseRetiredKeys(data []byte) error {
	var probe retiredProbe
	if err := yaml.Unmarshal(data, &probe); err != nil {
		// Anything malformed enough to fail here already failed the real
		// decode above; there is nothing this pass can add.
		return nil //nolint:nilerr // the caller already reported the parse failure
	}
	if probe.Search.Pando.CorpusDir != nil {
		return FieldError{
			Field: "search.pando.corpusDir",
			Message: "the corpus exporter was removed in GIT-EP-0020: Pando now indexes the " +
				"repository itself, so there is no corpus to write. Delete this key. " +
				"A corpus directory left by an older version is not used any more and is " +
				"safe to remove by hand.",
		}
	}
	return nil
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
	// YouTrackToken is the permanent token a command was given on the command
	// line. It beats the environment and the file and is never written back.
	YouTrackToken string

	// The `sync.engine` overrides of `gintrack serve`. A zero field means the
	// flag was not given, which is what keeps the file layer underneath it.
	SyncWorkers     int
	SyncBatch       int
	SyncRate        float64
	SyncMaxAttempts int
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
	if v := strings.TrimSpace(env(EnvYouTrackToken)); v != "" {
		c.SetYouTrackTokenOverride(v, TokenSourceEnv)
	}
	if v := strings.TrimSpace(env(EnvPandoToken)); v != "" {
		c.SetPandoTokenOverride(v)
	}
	if v := strings.TrimSpace(env(EnvPandoMCPToken)); v != "" {
		c.SetPandoMCPTokenOverride(v)
	}
	if v := strings.TrimSpace(env(EnvPandoRESTToken)); v != "" {
		c.SetPandoRESTTokenOverride(v)
	}
	if v := strings.TrimSpace(env(EnvLogLevel)); v != "" {
		c.Log.Level = v
	}
	if v := strings.TrimSpace(env(EnvLogFormat)); v != "" {
		c.Log.Format = v
	}
	return applySyncEnv(c, env)
}

// applySyncEnv layers the four GINTRACK_SYNC_* variables on top of the
// `sync.engine` section. A value that is not a number is refused here rather
// than silently ignored: an operator who exported a typo wants to be told.
func applySyncEnv(c *Config, env Reader) error {
	if v := strings.TrimSpace(env(EnvSyncWorkers)); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return FieldErrors{{Field: EnvSyncWorkers, Message: fmt.Sprintf("%q is not a whole number", v)}}
		}
		c.Sync.Engine.Workers = n
	}
	if v := strings.TrimSpace(env(EnvSyncBatch)); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return FieldErrors{{Field: EnvSyncBatch, Message: fmt.Sprintf("%q is not a whole number", v)}}
		}
		c.Sync.Engine.BatchSize = n
	}
	if v := strings.TrimSpace(env(EnvSyncRate)); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return FieldErrors{{Field: EnvSyncRate, Message: fmt.Sprintf("%q is not a number", v)}}
		}
		c.Sync.Engine.Rate = f
	}
	if v := strings.TrimSpace(env(EnvSyncMaxAttempts)); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return FieldErrors{{Field: EnvSyncMaxAttempts, Message: fmt.Sprintf("%q is not a whole number", v)}}
		}
		c.Sync.Engine.MaxAttempts = n
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
	if flags.YouTrackToken != "" {
		c.SetYouTrackTokenOverride(flags.YouTrackToken, TokenSourceFlag)
	}
	if flags.LogLevel != "" {
		c.Log.Level = flags.LogLevel
	}
	if flags.LogFormat != "" {
		c.Log.Format = flags.LogFormat
	}
	if flags.SyncWorkers != 0 {
		c.Sync.Engine.Workers = flags.SyncWorkers
	}
	if flags.SyncBatch != 0 {
		c.Sync.Engine.BatchSize = flags.SyncBatch
	}
	if flags.SyncRate != 0 {
		c.Sync.Engine.Rate = flags.SyncRate
	}
	if flags.SyncMaxAttempts != 0 {
		c.Sync.Engine.MaxAttempts = flags.SyncMaxAttempts
	}
}
