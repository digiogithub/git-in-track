package config

import (
	"fmt"
	"strings"
)

// The values of search.pando.mode (ADR-039).
const (
	PandoModeAuto     = "auto"
	PandoModeManaged  = "managed"
	PandoModeExternal = "external"
	PandoModeOff      = "off"
)

// Defaults and bounds of search.pando.managed.
const (
	DefaultPandoBinary       = "pando"
	DefaultPandoMaxInstances = 4
	MaxPandoMaxInstances     = 64
	DefaultPandoLogLevel     = "info"
)

// PandoManaged is the `search.pando.managed` section: how gintrack serve finds
// and runs the Pando processes it supervises.
type PandoManaged struct {
	// Binary is a name on PATH or an absolute path. Empty means "pando".
	Binary string `json:"binary,omitempty" yaml:"binary,omitempty"`
	// MaxInstances refuses to start more instances than this; the extra
	// repositories report unavailable. Zero means the default of 4.
	MaxInstances int `json:"maxInstances,omitempty" yaml:"maxInstances,omitempty"`
	// MinVersion overrides the version floor built into gintrack. Empty keeps it.
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
	// LogLevel is debug, info, warn or error. Empty means info.
	LogLevel string `json:"logLevel,omitempty" yaml:"logLevel,omitempty"`
}

// BinaryOrDefault is the binary to look up.
func (m PandoManaged) BinaryOrDefault() string {
	if b := strings.TrimSpace(m.Binary); b != "" {
		return b
	}
	return DefaultPandoBinary
}

// MaxInstancesOrDefault is the instance cap in force.
func (m PandoManaged) MaxInstancesOrDefault() int {
	if m.MaxInstances > 0 {
		return m.MaxInstances
	}
	return DefaultPandoMaxInstances
}

// LogLevelOrDefault is the log level in force.
func (m PandoManaged) LogLevelOrDefault() string {
	if l := strings.TrimSpace(m.LogLevel); l != "" {
		return l
	}
	return DefaultPandoLogLevel
}

// ModeOrDefault is the configured mode, with an absent one read as auto.
func (p SearchPando) ModeOrDefault() string {
	if m := strings.TrimSpace(p.Mode); m != "" {
		return m
	}
	return PandoModeAuto
}

// externalKeys names the keys that say where an external Pando is, in the order
// the refusal lists them. mode: managed may set none of them.
func (p SearchPando) externalKeys() []string {
	var keys []string
	for _, k := range []struct{ name, value string }{
		{"mcpUrl", p.MCPURL}, {"mcpToken", p.MCPToken}, {"restUrl", p.RESTURL},
		{"restToken", p.RESTToken}, {"projectId", p.ProjectID},
	} {
		if strings.TrimSpace(k.value) != "" {
			keys = append(keys, "search.pando."+k.name)
		}
	}
	return keys
}

// validatePandoMode checks search.pando.mode and search.pando.managed.*, and
// refuses mode: managed mixed with an external-only key, naming each key.
func validatePandoMode(add func(field, format string, args ...any), p SearchPando) {
	switch mode := p.ModeOrDefault(); mode {
	case PandoModeAuto, PandoModeExternal, PandoModeOff:
	case PandoModeManaged:
		for _, key := range p.externalKeys() {
			add(key, "must not be set with search.pando.mode: managed: managed mode "+
				"generates its own endpoint and token. Delete this key, or set mode to external")
		}
	default:
		add("search.pando.mode", "unknown mode %q: use auto, managed, external or off", mode)
	}
	m := p.Managed
	if m.MaxInstances < 0 || m.MaxInstances > MaxPandoMaxInstances {
		add("search.pando.managed.maxInstances", "%d is outside the range 0-%d, where 0 means the default of %d",
			m.MaxInstances, MaxPandoMaxInstances, DefaultPandoMaxInstances)
	}
	switch m.LogLevel {
	case "", "debug", "info", "warn", "error":
	default:
		add("search.pando.managed.logLevel", "unknown level %q: use debug, info, warn or error", m.LogLevel)
	}
	if v := strings.TrimSpace(m.MinVersion); v != "" && !validVersion(v) {
		add("search.pando.managed.minVersion", "%q is not a version such as 1.1.0", v)
	}
}

// validVersion accepts dotted numeric versions with an optional leading v and
// an optional pre-release or build suffix.
func validVersion(v string) bool {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return true
}

// PandoResolution is the effective Pando mode and why (ADR-039 "Mode
// resolution").
type PandoResolution struct {
	// Mode is managed, external or off.
	Mode string `json:"mode"`
	// Rule is the number of the first rule that matched, 1 to 6.
	Rule int `json:"rule"`
	// Reason is one line saying why, in the words of the unavailable answers.
	Reason string `json:"reason"`
	// Binary is the resolved binary path, set when Mode is managed and one was
	// found, or when rule 5 matched.
	Binary string `json:"binary,omitempty"`
	// Unavailable is true when the mode is managed but no binary was found: the
	// configuration loads and every Pando-backed answer is unavailable.
	Unavailable bool `json:"unavailable,omitempty"`
}

// String renders "managed (rule 5): ...".
func (r PandoResolution) String() string {
	return fmt.Sprintf("%s (rule %d): %s", r.Mode, r.Rule, r.Reason)
}

// ResolvePandoMode applies the six rules of ADR-039, first match wins. lookPath
// finds the binary (exec.LookPath in production); a lookup that fails is never
// an error, it only makes the mode off (rule 6) or managed-but-unavailable
// (rule 3). Nothing is started or contacted. The version floor is checked by
// the supervisor when it starts, not here.
func ResolvePandoMode(p SearchPando, lookPath func(string) (string, error)) PandoResolution {
	find := func() string {
		if lookPath == nil {
			return ""
		}
		path, err := lookPath(p.Managed.BinaryOrDefault())
		if err != nil {
			return ""
		}
		return path
	}
	hasURL := strings.TrimSpace(p.MCPURL) != ""
	switch mode := p.ModeOrDefault(); {
	case mode == PandoModeOff:
		return PandoResolution{Mode: PandoModeOff, Rule: 1, Reason: "Pando is turned off"}
	case mode == PandoModeExternal:
		reason := "search.pando.mode is external"
		if !hasURL {
			reason = "Pando is not configured"
		}
		return PandoResolution{Mode: PandoModeExternal, Rule: 2, Reason: reason}
	case mode == PandoModeManaged:
		bin := find()
		if bin == "" {
			return PandoResolution{Mode: PandoModeManaged, Rule: 3, Unavailable: true,
				Reason: "managed, unavailable: no pando binary"}
		}
		return PandoResolution{Mode: PandoModeManaged, Rule: 3, Binary: bin,
			Reason: "search.pando.mode is managed"}
	case hasURL:
		return PandoResolution{Mode: PandoModeExternal, Rule: 4,
			Reason: "search.pando.mcpUrl is set, and an explicit endpoint beats a binary on PATH"}
	}
	if bin := find(); bin != "" {
		return PandoResolution{Mode: PandoModeManaged, Rule: 5, Binary: bin,
			Reason: "no mcpUrl and a pando binary was found"}
	}
	return PandoResolution{Mode: PandoModeOff, Rule: 6, Reason: "no Pando binary was found"}
}
