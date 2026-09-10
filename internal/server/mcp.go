package server

import (
	"context"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/config"
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

// mcpState owns the MCP surface of this process: whether the write tools are
// advertised, the server that advertises them, and the configuration file the
// choice is persisted to.
//
// The write surface is a setting and not only a flag, because the agents that
// use it are configured elsewhere: an MCP client entry is usually a bare
// `gintrack mcp`, and teaching every runtime to pass `--allow-write` is work a
// checkbox in the UI does once (docs/08-mcp-server.md section 7.1).
type mcpState struct {
	// mu guards allowWrite and server, which a settings change replaces while
	// requests are being served.
	mu         sync.RWMutex
	allowWrite bool
	server     *mcp.Server

	// build rebuilds the server for a new write mode. It is nil when this
	// process serves no MCP endpoint, which is the common case: the setting is
	// then only written to the file `gintrack mcp` reads.
	build func(allowWrite bool) *mcp.Server
	// configPath is where the choice is persisted; empty means this process
	// only, which is what `serve --repo` and a test want.
	configPath string
}

// newMCPState builds the MCP surface: the server the HTTP endpoint serves, when
// it is mounted at all, and the write mode both it and the configuration start
// from.
func (s *Server) newMCPState(opts Options) *mcpState {
	st := &mcpState{allowWrite: opts.MCPAllowWrite, configPath: opts.ConfigPath}
	if !opts.MCPHTTP {
		return st
	}
	st.build = func(allowWrite bool) *mcp.Server { return s.newMCPServer(opts, allowWrite) }
	st.server = st.build(st.allowWrite)
	return st
}

// newMCPServer builds the MCP server the HTTP transport serves, or nil when the
// endpoint is disabled. A failure to build it is reported and the server starts
// without the endpoint: an agent surface that cannot come up must not stop the
// web UI from coming up.
func (s *Server) newMCPServer(opts Options, allowWrite bool) *mcp.Server {
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
		AllowWrite: allowWrite,
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

// server returns the MCP server the endpoint currently serves, nil when there
// is none.
func (m *mcpState) current() *mcp.Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.server
}

// writes reports whether the write tools are advertised.
func (m *mcpState) writes() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.allowWrite
}

// setWrites switches the write tools on or off. A mounted endpoint is rebuilt
// so that the change is live for this process too; the sessions the previous
// server held end with it, and a client initializes again.
func (m *mcpState) setWrites(allowWrite bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.allowWrite == allowWrite {
		return
	}
	m.allowWrite = allowWrite
	if m.build == nil {
		return
	}
	if rebuilt := m.build(allowWrite); rebuilt != nil {
		m.server = rebuilt
	}
}

// persist writes the write mode to the configuration file, and reports whether
// it reached it. `gintrack mcp` reads that file at startup, which is what makes
// the switch outlive this process and reach the stdio servers agents spawn.
func (m *mcpState) persist() (bool, error) {
	m.mu.RLock()
	path, allowWrite := m.configPath, m.allowWrite
	m.mu.RUnlock()
	if path == "" {
		return false, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return false, err //nolint:wrapcheck // config already names the file
	}
	cfg.MCP.AllowWrite = allowWrite
	if err := config.Save(path, cfg); err != nil {
		return false, err //nolint:wrapcheck // config already names the file
	}
	return true, nil
}

// mcpSettingsView is the document GET and PATCH /api/v1/mcp/settings answer.
type mcpSettingsView struct {
	// Supported is false when nothing here could take effect — no endpoint in
	// this process and no configuration file to write — and the UI hides the
	// control rather than offering one that does nothing.
	Supported bool `json:"supported"`
	// AllowWrite is whether the write tools are advertised.
	AllowWrite bool `json:"allowWrite"`
	// HTTP is whether this process serves the endpoint at POST /mcp, in which
	// case the change is live here as well as persisted.
	HTTP bool `json:"http"`
	// Persisted is whether the configuration file took the change, so the UI
	// can say "this process only" instead of promising too much.
	Persisted bool `json:"persisted"`
	// ConfigPath is the file the choice was written to, for the same reason.
	ConfigPath string `json:"configPath,omitempty"`
	// Tools are the tools the endpoint of this process advertises, empty when
	// it serves none.
	Tools []string `json:"tools"`
}

// view renders the current MCP settings.
func (s *Server) mcpSettingsView(persisted bool) mcpSettingsView {
	return mcpSettingsView{
		Supported:  s.mcp.build != nil || s.mcp.configPath != "",
		AllowWrite: s.mcp.writes(),
		HTTP:       s.mcp.current() != nil,
		Persisted:  persisted,
		ConfigPath: s.mcp.configPath,
		Tools:      s.mcpTools(),
	}
}

// mountMCPSettings composes /api/v1/mcp.
func (s *Server) mountMCPSettings(r chi.Router) {
	r.Get("/settings", s.handleMCPSettings)
	r.Patch("/settings", s.handleMCPSettingsPatch)
}

// handleMCPSettings serves GET /api/v1/mcp/settings.
func (s *Server) handleMCPSettings(w http.ResponseWriter, r *http.Request) {
	// A GET reports what the file holds, so `persisted` is only meaningful for
	// a change: it is answered as "there is a file behind this".
	writeJSON(w, r, http.StatusOK, s.mcpSettingsView(s.mcp.configPath != ""))
}

// handleMCPSettingsPatch serves PATCH /api/v1/mcp/settings.
//
// Turning the write tools on lets any agent holding the bearer token — or, for
// the stdio transport, any agent this user starts — create and edit items. It
// is therefore an explicit choice, never a default, and it is logged.
func (s *Server) handleMCPSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var patch struct {
		AllowWrite *bool `json:"allowWrite"`
	}
	if !decodeBody(w, r, &patch) {
		return
	}
	if patch.AllowWrite == nil {
		failProblem(w, r, codeInvalidRequest, "The body must set allowWrite to true or false.")
		return
	}
	if s.mcp.build == nil && s.mcp.configPath == "" {
		failProblem(w, r, codeNotImplemented,
			"This server has no configuration file to write to and serves no MCP endpoint, so the setting would have no effect. Start it from a configuration file, or pass --allow-write.")
		return
	}

	s.mcp.setWrites(*patch.AllowWrite)
	persisted, err := s.mcp.persist()
	if err != nil {
		// The running process already honors the change; only the file did
		// not take it, and the user has to know which of the two happened.
		s.log.Warn("could not persist the MCP settings", "error", err)
	}
	s.log.Info("MCP write tools", "allowWrite", *patch.AllowWrite, "persisted", persisted)
	writeJSON(w, r, http.StatusOK, s.mcpSettingsView(persisted))
}

// mountMCP registers POST /mcp behind the bearer token. It is deliberately
// outside /api/v1: the path is the one every MCP client configuration expects,
// and the transport speaks JSON-RPC rather than REST.
func (s *Server) mountMCP(r chi.Router) {
	if s.mcp.current() == nil {
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
	// The handler resolves the server per request, so that switching the write
	// tools on from the settings page reaches this endpoint without a restart.
	handler := mcp.HTTPHandlerFor(s.mcp.current, s.log)
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
	srv := s.mcp.current()
	if srv == nil {
		return nil
	}
	return srv.Tools()
}
