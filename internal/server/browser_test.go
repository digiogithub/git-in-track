package server

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	t.Parallel()

	target := "http://127.0.0.1:7317/?token=abc"
	cases := []struct {
		goos string
		name string
		args []string
		ok   bool
	}{
		{goos: "linux", name: "xdg-open", args: []string{target}, ok: true},
		{goos: "darwin", name: "open", args: []string{target}, ok: true},
		{goos: "windows", name: "rundll32", args: []string{"url.dll,FileProtocolHandler", target}, ok: true},
		{goos: "freebsd", name: "xdg-open", args: []string{target}, ok: true},
		{goos: "plan9", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			name, args, ok := browserCommand(tc.goos, target)
			if ok != tc.ok {
				t.Fatalf("supported = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if name != tc.name {
				t.Errorf("command = %q, want %q", name, tc.name)
			}
			if strings.Join(args, " ") != strings.Join(tc.args, " ") {
				t.Errorf("args = %v, want %v", args, tc.args)
			}
		})
	}
}

func TestOpenBrowserNeverPanics(t *testing.T) {
	t.Parallel()

	// The helper is very likely absent in CI; the point is that a failure is
	// returned, never fatal.
	err := OpenBrowser(context.Background(), "http://127.0.0.1:7317/")
	if err != nil {
		t.Logf("no browser launcher on %s: %v", runtime.GOOS, err)
	}
}
