package server

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/mcp"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The streamable-HTTP MCP surface, story GIT-US-0024.
//
// `gintrack serve --mcp-http` mounts the very same tool registry `gintrack mcp`
// serves over stdio, over the workspace this process already indexes and
// watches — one index, one watcher, shared with the web UI. The endpoint sits
// behind the bearer token and the origin checks every other local surface sits
// behind: the MCP server has no privileged path and grants no capability a
// human with a checkout lacks.

// mcpPath is where the streamable-HTTP transport is mounted.
const mcpPath = "/mcp"

// originMCP is the `origin` of an event caused by an agent writing through MCP,
// as opposed to `api` for the REST surface and `watcher` for a file that
// changed underneath us (docs/07 section 5.6).
const originMCP = "mcp"

// newMCPServer builds the MCP server the HTTP transport serves, or nil when the
// endpoint is disabled. A failure to build it is reported and the server starts
// without the endpoint: an agent surface that cannot come up must not stop the
// web UI from coming up.
func (s *Server) newMCPServer(opts Options) *mcp.Server {
	if !opts.MCPHTTP {
		return nil
	}
	roots := make([]string, 0, len(opts.Repos))
	for _, m := range s.repos.all() {
		roots = append(roots, m.path)
	}
	srv, err := mcp.New(mcp.Options{
		Core:       s.repos.workspace(),
		Version:    opts.Version,
		Agent:      opts.MCPAgent,
		AllowWrite: opts.MCPAllowWrite,
		Roots:      roots,
		AfterWrite: s.publishAgentWrite,
		Logger:     s.log,
		Now:        s.now,
	})
	if err != nil {
		s.log.Warn("the MCP endpoint could not be started", "error", err)
		return nil
	}
	return srv
}

// mountMCP registers POST /mcp behind the bearer token. It is deliberately
// outside /api/v1: the path is the one every MCP client configuration expects,
// and the transport speaks JSON-RPC rather than REST.
func (s *Server) mountMCP(r chi.Router) {
	if s.mcp == nil {
		// The path still answers, so that a client configured against a server
		// that has the endpoint switched off learns why instead of receiving
		// the single-page application.
		disabled := s.notImplemented(
			"The MCP endpoint is disabled. Start the server with `gintrack serve --mcp-http`, " +
				"or run `gintrack mcp` for the stdio transport.")
		r.Handle(mcpPath, disabled)
		r.Handle(mcpPath+"/*", disabled)
		return
	}
	handler := s.mcp.HTTPHandler()
	r.Group(func(m chi.Router) {
		m.Use(s.bearerAuth)
		m.Handle(mcpPath, handler)
		m.Handle(mcpPath+"/*", handler)
	})
}

// publishAgentWrite folds a write made through an MCP tool into everything the
// companion does with a write of its own: the event stream the open UIs listen
// to, the index freshness stamp, and the commit-on-save queue. An agent's
// change is an ordinary file change, and it is treated as one.
func (s *Server) publishAgentWrite(ctx context.Context, ev mcp.WriteEvent) {
	if moved, ok := ev.Result.(vault.BoardMoveResult); ok {
		s.publishWriteSetsWith("", moved.Writes)
		s.commitWriteSets(ctx, moved.Writes, moveFields(moved))
		s.publishAgentItemEvent(moved, ev.Op)
		return
	}
	m, found := s.repos.forItem(ev.ItemID)
	if !found {
		return
	}
	if ev.ItemID != "" {
		s.hub.Publish(eventItemChanged, itemChangedData{
			Repo:   m.id,
			ID:     ev.ItemID,
			Op:     ev.Op,
			Rev:    revOf(field(ev.Result, "item")),
			Origin: originMCP,
		})
	}
	counts := indexCounts{Updated: 1}
	if ev.Op == "created" {
		counts = indexCounts{Added: 1}
	}
	s.publishIndexUpdated(m, counts, "")
	m.touch(s.now())
	s.commitItemWrite(ctx, m, ev.Result, ev.ItemID, ev.Op)
}

// publishAgentItemEvent announces the item half of a card move an agent made.
func (s *Server) publishAgentItemEvent(moved vault.BoardMoveResult, op string) {
	if moved.Item == nil || string(moved.Item.ID) == "" {
		return
	}
	for _, set := range moved.Writes {
		m, found := s.repos.lookup(set.VaultID)
		if !found || set.VaultID == "" {
			continue
		}
		s.hub.Publish(eventItemChanged, itemChangedData{
			Repo:   m.id,
			ID:     string(moved.Item.ID),
			Op:     op,
			Rev:    string(moved.Item.Rev),
			Origin: originMCP,
		})
	}
}

// mcpTools reports the tools the HTTP endpoint advertises, for the startup
// banner and for /capabilities. It is empty when the endpoint is disabled.
func (s *Server) mcpTools() []string {
	if s.mcp == nil {
		return nil
	}
	return s.mcp.Tools()
}
