package server

import (
	"path/filepath"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// TestInstallSemanticSearch covers the host seam the stdio MCP server uses
// (GIT-US-0121): the searcher is installed exactly when the companion would
// build one.
func TestInstallSemanticSearch(t *testing.T) {
	tests := []struct {
		name     string
		settings config.SearchPando
		want     bool
	}{
		{name: "no pando configured", settings: config.SearchPando{}, want: false},
		{name: "loopback pando", settings: config.SearchPando{MCPURL: "http://127.0.0.1:1/mcp"}, want: true},
		{name: "remote pando refused", settings: config.SearchPando{MCPURL: "http://pando.example.com/mcp"}, want: false},
		{
			name:     "remote pando allowed",
			settings: config.SearchPando{MCPURL: "http://pando.example.com/mcp", AllowRemote: true},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := filepath.Abs(fixtureRoot)
			if err != nil {
				t.Fatal(err)
			}
			fsys, err := osfs.New(root)
			if err != nil {
				t.Fatal(err)
			}
			v, err := vault.Open(fsys, "project-basic")
			if err != nil {
				t.Fatal(err)
			}
			space := vault.NewWorkspace()
			if _, err := space.Attach("project-basic", roleProject, v); err != nil {
				t.Fatal(err)
			}

			host := InstallSemanticSearch(tt.settings, space,
				[]SemanticRepo{{ID: "project-basic", Path: root, Vault: v}}, nil)
			if host == nil {
				t.Fatal("the host is nil")
			}
			defer func() { _ = host.Close() }()
			if got := space.SemanticAvailable(); got != tt.want {
				t.Errorf("SemanticAvailable() = %v, want %v", got, tt.want)
			}
			// The impact seam gets the same client and searcher (GIT-US-0147).
			if got := host.CallGraph() != nil; got != tt.want {
				t.Errorf("CallGraph() present = %v, want %v", got, tt.want)
			}
			if got := host.Semantic() != nil; got != tt.want {
				t.Errorf("Semantic() present = %v, want %v", got, tt.want)
			}
		})
	}
}
