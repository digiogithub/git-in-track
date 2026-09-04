package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ErrInvalid is the sentinel behind every configuration validation failure, so
// that a caller can classify one with errors.Is without inspecting the fields.
var ErrInvalid = errors.New("invalid configuration")

// FieldError is one invalid setting: the dotted path of the field and why it
// was refused.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// Unwrap reports ErrInvalid so that errors.Is classifies a single field error.
func (e FieldError) Unwrap() error { return ErrInvalid }

// FieldErrors is the set of invalid settings Validate found.
type FieldErrors []FieldError

// Error implements the error interface.
func (e FieldErrors) Error() string {
	switch len(e) {
	case 0:
		return ErrInvalid.Error()
	case 1:
		return e[0].Error()
	}
	parts := make([]string, 0, len(e))
	for _, fe := range e {
		parts = append(parts, fe.Error())
	}
	return fmt.Sprintf("%d invalid settings: %s", len(e), strings.Join(parts, "; "))
}

// Unwrap reports ErrInvalid so that errors.Is classifies the whole set.
func (e FieldErrors) Unwrap() error { return ErrInvalid }

// Validate checks the configuration and returns every problem at once, because
// a user fixing a config file wants the whole list, not the first line of it.
func (c *Config) Validate() error {
	var errs FieldErrors
	add := func(field, format string, args ...any) {
		errs = append(errs, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	if c.Version != 0 && c.Version != SchemaVersion {
		add("version", "unsupported schema version %d, this build understands %d", c.Version, SchemaVersion)
	}
	c.validateServer(add)
	if !c.Git.Backend.Valid() {
		add("git.backend", "unknown backend %q: use auto, go-git or system", c.Git.Backend)
	}
	if c.Index.Debounce < 0 {
		add("index.debounce", "must not be negative")
	}
	if !validLogLevel(c.Log.Level) {
		add("log.level", "unknown level %q: use debug, info, warn or error", c.Log.Level)
	}
	if f := c.Log.Format; f != "" && f != "text" && f != "json" {
		add("log.format", "unknown format %q: use text or json", f)
	}
	c.validateRepos(add)
	c.validateWorkspaces(add)

	if len(errs) == 0 {
		return nil
	}
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Field < errs[j].Field })
	return errs
}

// validateServer checks the server section.
func (c *Config) validateServer(add func(field, format string, args ...any)) {
	if c.Server.Bind == "" {
		add("server.bind", "must not be empty")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		add("server.port", "%d is outside the range 1-65535", c.Server.Port)
	}
	if c.Server.IdleTimeout < 0 {
		add("server.idleTimeout", "must not be negative")
	}
}

// validateRepos checks the registered repositories.
func (c *Config) validateRepos(add func(field, format string, args ...any)) {
	seenID := make(map[string]bool, len(c.Repos))
	seenPath := make(map[string]bool, len(c.Repos))
	for i, r := range c.Repos {
		field := fmt.Sprintf("repos[%d]", i)
		switch {
		case r.ID == "":
			add(field+".id", "must not be empty")
		case seenID[r.ID]:
			add(field+".id", "duplicate repository id %q", r.ID)
		default:
			seenID[r.ID] = true
		}
		switch {
		case r.Path == "":
			add(field+".path", "must not be empty")
		case !filepath.IsAbs(r.Path):
			add(field+".path", "%q is not an absolute path", r.Path)
		case seenPath[canonical(r.Path)]:
			add(field+".path", "%q is registered twice", r.Path)
		default:
			seenPath[canonical(r.Path)] = true
		}
		if !r.Role.Valid() {
			add(field+".role", "unknown role %q: use project or team", r.Role)
		}
		if strings.HasPrefix(r.DocsFolder, "/") || filepath.IsAbs(r.DocsFolder) {
			add(field+".docsFolder", "%q must be relative to the repository root", r.DocsFolder)
		}
	}
}

// validateWorkspaces checks the workspaces and their repository references.
func (c *Config) validateWorkspaces(add func(field, format string, args ...any)) {
	known := make(map[string]bool, len(c.Repos))
	for _, r := range c.Repos {
		known[r.ID] = true
	}
	seen := make(map[string]bool, len(c.Workspaces))
	for i, w := range c.Workspaces {
		field := fmt.Sprintf("workspaces[%d]", i)
		switch {
		case w.Name == "":
			add(field+".name", "must not be empty")
		case seen[w.Name]:
			add(field+".name", "duplicate workspace name %q", w.Name)
		default:
			seen[w.Name] = true
		}
		for _, id := range w.Repos {
			if !known[id] {
				add(field+".repos", "unknown repository id %q", id)
			}
		}
	}
	if c.DefaultWorkspace != "" && !seen[c.DefaultWorkspace] {
		add("defaultWorkspace", "unknown workspace %q", c.DefaultWorkspace)
	}
}

// validLogLevel reports whether the level is one configureLogging accepts.
func validLogLevel(level string) bool {
	switch level {
	case "", "debug", "info", "warn", "warning", "error":
		return true
	default:
		return false
	}
}
