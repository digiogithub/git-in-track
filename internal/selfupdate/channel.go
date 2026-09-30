package selfupdate

import (
	"fmt"
	"os"
	"strings"
)

// Channel is how this gintrack was installed.
type Channel string

const (
	ChannelRelease   Channel = "release"
	ChannelHomebrew  Channel = "homebrew"
	ChannelScoop     Channel = "scoop"
	ChannelContainer Channel = "container"
	ChannelSource    Channel = "source"
)

// Env carries what DetectChannel needs besides the path, so tests can inject it.
type Env struct {
	// ReadFile reads a file; nil means os.ReadFile.
	ReadFile func(string) ([]byte, error)
	// Version and BuiltBy are the main.version and main.builtBy build values.
	Version string
	BuiltBy string
	// ExplicitVersion is true when the user asked for a specific version,
	// which is the only update a source build accepts.
	ExplicitVersion bool
}

func (e Env) read(p string) ([]byte, error) {
	if e.ReadFile != nil {
		return e.ReadFile(p)
	}
	return os.ReadFile(p)
}

// DetectChannel classifies the install at exePath (symlinks already resolved).
func DetectChannel(exePath string, env Env) Channel {
	p := strings.ToLower(strings.ReplaceAll(exePath, `\`, "/"))
	for _, m := range []string{"/cellar/", "/caskroom/", "/opt/homebrew/", "/home/linuxbrew/"} {
		if strings.Contains(p, m) {
			return ChannelHomebrew
		}
	}
	if strings.Contains(p, "/scoop/apps/") {
		return ChannelScoop
	}
	if inContainer(env) {
		return ChannelContainer
	}
	if env.Version == "dev" || env.BuiltBy == "source" {
		return ChannelSource
	}
	return ChannelRelease
}

func inContainer(env Env) bool {
	if _, err := env.read("/.dockerenv"); err == nil {
		return true
	}
	if b, err := env.read("/proc/1/cgroup"); err == nil {
		s := string(b)
		for _, m := range []string{"docker", "containerd", "kubepods"} {
			if strings.Contains(s, m) {
				return true
			}
		}
	}
	return false
}

// ChannelError is the refusal for a channel that must not self-update.
type ChannelError struct {
	Channel Channel
	// Command is the right way to upgrade this install.
	Command string
}

func (e *ChannelError) Error() string {
	switch e.Channel {
	case ChannelSource:
		return "this is a source or dev build; " + e.Command + " (or use --force)"
	default:
		return fmt.Sprintf("gintrack was installed through %s; %s instead (or use --force to replace the binary anyway)", e.Channel, e.Command)
	}
}

// CheckChannel decides whether exePath may self-update. A non-nil error is a
// *ChannelError refusal. With force, every refusal becomes a warning.
func CheckChannel(exePath string, env Env, force bool) (warning string, err error) {
	ch := DetectChannel(exePath, env)
	var ce *ChannelError
	switch ch {
	case ChannelHomebrew:
		ce = &ChannelError{ch, "run `brew upgrade --cask gintrack`"}
	case ChannelScoop:
		ce = &ChannelError{ch, "run `scoop update gintrack`"}
	case ChannelContainer:
		ce = &ChannelError{ch, "pull the new image (docker pull ghcr.io/digiogithub/git-in-track) and recreate the container"}
	case ChannelSource:
		if env.ExplicitVersion {
			return "", nil
		}
		ce = &ChannelError{ch, "pass an explicit version to update a source build, for example `gintrack update --version v1.2.3`"}
	default:
		return "", nil
	}
	if force {
		return fmt.Sprintf("warning: --force replaces the binary of a %s install; %s will not know about it", ch, ch), nil
	}
	return "", ce
}
