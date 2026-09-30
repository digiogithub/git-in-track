package selfupdate

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func fileEnv(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, os.ErrNotExist
	}
}

func TestDetectChannel(t *testing.T) {
	rel := Env{Version: "1.2.0", BuiltBy: "goreleaser", ReadFile: fileEnv(nil)}
	tests := []struct {
		name string
		exe  string
		env  Env
		want Channel
	}{
		{"cellar", "/opt/homebrew/Cellar/gintrack/1.0/bin/gintrack", rel, ChannelHomebrew},
		{"caskroom", "/usr/local/Caskroom/gintrack/1.0/gintrack", rel, ChannelHomebrew},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/bin/gintrack", rel, ChannelHomebrew},
		{"scoop", `C:\Users\me\scoop\apps\gintrack\current\gintrack.exe`, rel, ChannelScoop},
		{"dockerenv", "/usr/local/bin/gintrack", Env{Version: "1", ReadFile: fileEnv(map[string]string{"/.dockerenv": ""})}, ChannelContainer},
		{"cgroup docker", "/usr/local/bin/gintrack", Env{Version: "1", ReadFile: fileEnv(map[string]string{"/proc/1/cgroup": "0::/docker/abc"})}, ChannelContainer},
		{"cgroup kubepods", "/x/gintrack", Env{Version: "1", ReadFile: fileEnv(map[string]string{"/proc/1/cgroup": "1:cpu:/kubepods/x"})}, ChannelContainer},
		{"cgroup plain", "/x/gintrack", Env{Version: "1", ReadFile: fileEnv(map[string]string{"/proc/1/cgroup": "0::/user.slice"})}, ChannelRelease},
		{"dev", "/x/gintrack", Env{Version: "dev", BuiltBy: "goreleaser", ReadFile: fileEnv(nil)}, ChannelSource},
		{"builtBy source", "/x/gintrack", Env{Version: "1.0.0", BuiltBy: "source", ReadFile: fileEnv(nil)}, ChannelSource},
		{"release", "/usr/local/bin/gintrack", rel, ChannelRelease},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectChannel(tc.exe, tc.env); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestCheckChannel(t *testing.T) {
	none := fileEnv(nil)
	tests := []struct {
		name    string
		exe     string
		env     Env
		force   bool
		wantCmd string
		wantOK  bool
		warn    bool
	}{
		{"brew", "/opt/homebrew/Cellar/g/g", Env{ReadFile: none}, false, "brew upgrade --cask gintrack", false, false},
		{"scoop", `C:\scoop\apps\g\g.exe`, Env{ReadFile: none}, false, "scoop update gintrack", false, false},
		{"container", "/g", Env{ReadFile: fileEnv(map[string]string{"/.dockerenv": ""})}, false, "pull the new image", false, false},
		{"source no version", "/g", Env{Version: "dev", ReadFile: none}, false, "explicit version", false, false},
		{"source explicit", "/g", Env{Version: "dev", ExplicitVersion: true, ReadFile: none}, false, "", true, false},
		{"release", "/g", Env{Version: "1", BuiltBy: "goreleaser", ReadFile: none}, false, "", true, false},
		{"force brew", "/opt/homebrew/Cellar/g/g", Env{ReadFile: none}, true, "", true, true},
		{"force source", "/g", Env{Version: "dev", ReadFile: none}, true, "", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := CheckChannel(tc.exe, tc.env, tc.force)
			if tc.wantOK {
				if err != nil || (w != "") != tc.warn {
					t.Fatalf("err=%v warn=%q", err, w)
				}
				return
			}
			var ce *ChannelError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), tc.wantCmd) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
