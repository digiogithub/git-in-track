package server

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/impact"
	"github.com/digiogithub/git-in-track/internal/pando"
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
	// SemanticSearch is the repository's opt-in to a managed Pando
	// (`repos[].semanticSearch`). Only [InstallDiscoveredSemanticSearch] reads it.
	SemanticSearch bool
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
//
// cacheDir is where the tier 2 graph check is persisted for this endpoint
// (GIT-US-0188); empty keeps the answer in memory only.
func InstallSemanticSearch(settings config.SearchPando, cacheDir string, space *vault.Workspace, repos []SemanticRepo, log *slog.Logger) *SemanticHost {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	reg := semanticRegistry(space, repos)
	client, searcher := newPandoSearcher(settings, reg, log)
	if searcher == nil {
		space.SetSemanticSearcher(nil)
		return &SemanticHost{}
	}
	space.SetSemanticSearcher(searcher)
	host := &SemanticHost{client: client, searcher: searcher}
	if cacheDir != "" {
		host.graphCache = cacheDir
		host.endpoint = strings.TrimSpace(settings.MCPURL)
		host.projects = make(map[string]string, len(repos))
		for _, r := range repos {
			host.projects[r.ID] = pando.SanitizeProjectID(r.Path)
		}
	}
	return host
}

// semanticRegistry mounts the repositories a host attached to space.
func semanticRegistry(space *vault.Workspace, repos []SemanticRepo) *registry {
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
		m.semantic.Store(r.SemanticSearch)
		reg.byID[m.id] = m
		reg.mounts = append(reg.mounts, m)
	}
	return reg
}

// SemanticHost is the Pando session a host other than the companion built
// with [InstallSemanticSearch]. Its CallGraph and Semantic methods are the
// functions [TraceSeams] reads at call time, so tiers 2 and 3 of the impact
// query reach the same Pando search_semantic does. The zero value is a host
// with no Pando: both answer nil and Close does nothing.
type SemanticHost struct {
	client   pandoAPI
	searcher *pandoSearcher
	// discovered is set by [InstallDiscoveredSemanticSearch]: one connect-only
	// slot per opted-in repository instead of one client.
	discovered *managedState
	// graphCache, endpoint and projects persist the graph check of an external
	// Pando: the cache directory, the endpoint URL and the code project of each
	// repository (GIT-US-0188). Empty for any other host.
	graphCache string
	endpoint   string
	projects   map[string]string

	// probeMu guards probeCtx, the base context of the background graph
	// checks; Close cancels it (GIT-US-0190).
	probeMu     sync.Mutex
	probeCtx    context.Context
	probeCancel context.CancelFunc
}

// ProbeContext is the context the background tier 2 graph checks run under. A
// short-lived host cancels it in Close, so a check still running when the
// process exits does not stay queued in Pando. Nil-safe: a nil host answers
// context.Background().
func (h *SemanticHost) ProbeContext() context.Context {
	if h == nil {
		return context.Background()
	}
	h.probeMu.Lock()
	defer h.probeMu.Unlock()
	if h.probeCtx == nil {
		h.probeCtx, h.probeCancel = context.WithCancel(context.Background())
	}
	return h.probeCtx
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

// CallGraphFor is the code graph tier 2 reads for one repository. A host
// connected to managed instances answers that repository's own instance, or a
// graph that says why there is none; any other host answers [SemanticHost.CallGraph].
func (h *SemanticHost) CallGraphFor(repo string) impact.CallGraph {
	if h != nil && h.discovered != nil {
		return h.discovered.impactGraph(repo)
	}
	return h.CallGraph()
}

// GraphStoreFor is where the tier 2 graph check of one repository is persisted
// across processes. A host connected to managed instances answers the
// instance's own store; a host with an external Pando answers a TTL-only file
// keyed by endpoint and project; any other host answers nil.
func (h *SemanticHost) GraphStoreFor(repo string) func(impact.CallGraph) impact.GraphStore {
	switch {
	case h == nil:
		return nil
	case h.discovered != nil:
		return h.discovered.graphStore(repo)
	case h.graphCache != "" && h.endpoint != "":
		project := h.projects[repo]
		if project == "" {
			return nil
		}
		store := externalGraphStore(h.graphCache, h.endpoint, project)
		return func(impact.CallGraph) impact.GraphStore { return store }
	}
	return nil
}

// SemanticFor is the searcher tier 3 asks for one repository, as
// [SemanticHost.CallGraphFor] is for tier 2.
func (h *SemanticHost) SemanticFor(repo string) vault.SemanticSearcher {
	if h != nil && h.discovered != nil {
		return h.discovered.impactSemantic(repo)
	}
	return h.Semantic()
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
	if h != nil {
		h.ProbeContext() // make sure there is one to cancel
		h.probeMu.Lock()
		h.probeCancel()
		h.probeMu.Unlock()
	}
	if h != nil && h.discovered != nil {
		for _, slot := range h.discovered.slots() {
			slot.mu.Lock()
			c := slot.client
			slot.client = nil
			slot.mu.Unlock()
			if c != nil {
				_ = c.Close()
			}
		}
		return nil
	}
	if h == nil || h.client == nil {
		return nil
	}
	if err := h.client.Close(); err != nil {
		return fmt.Errorf("close the Pando session: %w", err)
	}
	return nil
}
