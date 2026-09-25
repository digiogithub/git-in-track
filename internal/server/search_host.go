package server

import (
	"log/slog"
	"path/filepath"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// SemanticRepo is one repository a host other than the companion has already
// opened and attached to a vault.Workspace: the stdio `gintrack mcp` server.
type SemanticRepo struct {
	// ID is the id the repository is attached to the workspace under.
	ID string
	// Path is the absolute path of the working tree.
	Path string
	// Role is "project" or "team".
	Role string
	// DocsFolders are every documentation folder the registration declares.
	DocsFolders []string
	// Vault is the open vault attached to the workspace.
	Vault *vault.Vault
}

// InstallSemanticSearch builds the semantic searcher `gintrack serve`
// installs — the same Pando client and the same resolution back into the local
// index, through newPandoSearcher — and installs it on space, so that the
// "search.semantic" method, and with it the search_semantic MCP tool, works for
// a host that does not run the companion (GIT-US-0121).
//
// settings must carry resolved tokens. With no MCP URL configured, or a refused
// one, nothing is installed and the workspace keeps answering `unavailable`.
// The returned function closes the Pando session; it is never nil.
//
// Unlike the companion it registers no code project with Pando: that is the
// long-lived server's job, and a short-lived agent session must not queue an
// indexing job every time it is spawned.
func InstallSemanticSearch(settings config.SearchPando, space *vault.Workspace, repos []SemanticRepo, log *slog.Logger) func() error {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	reg := &registry{byID: make(map[string]*mount, len(repos)), space: space}
	for _, r := range repos {
		if r.Vault == nil {
			continue
		}
		role := r.Role
		if role == "" {
			role = roleProject
		}
		docs := ""
		if len(r.DocsFolders) > 0 {
			docs = r.DocsFolders[0]
		}
		m := &mount{
			id: r.ID, path: r.Path, role: role, docs: docs, docsFolders: r.DocsFolders,
			label: filepath.Base(filepath.Clean(r.Path)), vlt: r.Vault,
		}
		reg.byID[m.id] = m
		reg.mounts = append(reg.mounts, m)
	}

	client, searcher := newPandoSearcher(settings, reg, log)
	if searcher == nil {
		space.SetSemanticSearcher(nil)
		return func() error { return nil }
	}
	space.SetSemanticSearcher(searcher)
	return client.Close
}
