package server

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/impact"
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
// The returned host is never nil: it hands the same Pando client and searcher
// to the impact seam ([TraceSeams.CallGraph], [TraceSeams.Semantic]), exactly
// as the companion does, and closes the Pando session (GIT-US-0147).
//
// Unlike the companion it registers no code project with Pando: that is the
// long-lived server's job, and a short-lived agent session must not queue an
// indexing job every time it is spawned.
func InstallSemanticSearch(settings config.SearchPando, space *vault.Workspace, repos []SemanticRepo, log *slog.Logger) *SemanticHost {
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
		return &SemanticHost{}
	}
	space.SetSemanticSearcher(searcher)
	return &SemanticHost{client: client, searcher: searcher}
}

// SemanticHost is the Pando session a host other than the companion built
// with [InstallSemanticSearch]. Its CallGraph and Semantic methods are the
// functions [TraceSeams] reads at call time, so tiers 2 and 3 of the impact
// query reach the same Pando search_semantic does. The zero value is a host
// with no Pando: both answer nil and Close does nothing.
type SemanticHost struct {
	client   pandoAPI
	searcher *pandoSearcher
}

// CallGraph is the Pando client tier 2 of the impact query calls; nil when no
// Pando is configured or the client cannot read the code graph.
func (h *SemanticHost) CallGraph() impact.CallGraph {
	if h == nil || h.client == nil {
		return nil
	}
	if g, ok := h.client.(impact.CallGraph); ok && g != nil {
		return g
	}
	return nil
}

// Semantic is the searcher tier 3 of the impact query asks for requirement
// blocks; nil when semantic search is off.
func (h *SemanticHost) Semantic() vault.SemanticSearcher {
	if h == nil || h.searcher == nil {
		return nil
	}
	return h.searcher
}

// Close ends the Pando session, if there is one.
func (h *SemanticHost) Close() error {
	if h == nil || h.client == nil {
		return nil
	}
	if err := h.client.Close(); err != nil {
		return fmt.Errorf("close the Pando session: %w", err)
	}
	return nil
}
